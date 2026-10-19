#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$ROOT"

if ! command -v docker >/dev/null 2>&1; then
  echo "Docker не найден. Установите Docker Desktop или Docker Engine." >&2
  exit 1
fi

if [ ! -f .env ]; then
  cp .env.example .env
  echo "Создан .env. Замените POSTGRES_PASSWORD и API_TOKEN случайными значениями, затем запустите ./start.sh снова." >&2
  exit 1
fi

GRAFANA_PASSWORD_FILE="$ROOT/.grafana-admin-password"
if [ ! -s "$GRAFANA_PASSWORD_FILE" ]; then
  if ! command -v openssl >/dev/null 2>&1; then
    echo "OpenSSL не найден: он нужен для генерации пароля Grafana." >&2
    exit 1
  fi
  umask 077
  openssl rand -hex 32 >"$GRAFANA_PASSWORD_FILE"
  echo "Создан случайный пароль Grafana: $GRAFANA_PASSWORD_FILE" >&2
fi

exec docker compose \
  --file "$ROOT/docker-compose.yml" \
  --env-file "$ROOT/.env" \
  up --build "$@"
