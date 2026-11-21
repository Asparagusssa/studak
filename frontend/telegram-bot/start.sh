#!/bin/sh
set -eu
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
# Both bots run in the same Go service, database and HTTPS tunnel.
exec "$here/../max-bot/start.sh" "$@"
