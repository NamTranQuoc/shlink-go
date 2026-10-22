#!/usr/bin/env bash
# Runs contract/webclient/check.mjs: shlink-web-client 4.8.1 driven through its main screens against one
# implementation (php = upstream image, go = this port) on SQLite.
# Usage: scripts/webclient.sh <php|go>    (BROWSER_CHANNEL=chrome uses an installed Google Chrome)
set -euo pipefail

target=${1:?target php|go}
case $target in
  php) image=shlinkio/shlink:5.1.8 ;;
  go) image=shlink-go:dev ;;
  *) echo "unknown target $target" >&2; exit 2 ;;
esac

root=$(cd "$(dirname "$0")/.." && pwd)
server_port=48080
client_port=48090
api_key=webclient-check-key
server=shlink-webclient-server
client=shlink-webclient-client

cleanup() { docker rm -f "$server" "$client" >/dev/null 2>&1 || true; }
trap cleanup EXIT
cleanup

docker run -d --name "$server" -p "$server_port:8080" \
  -e DEFAULT_DOMAIN="localhost:$server_port" -e IS_HTTPS_ENABLED=false -e INITIAL_API_KEY="$api_key" \
  "$image" >/dev/null
docker run -d --name "$client" -p "$client_port:8080" \
  -e SHLINK_SERVER_URL="http://localhost:$server_port" -e SHLINK_SERVER_API_KEY="$api_key" \
  -e SHLINK_SERVER_NAME=Target shlinkio/shlink-web-client:4.8.1 >/dev/null

for _ in $(seq 1 60); do
  curl -fs "http://localhost:$server_port/rest/health" >/dev/null && break
  sleep 1
done
curl -fs "http://localhost:$server_port/rest/health" >/dev/null || { docker logs "$server"; exit 1; }

cd "$root/contract/webclient"
[[ -d node_modules ]] || npm install --no-audit --no-fund
CLIENT_URL="http://localhost:$client_port" SERVER_URL="http://localhost:$server_port" SERVER_NAME=Target node check.mjs
