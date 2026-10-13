# lab-api-queues

**Español** | [English](README.en.md)

Laboratorio para comparar el procesamiento síncrono y el procesamiento con colas en Go.

## Por qué existe este proyecto

Este proyecto pone a prueba una decisión arquitectónica: **¿las escrituras deben procesarse dentro de la petición HTTP, o entregarse a una cola para que las procesen workers?**

Ambas versiones implementan los mismos casos de uso sobre el mismo stack (Go, Postgres, RabbitMQ). Lo único que cambia es cómo llegan las escrituras a la base de datos. Eso permite someter ambas a alta demanda y observar los tradeoffs desde el punto de vista de un arquitecto de software y de un ingeniero DevOps, en lugar de razonarlos en abstracto.

> Los tradeoffs siguientes son las **hipótesis** que este laboratorio busca confirmar o refutar con mediciones. No son resultados.

### Las dos arquitecturas

```
api-sync:   cliente ──HTTP──► API ──INSERT──► Postgres ──► 201 (el dato ya está guardado)

api-async:  cliente ──HTTP──► API ──publish──► RabbitMQ ──► 202 (el dato TODAVÍA no está guardado)
                                                   │
                                                   ▼
                                     worker × N ──INSERT──► Postgres
```

### Tradeoffs: visión de arquitectura de software

| Aspecto | Síncrona | Asíncrona (cola + workers) |
|---------|----------|----------------------------|
| Consistencia | Fuerte: cuando el cliente recibe `201`, la fila existe | Eventual: `202` solo significa "aceptado"; una lectura inmediata puede no ver el cambio |
| Contrato de respuesta | Simple: éxito o un error definitivo (`404`, `400`) | Más débil: los errores que detecta el worker (p. ej. `404` en un update) nunca llegan al cliente |
| Acoplamiento | La disponibilidad de la API depende de la de Postgres | La API depende del broker; Postgres puede estar lento o caído sin rechazar escrituras |
| Complejidad | Un proceso, un solo camino de código | Broker, contrato de mensajes, versionado (campo `type`), reintentos, mensajes envenenados, idempotencia |
| Orden | Las peticiones se aplican en el orden en que terminan | Workers concurrentes pueden aplicar jobs fuera de orden (p. ej. un `update` antes de su `create`) |
| Depuración | Una petición = una línea de log y una traza | Una petición atraviesa API → broker → worker; requiere un id de correlación (`request_id`) |

### Tradeoffs: visión DevOps / operaciones

| Aspecto | Síncrona | Asíncrona (cola + workers) |
|---------|----------|----------------------------|
| Comportamiento ante un pico de tráfico | La latencia crece con la contención en la BD; al agotarse el pool de conexiones, las peticiones expiran o fallan | La latencia de la API se mantiene baja y estable; el pico se convierte en profundidad de cola (backlog) que los workers drenan con el tiempo |
| Cuello de botella | Conexiones y capacidad de escritura de Postgres, golpeadas directamente por cada petición | Se traslada al broker; la carga sobre Postgres queda limitada por `workers × WORKER_CONCURRENCY` |
| Escalado | Se escalan réplicas de la API, pero cada réplica suma conexiones a la BD (más presión) | API y workers escalan de forma independiente (`--scale worker=N`); la presión de escritura es un control que tú ajustas |
| Modo de falla | Los errores son visibles de inmediato para el cliente (`500`, timeouts) | Degradación silenciosa: la API sigue sana mientras crece el backlog; requiere alertas sobre profundidad de cola y lag del consumidor |
| Seguridad de los datos ante una caída | La petición falla y el cliente puede reintentar | Cola durable + mensajes persistentes + ack manual: los jobs en curso se reentregan (at-least-once, así que puede haber duplicados) |
| Métricas clave | Latencia p50/p95/p99, tasa de errores, saturación del pool de la BD | Latencia de la API, profundidad de cola, tasa de publish vs ack, tiempo total hasta la persistencia, reentregas |
| Costo de infraestructura | Menos piezas móviles | Un componente con estado más (RabbitMQ) que operar, monitorear, respaldar y actualizar |

### Cuándo gana cada una

- **Elige la síncrona** cuando el cliente necesita el resultado de inmediato, la carga es predecible o la consistencia fuerte es un requisito de negocio (pagos, verificación de inventario).
- **Elige la asíncrona** cuando el tráfico llega en ráfagas, la escritura es lenta o puede fallar de forma transitoria, el cliente acepta un "recibido, en proceso", o necesitas proteger la base de datos de los picos.

### Experimento sugerido

