#!/bin/sh
set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$here"

for command in docker ngrok python3 curl; do
  if ! command -v "$command" >/dev/null 2>&1; then
    printf 'Не найдена команда: %s\n' "$command" >&2
    exit 1
  fi
done
if [ ! -f .env ]; then
  printf '%s\n' 'Создайте backend/.env из backend/.env.example и заполните настройки.' >&2
  exit 1
fi
docker info >/dev/null
ngrok config check >/dev/null

# Read the effective host port without sourcing .env or printing credentials.
port=$(docker compose config --format json | python3 -c '
import json, re, sys
config = json.load(sys.stdin)
backend = config["services"]["backend"]
env = backend.get("environment", {})
errors = []
if len((env.get("API_TOKEN") or "").strip()) < 32:
    errors.append("API_TOKEN должен содержать минимум 32 символа.")
for token, secret in (("TG_BOT_TOKEN", "TG_WEBHOOK_SECRET"), ("MAX_BOT_TOKEN", "MAX_WEBHOOK_SECRET")):
    value = (env.get(token) or "").strip()
    if not value:
        print(token + " не заполнен: этот бот пока отключён.", file=sys.stderr)
        continue
    if token == "TG_BOT_TOKEN" and not re.fullmatch(r"[0-9]+:[A-Za-z0-9_-]+", value):
        errors.append("TG_BOT_TOKEN должен иметь формат числовой-ID:секрет от BotFather.")
    if not re.fullmatch(r"[A-Za-z0-9_-]{16,256}", (env.get(secret) or "").strip()):
        errors.append(secret + " должен содержать 16–256 символов A-Z, a-z, 0-9, _ или -. Сгенерируйте отдельный секрет: openssl rand -hex 32")
if errors:
    sys.exit("Исправьте backend/.env перед запуском:\n" + "\n".join(errors))
ports = [str(p["published"]) for p in backend["ports"] if int(p["target"]) == 8080]
if len(ports) != 1 or not ports[0].isdigit():
    sys.exit("Не удалось определить локальный порт backend.")
print(ports[0])
')

umask 077
runtime_dir=$(mktemp -d "${TMPDIR:-/tmp}/studak-ngrok.XXXXXX")
ngrok_pid=
cleanup() {
  if [ -n "$ngrok_pid" ]; then
    kill "$ngrok_pid" 2>/dev/null || true
    wait "$ngrok_pid" 2>/dev/null || true
  fi
  rm -rf "$runtime_dir"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Reuse an existing local tunnel only when it forwards to this backend port.
url=$(python3 - "$port" <<'PY'
import json, sys, urllib.error, urllib.request
from urllib.parse import urlparse
try:
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open("http://127.0.0.1:4040/api/tunnels", timeout=2) as response:
        tunnels = json.load(response).get("tunnels", [])
    for tunnel in tunnels:
        address = str(tunnel.get("config", {}).get("addr", ""))
        upstream = urlparse(address if "://" in address else "http://" + address)
        public_url = str(tunnel.get("public_url", ""))
        public = urlparse(public_url)
        if (upstream.scheme == "http" and upstream.hostname in ("localhost", "127.0.0.1", "::1")
                and upstream.port == int(sys.argv[1]) and upstream.path in ("", "/")
                and not upstream.username and not upstream.query and not upstream.fragment
                and public.scheme == "https" and public.hostname and not public.username
                and not public.query and not public.fragment):
            print(public_url.rstrip("/"))
            break
except (OSError, ValueError, TypeError, AttributeError, urllib.error.URLError):
    pass
PY
)

if [ -n "$url" ]; then
  printf 'Используется уже работающий туннель: %s\n' "$url"
else
  ngrok http "http://127.0.0.1:$port" --log stdout --log-format json >"$runtime_dir/ngrok.log" 2>&1 &
  ngrok_pid=$!
  attempts=0
  while [ "$attempts" -lt 45 ]; do
    if ! kill -0 "$ngrok_pid" 2>/dev/null; then
      # Print error codes, never raw logs that might include credentials.
      python3 - "$runtime_dir/ngrok.log" <<'PY' >&2
import re, sys
with open(sys.argv[1]) as stream:
    codes = sorted(set(re.findall(r"ERR_NGROK_[0-9]+", stream.read())))
for code in codes:
    print("Ошибка ngrok: " + code + " — https://ngrok.com/docs/errors/" + code.lower())
if "ERR_NGROK_334" in codes:
    print("Этот HTTPS-адрес уже занят. Остановите прежний туннель в его терминале или в панели ngrok.")
PY
      printf '%s\n' "ngrok завершился. Проверьте авторизацию и доступ к сети командой: ngrok http $port" >&2
      exit 1
    fi
    url=$(python3 - "$runtime_dir/ngrok.log" <<'PY'
import json, sys
from urllib.parse import urlparse
with open(sys.argv[1]) as stream:
    for line in stream:
        try:
            event = json.loads(line)
        except ValueError:
            continue
        url = event.get("url", "")
        parsed = urlparse(url)
        if event.get("msg") == "started tunnel" and parsed.scheme == "https" and parsed.hostname:
            print(url.rstrip("/"))
            break
PY
  )
    [ -n "$url" ] && break
    attempts=$((attempts + 1))
    sleep 1
  done
  if [ -z "$url" ]; then
    printf '%s\n' "ngrok не выдал HTTPS-адрес за 45 секунд. Проверьте: ngrok http $port" >&2
    exit 1
  fi
fi

# The tunnel must exist before startup registers either bot's webhook.
export WEBHOOK_BASE_URL="$url"
docker compose up -d --build --wait --wait-timeout 120 backend
curl -fsS --max-time 10 --retry 4 --retry-delay 2 --retry-all-errors \
  -H 'ngrok-skip-browser-warning: true' "$url/health" >/dev/null

printf 'Локальный API: http://127.0.0.1:%s\nWebhook Telegram: %s/webhook/tg\nWebhook MAX: %s/webhook/max\n' "$port" "$url" "$url"
printf '%s\n' 'Регистрацию ботов проверьте: docker compose logs --tail=50 backend'
if [ -n "$ngrok_pid" ]; then
  printf '%s\n' 'Ctrl+C остановит ngrok. Backend и база продолжат работать; остановка: docker compose down'
  wait "$ngrok_pid"
else
  printf '%s\n' 'Повторный процесс ngrok не запускался. Туннель продолжает работать в прежнем процессе.'
fi
