# Container deployment

The `Dockerfile` is a multi-stage build that produces one binary per command,
selected with the `TARGET` build argument. Builds always run from the
repository root so the module cache and source copy behave correctly.

## Build an image

```bash
docker build -f docker/Dockerfile --build-arg TARGET=auth         -t ispilolite-api:latest .
docker build -f docker/Dockerfile --build-arg TARGET=migrate      -t ispilolite-migrate:latest .
docker build -f docker/Dockerfile --build-arg TARGET=notification -t ispilolite-notification:latest .
```

Valid targets are the directory names under `cmd/` (`auth`, `core`, `migrate`,
`notification`, ...). The image runs as the non-root `app` user and contains
`config/` plus `migrations/`, so no bind mount is required.

## Compose stack

The stack is a **local-development** stack (plain internal DB/Redis/RabbitMQ
connections). It includes Postgres, Redis, RabbitMQ, the API, and monitoring
services; only the API and UIs are published, on loopback. For production use
the hardened VPS deployment guide with proper TLS and secrets. Compose sets
`ENVIRONMENT=development` deliberately even when the host `.env` says
`production`: connecting to `postgres` without TLS in production is rejected.

```bash
cp .env.example .env        # then replace every <...> placeholder
docker compose --env-file .env -f docker/docker-compose.yaml up -d --build
```

Run schema migrations before starting a new API image:

```bash
docker compose --env-file .env -f docker/docker-compose.yaml run --rm migrate
```

Enable the async SMS/push worker (requires `RABBITMQ_URL` and
`NOTIFICATION_WEBHOOK_URL`):

```bash
docker compose --env-file .env -f docker/docker-compose.yaml --profile workers up -d
```

Check readiness:

```bash
curl -fsS http://127.0.0.1:8001/readyz
curl -fsS http://127.0.0.1:8001/health
```

## Notes

- `DATABASE_URL`, `DATABASE_READ_URL`, and `REDIS_URL` set in the compose
  service `environment` override the values in `.env`, so containers talk to
  the compose network instead of `localhost`.
- `TRUSTED_PROXIES` defaults to loopback. Keep Nginx on the same host, or add
  its CIDR when the proxy runs elsewhere.
- Compose disables the metrics token **only on its local bridge network** so
  Prometheus can scrape `api:8001`. Production requires `METRICS_TOKEN` and a
  Prometheus `authorization.credentials_file` provisioned outside the repo.
- `.dockerignore` excludes `.env`, `.gocache`, binaries, tests, and docs, so
  secrets and build artifacts never enter the image.
