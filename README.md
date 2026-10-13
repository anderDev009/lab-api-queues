# lab-api-queues

Learning lab comparing synchronous vs queued request processing in Go.

## Why this project exists

This project tests one architectural decision: **should writes be processed inside the HTTP request, or handed off to a queue and processed by workers?**

Both versions implement the same use cases on the same stack (Go, Postgres, RabbitMQ). Only the way writes reach the database differs. That makes it possible to put both under high load and observe the tradeoffs, from a software architect's and a DevOps engineer's point of view, instead of reasoning about them in the abstract.

> The tradeoffs below are the **hypotheses** this lab is meant to confirm or refute with measurements. They are not results.

### The two architectures

```
api-sync:   client ──HTTP──► API ──INSERT──► Postgres ──► 201 (data is stored)

api-async:  client ──HTTP──► API ──publish──► RabbitMQ ──► 202 (data is NOT stored yet)
                                                  │
                                                  ▼
                                    worker × N ──INSERT──► Postgres
```

### Tradeoffs: software architecture view

| Concern | Sync | Async (queue + workers) |
|---------|------|-------------------------|
| Consistency | Strong: when the client gets `201`, the row exists | Eventual: `202` only means "accepted"; a read right after may not see the change |
| Response contract | Simple: success or a definitive error (`404`, `400`) | Weaker: errors found by the worker (e.g. `404` on update) never reach the client |
| Coupling | API availability depends on Postgres availability | API depends on the broker; Postgres can be slow or down without rejecting writes |
| Complexity | One process, one code path | Broker, message contract, versioning (`type` field), retries, poison messages, idempotency |
| Ordering | Requests are applied in the order they finish | Concurrent workers can apply jobs out of order (e.g. `update` before its `create`) |
| Debugging | One request = one log line and one trace | One request spans API → broker → worker; needs a correlation id (`request_id`) |

### Tradeoffs: DevOps / operations view

| Concern | Sync | Async (queue + workers) |
|---------|------|-------------------------|
| Behavior under a traffic spike | Latency grows with DB contention; when the connection pool is exhausted, requests time out or fail | API latency stays low and flat; the spike becomes queue depth (backlog) that workers drain over time |
| Bottleneck | Postgres connections and write throughput, hit directly by every request | Moves to the broker; Postgres load is capped by `workers × WORKER_CONCURRENCY` |
| Scaling | Scale API replicas, but every replica adds DB connections (more pressure on the DB) | Scale API and workers independently (`--scale worker=N`); write pressure is a dial you control |
| Failure mode | Errors visible immediately to clients (`500`, timeouts) | Silent degradation: the API stays green while the backlog grows; needs alerting on queue depth and consumer lag |
| Data safety on crash | Request fails, the client can retry | Durable queue + persistent messages + manual ack: in-flight jobs are redelivered (at-least-once, so duplicates are possible) |
| Key metrics | p50/p95/p99 latency, error rate, DB pool saturation | API latency, queue depth, publish vs ack rate, end-to-end time to persistence, redeliveries |
| Infrastructure cost | Fewer moving parts | One more stateful component (RabbitMQ) to run, monitor, back up and upgrade |

### When each one wins

- **Choose sync** when the client needs the result immediately, the load is predictable, or strong consistency is a business requirement (payments, inventory checks).
- **Choose async** when traffic is bursty, the write is slow or can fail transiently, the client can accept "accepted, processing", or you need to protect the database from spikes.

### Suggested experiment

