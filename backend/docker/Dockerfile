# syntax=docker/dockerfile:1.7

FROM golang:1.26-alpine AS build

RUN apk add --no-cache ca-certificates git
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM golang:1.26-alpine AS goose-build

ARG GOOSE_VERSION=v3.29.0
RUN --mount=type=cache,target=/go/pkg/mod \
    GOBIN=/out go install \
      -tags='no_azuresql no_clickhouse no_libsql no_mssql no_mysql no_sqlite3 no_vertica no_ydb' \
      -ldflags='-s -w' \
      github.com/pressly/goose/v3/cmd/goose@${GOOSE_VERSION}

FROM alpine:3.23 AS backend

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app \
    && adduser -S -G app -H app

WORKDIR /app
COPY --from=build --chown=app:app /out/server /app/server

USER app
EXPOSE 8080
ENV GIN_MODE=release

ENTRYPOINT ["/app/server"]

FROM alpine:3.23 AS migrate

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app \
    && adduser -S -G app -H app

COPY --from=goose-build /out/goose /usr/local/bin/goose
COPY --chown=app:app migrations /migrations

USER app
ENV GOOSE_DRIVER=postgres \
    GOOSE_DBSTRING=sslmode=disable \
    GOOSE_MIGRATION_DIR=/migrations \
    NO_COLOR=1

ENTRYPOINT ["/usr/local/bin/goose"]
CMD ["up"]
