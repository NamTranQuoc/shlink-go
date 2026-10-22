#!/usr/bin/env bash
# Runs the benchmark for both targets and stores raw results under bench/results/<run-id>/.
# Usage: bench/run.sh [scenario ...]   (default: all scenarios)
# Env: TARGETS="php go", RUNS=3, WARMUP=30s, DURATION=60s, CAPACITY=1 (step test for max sustainable rate)
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
compose() { docker compose -f bench/compose.yml "$@"; }

targets=${TARGETS:-php go}
runs=${RUNS:-3}
warmup=${WARMUP:-30s}
duration=${DURATION:-60s}
capacity=${CAPACITY:-1}
k6_image=${K6_IMAGE:-grafana/k6:1.3.0}
scenarios=("$@")
if [[ ${#scenarios[@]} -eq 0 ]]; then
  scenarios=(redirect redirect-no-tracking create list short-url-visits tags-stats global-visits mixed)
fi
# Fixed arrival rates (requests/s), low enough for both targets to sustain on 1 CPU; latency is compared at these.
rate_for() {
  case $1 in
    redirect | redirect-no-tracking) echo 200 ;;
    mixed) echo 150 ;;
    create | short-url-visits | global-visits) echo 50 ;;
    list | tags-stats) echo 20 ;;
  esac
}

run_id=$(date +%Y%m%d-%H%M%S)
out="bench/results/$run_id"
mkdir -p "$out"
port() { [[ $1 == php ]] && echo 38080 || echo 38081; }
psql() { docker exec -i shlink-bench-postgres-1 psql -U postgres -v ON_ERROR_STOP=1 -q "$@"; }

compose up -d --wait postgres >/dev/null

# Seed once: upstream creates and migrates the schema, then the seeder fills it and the result is dumped.
if [[ ! -f bench/data/seed.dump ]]; then
  echo "seeding..."
  psql -c "DROP DATABASE IF EXISTS shlink WITH (FORCE)" -c "CREATE DATABASE shlink"
  compose --profile php up -d --wait php >/dev/null
  until curl -fs "http://127.0.0.1:$(port php)/rest/health" >/dev/null; do sleep 1; done
  compose --profile php rm -sf php >/dev/null
  go run ./bench/seed -dsn "postgres://postgres:root@127.0.0.1:35432/shlink?sslmode=disable" -out bench/data
  docker exec shlink-bench-postgres-1 pg_dump -U postgres -Fc -f /tmp/seed.dump shlink
  docker cp shlink-bench-postgres-1:/tmp/seed.dump bench/data/seed.dump
fi
docker cp bench/data/seed.dump shlink-bench-postgres-1:/tmp/seed.dump

restore() {
  psql -c "DROP DATABASE IF EXISTS shlink WITH (FORCE)" -c "CREATE DATABASE shlink"
  docker exec shlink-bench-postgres-1 pg_restore -U postgres -d shlink /tmp/seed.dump
  psql -d shlink -c "ANALYZE"
}

now_ms() { python3 -c 'import time; print(int(time.time()*1000))'; }

# Samples CPU and memory of a container every second until killed.
sample() {
  local container=$1 file=$2
  echo "ts,cpu_percent,mem_bytes" >"$file"
  while true; do
    docker stats --no-stream --format '{{.CPUPerc}},{{.MemUsage}}' "$container" 2>/dev/null |
      awk -F'[,/]' -v ts="$(date +%s)" '{gsub(/%| /,"",$1); m=$2; gsub(/ /,"",m);
        u=1; if (m ~ /GiB$/) u=1073741824; else if (m ~ /MiB$/) u=1048576; else if (m ~ /KiB$/) u=1024;
        gsub(/[A-Za-z]/,"",m); printf "%s,%s,%d\n", ts, $1, m*u}' >>"$file"
  done
}

k6() {
  local target=$1 scenario=$2 r=$3 d=$4 summary=$5
  docker run --rm --network shlink-bench_default -v "$root/bench:/bench:ro" -v "$root/$out:/out" \
    -e BASE_URL="http://$target:8080" -e SCENARIO="$scenario" -e RATE="$r" -e DURATION="$d" \
    "$k6_image" run --quiet --summary-export "/out/$summary" /bench/k6/scenarios.js >/dev/null 2>&1 || true
}

{
  echo "{"
  echo "  \"runId\": \"$run_id\","
  echo "  \"host\": \"$(sysctl -n machdep.cpu.brand_string 2>/dev/null || uname -m), $(sysctl -n hw.ncpu 2>/dev/null) cores, $(( $(sysctl -n hw.memsize 2>/dev/null || echo 0) / 1073741824 )) GB\","
  echo "  \"docker\": \"$(docker info --format '{{.ServerVersion}}, {{.NCPU}} CPU, {{.MemTotal}} bytes')\","
  echo "  \"k6\": \"$k6_image\","
  echo "  \"limits\": \"${APP_CPUS:-1} CPU, ${APP_MEMORY:-512m}\","
  echo "  \"warmup\": \"$warmup\", \"duration\": \"$duration\", \"runs\": $runs,"
  echo "  \"imageSizes\": {\"php\": $(docker image inspect shlinkio/shlink:5.1.8 --format '{{.Size}}'), \"go\": $(docker image inspect shlink-go:dev --format '{{.Size}}')}"
  echo "}"
} >"$out/env.json"

for target in $targets; do
  for scenario in "${scenarios[@]}"; do
    for run in $(seq 1 "$runs"); do
      tag="$target.$scenario.$run"
      echo "== $tag"
      restore
      start=$(now_ms)
      compose --profile "$target" up -d "$target" >/dev/null
      until curl -fs "http://127.0.0.1:$(port "$target")/rest/health" >/dev/null 2>&1; do sleep 0.1; done
      ready=$(now_ms)
      sleep 5
      idle=$(docker stats --no-stream --format '{{.MemUsage}}' "shlink-bench-$target-1" | cut -d/ -f1 | tr -d ' ')
      echo "{\"startupMs\": $((ready - start)), \"idleMemory\": \"$idle\"}" >"$out/$tag.startup.json"

      k6 "$target" "$scenario" "$(rate_for "$scenario")" "$warmup" "$tag.warmup.json"
      sample "shlink-bench-$target-1" "$out/$tag.stats.csv" &
      sampler=$!
      k6 "$target" "$scenario" "$(rate_for "$scenario")" "$duration" "$tag.k6.json"
      kill "$sampler" 2>/dev/null || true
      wait "$sampler" 2>/dev/null || true

      if [[ $capacity == 1 && $run == 1 ]]; then
        # Step test: double the rate until p99 > 500 ms, errors > 1% or the target cannot keep up
        r=$(rate_for "$scenario")
        while (( r <= 20000 )); do
          k6 "$target" "$scenario" "$r" 20s "$tag.capacity-$r.json"
          ok=$(python3 - "$out/$tag.capacity-$r.json" "$r" <<'PY'
import json, sys
s = json.load(open(sys.argv[1]))
m = s["metrics"]
p99 = m["http_req_duration"]["p(99)"]
fails = m["checks"]["fails"] / max(1, m["checks"]["passes"] + m["checks"]["fails"])
achieved = m["http_reqs"]["rate"]
print("1" if p99 <= 500 and fails <= 0.01 and achieved >= 0.95 * float(sys.argv[2]) else "0")
PY
)
          [[ $ok == 1 ]] || break
          r=$((r * 2))
        done
      fi
      compose --profile "$target" rm -sf "$target" >/dev/null
    done
  done
done
echo "results in $out"