1. Start the stack and fire the same write load at both APIs (any HTTP load tool, e.g. `k6`, `hey` or `vegeta`).
2. Increase concurrency step by step until `api-sync` starts degrading.
3. Compare: client latency percentiles and error rate, RabbitMQ queue depth (management UI at http://localhost:15672), and the time until rows actually appear in Postgres.
4. Repeat with `--scale worker=1`, `3` and `5`, and with Postgres stopped for a few seconds, to see how each version fails and recovers.

| App | Role | Port |
|-----|------|------|
| `api-sync` | Full CRUD directly against Postgres | 8081 |
| `api-async` | Reads from Postgres; validates writes and publishes them to RabbitMQ | 8082 |
| `worker` | Consumes typed jobs (`create` / `update` / `delete`) and applies them to Postgres (scalable) | — |

| Route | api-sync | api-async |
|-------|----------|-----------|
| `POST /reservations` | insert → `201` | enqueue `create` → `202` |
| `GET /reservations?limit=&offset=` | read → `200` | read → `200` (synchronous) |
| `GET /reservations/:id` | read → `200` / `404` | read → `200` / `404` (synchronous) |
| `PUT /reservations/:id` | replace email + name → `200` / `404` | enqueue `update` → `202` |
| `DELETE /reservations/:id` | delete → `204` / `404` | enqueue `delete` → `202` |

All three share the reservation use cases (`app/internal/reservation`). HTTP and the queue consumer are adapters.
Deletes are soft deletes (`deleted_at`); a deleted id returns `404`.

## Run

```sh
docker compose up --build
```

Scale workers horizontally:

```sh
docker compose up --build --scale worker=3
```

RabbitMQ UI: http://localhost:15672 (guest / guest).

## Try it

```sh
# Sync: stored before the response
curl -i -X POST localhost:8081/reservations \
  -H 'Content-Type: application/json' \
  -d '{"email":"ana@example.com","name":"Ana"}'

# Async: queued, stored later by a worker
curl -i -X POST localhost:8082/reservations \
  -H 'Content-Type: application/json' \
  -d '{"email":"ana@example.com","name":"Ana"}'

# List (limit defaults to 20, max 100; offset defaults to 0)
curl -i 'localhost:8081/reservations?limit=10&offset=0'
curl -i 'localhost:8082/reservations?limit=10&offset=0'

# Get by id
curl -i localhost:8081/reservations/1

# Replace email and name: sync returns the updated reservation (200)
curl -i -X PUT localhost:8081/reservations/1 \
  -H 'Content-Type: application/json' \
  -d '{"email":"bob@example.com","name":"Bob"}'

# Replace via the queue: 202, applied later by a worker
curl -i -X PUT localhost:8082/reservations/1 \
  -H 'Content-Type: application/json' \
  -d '{"email":"carol@example.com","name":"Carol"}'

# Delete: sync returns 204, async returns 202
curl -i -X DELETE localhost:8081/reservations/1
curl -i -X DELETE localhost:8082/reservations/2

# Health
curl localhost:8081/health
```

Invalid input (missing/invalid `email`, missing `name`), a non-positive or non-numeric id, or an invalid `limit`/`offset` returns `400`. A missing id returns `404` on the sync API; on the async API a queued update/delete of a missing id is dropped by the worker (logged, not retried).

## API docs

Each HTTP API serves Swagger UI and its OpenAPI 3 spec (specs live in `app/internal/httpapi/openapi/`):

- Sync: http://localhost:8081/swagger (spec: `/openapi.yaml`)
- Async: http://localhost:8082/swagger (spec: `/openapi.yaml`)

Swagger UI assets load from the unpkg CDN, so the browser needs internet access.

## Build one app

```sh
docker build --build-arg APP=worker -t reservations-worker app/
```

## Configuration

| Variable | Default | Used by |
|----------|---------|---------|
| `HTTP_PORT` | `8080` | APIs |
| `DATABASE_URL` | built from `DB_*` | APIs, worker |
| `DB_HOST` / `DB_PORT` | `postgres` / `5432` | APIs, worker |
| `DB_USER` / `DB_PASSWORD` | `postgres` / `postgres` | APIs, worker |
| `DB_NAME` | `reservations` | APIs, worker |
| `RABBITMQ_URL` | `amqp://guest:guest@rabbitmq:5672/` | api-async, worker |
| `QUEUE_NAME` | `reservations` | api-async, worker |
| `WORKER_CONCURRENCY` | `10` | worker (goroutines per instance) |
| `WORKER_PREFETCH` | `20` | worker (RabbitMQ QoS) |

## Tests

```sh
cd app && go test ./...
```
