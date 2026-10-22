# lab-api-queues

**Español** | [English](README.en.md)

<p>
  <img alt="Go" src="https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white">
  <img alt="PostgreSQL" src="https://img.shields.io/badge/PostgreSQL-4169E1?logo=postgresql&logoColor=white">
  <img alt="RabbitMQ" src="https://img.shields.io/badge/RabbitMQ-FF6600?logo=rabbitmq&logoColor=white">
  <img alt="Kubernetes" src="https://img.shields.io/badge/Kubernetes-326CE5?logo=kubernetes&logoColor=white">
  <img alt="k6" src="https://img.shields.io/badge/k6-7D64FF?logo=k6&logoColor=white">
  <img alt="Docker" src="https://img.shields.io/badge/Docker-2496ED?logo=docker&logoColor=white">
</p>

Laboratorio para comparar el procesamiento síncrono y el procesamiento con colas en Go, desplegado en Kubernetes y medido con k6.

**Contenido:** [Por qué existe](#por-qué-existe-este-proyecto) · [Arquitecturas](#las-dos-arquitecturas) · [Componentes](#componentes) · [Ejecutar](#ejecutar) · [Kubernetes](#ejecutar-en-kubernetes-kustomize) · [Pruebas de carga](#pruebas-de-carga-con-k6) · [API](#documentación-de-la-api) · [Configuración](#configuración) · [Pendiente](#pendiente)

## Por qué existe este proyecto

Este proyecto pone a prueba una decisión arquitectónica: **¿las escrituras deben procesarse dentro de la petición HTTP, o entregarse a una cola para que las procesen workers?**

Ambas versiones implementan los mismos casos de uso sobre el mismo stack (Go, Postgres, RabbitMQ). Lo único que cambia es cómo llegan las escrituras a la base de datos. Eso permite someter ambas a alta demanda y observar los tradeoffs desde el punto de vista de un arquitecto de software y de un ingeniero DevOps, en lugar de razonarlos en abstracto.

> Los tradeoffs siguientes son las **hipótesis** que este laboratorio busca confirmar o refutar con mediciones. No son resultados.

### Las dos arquitecturas

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/img/architecture.es.dark.png">
    <img alt="Diagrama de arquitectura: k6 entra por el Ingress de Traefik; api-sync escribe directo en PostgreSQL y api-async publica en RabbitMQ para que un worker escriba en PostgreSQL" src="docs/img/architecture.es.light.png" width="900">
  </picture>
</p>

- **`api-sync`**: la petición escribe en Postgres y responde `201`; el dato ya está guardado.
- **`api-async`**: la petición se valida, se publica en RabbitMQ y responde `202`; un worker lo guarda después.

<details>
<summary>Versión en texto</summary>

```
api-sync:   cliente ──HTTP──► API ──INSERT──► Postgres ──► 201 (el dato ya está guardado)

api-async:  cliente ──HTTP──► API ──publish──► RabbitMQ ──► 202 (el dato TODAVÍA no está guardado)
                                                   │
                                                   ▼
                                     worker × N ──INSERT──► Postgres
```

</details>

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
| `POST /admin/truncate` | vacía la tabla → `204` | vacía la tabla (sin pasar por la cola) → `204` |

Las tres comparten los casos de uso de reservas (`app/internal/reservation`). HTTP y el consumidor de la cola son adaptadores.
Los borrados son lógicos (`deleted_at`); un id borrado devuelve `404`.

> ⚠️ `POST /admin/truncate` ejecuta `TRUNCATE TABLE reservations RESTART IDENTITY`: borra todo y reinicia los ids. No tiene autenticación; está pensado solo para restablecer el estado entre pruebas de carga.

## Ejecutar

```sh
docker compose up --build
```

Escalar workers horizontalmente:

```sh
docker compose up --build --scale worker=3
```

Panel de RabbitMQ: http://localhost:15672 (guest / guest).

## Ejecutar en Kubernetes (Kustomize)

Los manifiestos están en `infrastructure/` y se organizan con Kustomize: una `base` por componente y overlays por entorno. El objetivo es correr el stack en un cluster para hacer pruebas de carga con k6.

| Base | Recursos |
|------|----------|
| `base/database` | Postgres 15: `PersistentVolumeClaim`, `Deployment` y `Service` (`bd-service:5432`) |
| `base/rabbitmq` | RabbitMQ: `Deployment` y `Service` (`rabbitmq-service`, AMQP + panel) |
| `base/api-sync` | `Deployment`, `Service` e `Ingress` de `api-sync` (host `api-sync.lan`, Traefik) |
| `base/api-async` | `Deployment`, `Service` e `Ingress` de `api-async` (host `api-async.lan`, Traefik) |
| `base/worker` | `Deployment` del `worker` |

Cada app usa su propia imagen, porque `APP` es un argumento de **build** y no de runtime. El binario se elige al compilar, así que pasar `--APP=...` en los `args` del contenedor no tiene efecto. Para construir y publicar las imágenes (`api-sync` y `api-async` en `1.1`, `worker` en `1.0`):

```sh
IMAGE=richarddev009/api_sync_async_membership

docker build --build-arg APP=api-sync  -t $IMAGE:api-sync-1.1  app/ && docker push $IMAGE:api-sync-1.1
docker build --build-arg APP=api-async -t $IMAGE:api-async-1.1 app/ && docker push $IMAGE:api-async-1.1
docker build --build-arg APP=worker    -t $IMAGE:worker-1.0    app/ && docker push $IMAGE:worker-1.0
```

Los `Deployment` de `api-sync` y `api-async` usan `imagePullPolicy: Always`, así que volver a publicar el mismo tag y ejecutar `kubectl -n queue-lab rollout restart deployment/api-sync deployment/api-async` basta para actualizarlos.

```sh
kubectl apply -k infrastructure/overlays/staging
```

Para acceder por Ingress, los hosts `api-sync.lan` y `api-async.lan` deben resolver a la IP del cluster (por ejemplo, con una entrada en `/etc/hosts`).

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

## Pruebas de carga con k6

Los scripts están en `tests/performance/`:

| Archivo | Qué hace |
|---------|----------|
| `performance-test.js` | 10 usuarios virtuales envían 1000 `POST /reservations` a cada API, sin repetir emails (`largeDataset`). Valida `201` en la síncrona y `202` en la asíncrona. |
| `rate-test.js` | Aplica tasas constantes de **100, 500 y 1000 solicitudes/s** (30 s cada una) a ambas APIs a la vez, con emails generados. |
| `mails-sync.js` / `mails-async.js` | Datasets `{ email, name }` (`small`, `medium`, `large`, `edgeCase`, `invalid`) sin emails repetidos entre ambos archivos. |

Los umbrales (`thresholds`) separan cada API con tags (`{api:sync}` / `{api:async}`): p95 de latencia, tasa de errores y checks.

```sh
# Restablecer el estado antes de cada corrida (ambas APIs comparten la base de datos)
curl -X POST http://api-sync.lan/admin/truncate

k6 run tests/performance/performance-test.js
k6 run tests/performance/rate-test.js
```

> La latencia de `api-async` solo mide el tiempo de **encolar** (`202`). El tiempo hasta que el dato queda guardado se observa en la profundidad de la cola de RabbitMQ.

## Pendiente

- **Ingress para el panel de RabbitMQ**. `api-sync` y `api-async` ya están expuestas por Ingress.
- **Escalado del `worker` en Kubernetes** (réplicas o HPA). El `Deployment` ya existe.
- **Ejecutar y documentar los resultados** de las pruebas de carga contra el cluster (los scripts ya existen).

## Tests

```sh
cd app && go test ./...
```