1. Levanta el stack y lanza la misma carga de escrituras contra ambas APIs (cualquier herramienta de carga HTTP, p. ej. `k6`, `hey` o `vegeta`).
2. Aumenta la concurrencia paso a paso hasta que `api-sync` empiece a degradarse.
3. Compara: percentiles de latencia y tasa de errores en el cliente, profundidad de la cola en RabbitMQ (panel en http://localhost:15672) y el tiempo hasta que las filas aparecen realmente en Postgres.
4. Repite con `--scale worker=1`, `3` y `5`, y deteniendo Postgres unos segundos, para ver cómo falla y se recupera cada versión.

## Componentes

| App | Rol | Puerto |
|-----|-----|--------|
| `api-sync` | CRUD completo directo contra Postgres | 8081 |
| `api-async` | Lee de Postgres; valida las escrituras y las publica en RabbitMQ | 8082 |
| `worker` | Consume jobs tipados (`create` / `update` / `delete`) y los aplica en Postgres (escalable) | — |

| Ruta | api-sync | api-async |
|------|----------|-----------|
| `POST /reservations` | insert → `201` | encola `create` → `202` |
| `GET /reservations?limit=&offset=` | lectura → `200` | lectura → `200` (síncrona) |
| `GET /reservations/:id` | lectura → `200` / `404` | lectura → `200` / `404` (síncrona) |
| `PUT /reservations/:id` | reemplaza email + name → `200` / `404` | encola `update` → `202` |
| `DELETE /reservations/:id` | borra → `204` / `404` | encola `delete` → `202` |

Las tres comparten los casos de uso de reservas (`app/internal/reservation`). HTTP y el consumidor de la cola son adaptadores.
Los borrados son lógicos (`deleted_at`); un id borrado devuelve `404`.

## Ejecutar

```sh
docker compose up --build
```

Escalar workers horizontalmente:

```sh
docker compose up --build --scale worker=3
```

Panel de RabbitMQ: http://localhost:15672 (guest / guest).

## Probar

```sh
# Síncrona: guardado antes de la respuesta
curl -i -X POST localhost:8081/reservations \
  -H 'Content-Type: application/json' \
  -d '{"email":"ana@example.com","name":"Ana"}'

# Asíncrona: encolado, lo guarda después un worker
curl -i -X POST localhost:8082/reservations \
  -H 'Content-Type: application/json' \
  -d '{"email":"ana@example.com","name":"Ana"}'

# Listar (limit por defecto 20, máximo 100; offset por defecto 0)
curl -i 'localhost:8081/reservations?limit=10&offset=0'
curl -i 'localhost:8082/reservations?limit=10&offset=0'

# Obtener por id
curl -i localhost:8081/reservations/1

# Reemplazar email y name: la síncrona devuelve la reserva actualizada (200)
curl -i -X PUT localhost:8081/reservations/1 \
  -H 'Content-Type: application/json' \
  -d '{"email":"bob@example.com","name":"Bob"}'

# Reemplazar vía la cola: 202, lo aplica después un worker
curl -i -X PUT localhost:8082/reservations/1 \
  -H 'Content-Type: application/json' \
  -d '{"email":"carol@example.com","name":"Carol"}'

# Borrar: la síncrona devuelve 204, la asíncrona 202
curl -i -X DELETE localhost:8081/reservations/1
curl -i -X DELETE localhost:8082/reservations/2

# Health
curl localhost:8081/health
```

Una entrada inválida (`email` ausente o inválido, `name` ausente), un id no positivo o no numérico, o un `limit`/`offset` inválido devuelven `400`. Un id inexistente devuelve `404` en la API síncrona; en la asíncrona, un update/delete encolado de un id inexistente lo descarta el worker (se registra en el log y no se reintenta).

## Documentación de la API

Cada API HTTP sirve Swagger UI y su especificación OpenAPI 3 (las especificaciones están en `app/internal/httpapi/openapi/`):

- Síncrona: http://localhost:8081/swagger (especificación: `/openapi.yaml`)
- Asíncrona: http://localhost:8082/swagger (especificación: `/openapi.yaml`)

Los recursos de Swagger UI se cargan desde el CDN de unpkg, así que el navegador necesita acceso a internet.

## Construir una sola app

```sh
docker build --build-arg APP=worker -t reservations-worker app/
```

## Configuración

| Variable | Valor por defecto | Usada por |
|----------|-------------------|-----------|
| `HTTP_PORT` | `8080` | APIs |
| `DATABASE_URL` | se construye a partir de `DB_*` | APIs, worker |
| `DB_HOST` / `DB_PORT` | `postgres` / `5432` | APIs, worker |
| `DB_USER` / `DB_PASSWORD` | `postgres` / `postgres` | APIs, worker |
| `DB_NAME` | `reservations` | APIs, worker |
| `RABBITMQ_URL` | `amqp://guest:guest@rabbitmq:5672/` | api-async, worker |
| `QUEUE_NAME` | `reservations` | api-async, worker |
| `WORKER_CONCURRENCY` | `10` | worker (goroutines por instancia) |
| `WORKER_PREFETCH` | `20` | worker (QoS de RabbitMQ) |

## Tests

```sh
cd app && go test ./...
```
