#!/bin/sh
set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$here/.."

if [ "$#" -gt 1 ]; then
  printf '%s\n' 'Использование: seed-demo-msu.sh [--with-likes]' >&2
  exit 2
fi

case "${1:-}" in
  '') with_likes=false ;;
  --with-likes) with_likes=true ;;
  *) printf '%s\n' 'Использование: seed-demo-msu.sh [--with-likes]' >&2; exit 2 ;;
esac

if [ -n "${DOCKER_HOST:-}" ]; then
  endpoint=$DOCKER_HOST
else
  endpoint=$(docker context inspect --format '{{.Endpoints.docker.Host}}')
fi
case "$endpoint" in
  unix://*) ;;
  *) printf '%s\n' 'Команда предназначена только для локального Docker.' >&2; exit 1 ;;
esac

docker compose exec -T postgres sh -c '
  exec psql -X -v ON_ERROR_STOP=1 -v "with_likes=$1" -U "$POSTGRES_USER" -d "$POSTGRES_DB"
' sh "$with_likes" < "$here/demo-msu.sql"
