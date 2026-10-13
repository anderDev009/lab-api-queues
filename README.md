# lab-api-queues

Learning lab comparing synchronous vs queued request processing in Go.

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
