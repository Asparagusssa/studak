#!/bin/sh
set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$here/../../backend"

if [ ! -f .env ]; then
  printf '%s\n' 'Создайте backend/.env из backend/.env.example и заполните секреты.' >&2
  exit 1
fi

usage() {
  printf '%s\n' 'Использование: start.sh [--ngrok | --dev-tunnel | https://публичный-адрес]' >&2
  exit 2
}
[ "$#" -le 1 ] || usage

case "${1:-}" in
  --ngrok)
    exec sh ./start-ngrok.sh
    ;;
  --dev-tunnel)
    tunnel=studak-max-dev-tunnel
    if [ "$(docker inspect -f '{{.State.Running}}' "$tunnel" 2>/dev/null || true)" != true ]; then
      # Do not register an old HTTPS address while starting a new tunnel.
      WEBHOOK_BASE_URL= docker compose up -d --build --wait --wait-timeout 90 backend
      if docker container inspect "$tunnel" >/dev/null 2>&1; then
        [ "$(docker inspect -f '{{index .Config.Labels "studak.max-dev-tunnel"}}' "$tunnel")" = true ] || {
          printf '%s\n' "Имя контейнера $tunnel уже занято." >&2; exit 1;
        }
        docker rm "$tunnel" >/dev/null
      fi
      docker run -d --name "$tunnel" --label studak.max-dev-tunnel=true \
        --network studak_backend --read-only --cap-drop ALL --security-opt no-new-privileges \
        cloudflare/cloudflared:latest tunnel --no-autoupdate --protocol http2 --url http://backend:8080 >/dev/null
    fi

    url=
    attempts=0
    while [ "$attempts" -lt 30 ]; do
      url=$(docker logs "$tunnel" 2>&1 | sed -n 's/.*\(https:\/\/[a-z0-9-]*\.trycloudflare\.com\).*/\1/p' | tail -n 1)
      [ -n "$url" ] && break
      attempts=$((attempts + 1))
      sleep 1
    done
    if [ -z "$url" ]; then
      printf '%s\n' "Не удалось получить HTTPS-адрес. Проверьте docker logs $tunnel." >&2
      exit 1
    fi
    export WEBHOOK_BASE_URL="$url"
    docker compose up -d --build --wait --wait-timeout 90 backend
    if ! curl -fsS --max-time 5 --retry 4 --retry-delay 2 --retry-all-errors "$url/health" >/dev/null; then
      printf '%s\n' "Бэкенд запущен, но туннель пока не отвечает: $url" >&2
      exit 1
    fi
    printf 'Webhook MAX: %s/webhook/max\nWebhook Telegram: %s/webhook/tg\n' "$url" "$url"
    ;;
  https://?*)
    export WEBHOOK_BASE_URL="${1%/}"
    exec docker compose up -d --build backend
    ;;
  '')
    exec docker compose up -d --build backend
    ;;
  *) usage ;;
esac
