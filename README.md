# lab-api-queues

Learning lab comparing synchronous vs queued request processing in Go.

| App | Role | Port |
|-----|------|------|
| `api-sync` | `POST /reservations` inserts into Postgres, returns `201` | 8081 |
| `api-async` | `POST /reservations` validates and publishes to RabbitMQ, returns `202` | 8082 |
| `worker` | Consumes the queue and inserts into Postgres (scalable) | — |

All three share the `CreateReservation` use case (`app/internal/reservation`). HTTP and the queue consumer are adapters.

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

# Health
curl localhost:8081/health
```

Invalid input (missing/invalid `email`, missing `name`) returns `400`.

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
| `DATABASE_URL` | built from `DB_*` | api-sync, worker |
| `DB_HOST` / `DB_PORT` | `postgres` / `5432` | api-sync, worker |
| `DB_USER` / `DB_PASSWORD` | `postgres` / `postgres` | api-sync, worker |
| `DB_NAME` | `reservations` | api-sync, worker |
| `RABBITMQ_URL` | `amqp://guest:guest@rabbitmq:5672/` | api-async, worker |
| `QUEUE_NAME` | `reservations` | api-async, worker |
| `WORKER_CONCURRENCY` | `10` | worker (goroutines per instance) |
| `WORKER_PREFETCH` | `20` | worker (RabbitMQ QoS) |

## Tests

```sh
cd app && go test ./...
```
