# syntax=docker/dockerfile:1.7
#
# Multi-stage build for every Ispilolite command. The target binary is chosen
# with the TARGET build argument, so the same Dockerfile produces the API, the
# migration runner, and the notification worker without duplication.
#
# Build from the repository root:
#   docker build -f docker/Dockerfile --build-arg TARGET=auth -t ispilolite-api .
#   docker build -f docker/Dockerfile --build-arg TARGET=migrate -t ispilolite-migrate .
#   docker build -f docker/Dockerfile --build-arg TARGET=notification -t ispilolite-notification .

ARG GO_VERSION=1.26

FROM golang:${GO_VERSION}-alpine AS build

WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGET=auth
ENV CGO_ENABLED=0 \
    GOOS=linux
RUN test -d "./cmd/${TARGET}" || { echo "unknown TARGET '${TARGET}'; expected a directory under cmd/"; exit 1; } && \
    go build -trimpath -ldflags="-s -w" -o /out/app "./cmd/${TARGET}"

# --- Runtime -----------------------------------------------------------------
# Alpine is used instead of scratch so operators get a shell, CA roots, and
# timezone data for outbound HTTPS calls (M-Pesa, Airtel, Talksasa, OTel).
FROM alpine:3.20 AS runtime

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S -G app app

WORKDIR /app

COPY --from=build /out/app /app/app
COPY --from=build /src/config /app/config
COPY --from=build /src/migrations /app/migrations

# Writable locations used by document storage and audit archives.
RUN mkdir -p /app/storage/documents /app/storage/audit && \
    chown -R app:app /app/storage

USER app

ENV ENVIRONMENT=production \
    CONFIG_PATH=/app/config/config.yaml \
    MIGRATIONS_PATH=/app/migrations \
    PORT=8001

EXPOSE 8001

ENTRYPOINT ["/app/app"]
