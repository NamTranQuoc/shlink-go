#!/usr/bin/env bash
# Runs the contract suite against one implementation (php = upstream image, go = this port) on one DB engine.
# Usage: scripts/contract.sh <php|go> <sqlite|postgres|mysql|maria|mssql> [go test args...]
set -euo pipefail

target=${1:?target php|go}
db=${2:?db engine}
shift 2

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root/docker"

files=(-f compose.yml)
if [[ $target == php && $db == mssql ]]; then
  files+=(-f compose.php-amd64.yml)
fi

run_id="$target-$db-$(date +%s)"
data_dir="$root/var/run/$run_id"
mkdir -p "$data_dir"
if [[ $target == php ]]; then export PHP_DATA_DIR=$data_dir; else export GO_DATA_DIR=$data_dir; fi

# PROFILE selects docker/env/profiles/<name>.env: extra env vars for both targets, used by profile-specific tests.
profile=${PROFILE:-default}
export PROFILE_ENV_FILE="env/profiles/$profile.env"
env_files=(--env-file "env/$db.env")
if [[ -f $PROFILE_ENV_FILE ]]; then env_files+=(--env-file "$PROFILE_ENV_FILE"); fi

profiles=(--profile "$target")
if [[ $db != sqlite ]]; then
  profiles+=(--profile "$db")
fi
if [[ $profile == integrations ]]; then
  profiles+=(--profile integrations)
fi

base_path=$(sed -n 's/^BASE_PATH=//p' "$PROFILE_ENV_FILE" 2>/dev/null || true)

compose() { docker compose "${files[@]}" "${env_files[@]}" "${profiles[@]}" "$@"; }

# Recreate only this target's container; the other target and the DB containers may be in use by another run.
compose rm -sf "shlink-$target" >/dev/null 2>&1 || true
if [[ $target == go ]]; then
  compose build shlink-go >/dev/null
fi
# Start from an empty database, so the target creates and migrates the schema itself.
if [[ $db != sqlite ]]; then
  compose up -d --wait "$db" >/dev/null
  case $db in
    postgres) docker exec "shlink-go-$db-1" psql -U postgres -qc "DROP DATABASE IF EXISTS shlink WITH (FORCE)" ;;
    mysql | maria) docker exec "shlink-go-$db-1" sh -c 'if command -v mysql >/dev/null; then cli=mysql; else cli=mariadb; fi; $cli -uroot -proot -e "DROP DATABASE IF EXISTS shlink" 2>/dev/null' ;;
    mssql) docker exec "shlink-go-$db-1" /opt/mssql-tools18/bin/sqlcmd -C -S localhost -U sa -P 'Passw0rd!' -Q \
      "IF DB_ID('shlink') IS NOT NULL BEGIN ALTER DATABASE shlink SET SINGLE_USER WITH ROLLBACK IMMEDIATE; DROP DATABASE shlink; END" >/dev/null ;;
  esac
fi
compose up -d --wait

container="shlink-go-shlink-$target-1"
for _ in $(seq 1 60); do
  if curl -fs "http://127.0.0.1:$([[ $target == php ]] && echo 18080 || echo 28080)$base_path/rest/health" >/dev/null; then break; fi
  sleep 1
done

case $db in
  sqlite) dsn="file:$data_dir/database.sqlite?_pragma=busy_timeout(10000)" ;;
  postgres) dsn="postgres://postgres:root@127.0.0.1:15432/shlink?sslmode=disable" ;;
  mysql) dsn="root:root@tcp(127.0.0.1:13306)/shlink" ;;
  maria) dsn="root:root@tcp(127.0.0.1:13307)/shlink" ;;
  mssql) dsn="sqlserver://sa:Passw0rd!@127.0.0.1:11433?database=shlink&TrustServerCertificate=true" ;;
esac

export SHLINK_TARGET_URL="http://127.0.0.1:$([[ $target == php ]] && echo 18080 || echo 28080)"
export SHLINK_CLI="docker exec -i $container shlink"
export CONTRACT_DB_DRIVER=$db
export CONTRACT_DB_DSN=$dsn
export CONTRACT_TARGET=$target
export CONTRACT_PROFILE=$profile
# Coverage gates only make sense when the whole suite runs
if [[ " $* " == *" -run "* ]]; then export CONTRACT_SKIP_COVERAGE=1; fi

cd "$root"
status=0
packages=./contract/...
# Non-default profiles run only the suites written for them. The rest and cli suites port upstream's tests, which
# assume the default configuration (no base path, default slug and redirect settings).
# The integrations profile runs only its own checks: its async listeners keep writing to the database while the other
# suites purge it between tests, and those suites do not depend on the integrations.
case $profile in
  default) ;;
  integrations) packages=./contract/integration/... ;;
  *) packages="./contract/diff/... ./contract/schema/..." ;;
esac
# shellcheck disable=SC2086 # packages is a list
go test -count=1 -p 1 "$@" $packages || status=$?

if [[ ${KEEP_STACK:-0} != 1 ]]; then
  (cd "$root/docker" && compose rm -sf "shlink-$target" >/dev/null 2>&1) || true
fi
exit $status
