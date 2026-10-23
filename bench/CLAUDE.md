# bench

How the benchmark compares upstream Shlink 5.1.8 and this port, and the rules that keep the comparison fair.

## Topology

- `bench/compose.yml` (project `shlink-bench`): one PostgreSQL 17 container (2 CPU, 2 GB) and one app container at a time, `php` (`shlinkio/shlink:5.1.8`, default RoadRunner runtime) or `go` (`shlink-go:dev`, built with `make image`).
- Both apps get the same limits (`APP_CPUS`, default 1; `APP_MEMORY`, default 1g: at 512m upstream with its default worker count is OOM-killed under load), the same env vars and their default runtime settings (RoadRunner worker count, Go runtime defaults). Do not tune one target only.
- k6 runs in the `grafana/k6` image on the same Docker network.

## Data

- `bench/seed` fills the database after upstream created and migrated it: 5 domains, 200 tags, 100k short URLs, 1M visits (Zipf-distributed popularity, 5% orphan visits, counters tables filled), plus a fake API key used only in the benchmark database. It is deterministic for a seed.
- The seeded database is dumped once to `bench/data/seed.dump` (gitignored) and restored before every measured run, so each run starts from the same state.

## Running

```
make image
bench/run.sh                        # all scenarios, both targets, 3 runs each
TARGETS=go RUNS=1 bench/run.sh redirect
go run ./bench/report               # renders tables from the latest run into README.md
```

- Per run: restore DB, start the target and time it to a healthy `/rest/health`, record idle memory, warm up (`WARMUP`, 30s, discarded), then measure at a fixed arrival rate (`DURATION`, 60s) while `docker stats` is sampled every second.
- On the first run of each scenario a step test doubles the arrival rate (20s per step) until p99 > 500 ms, errors > 1% or the achieved rate falls below 95% of the target; the last passing step is the highest sustained rate.
- Fixed rates per scenario live in `rate_for` in `run.sh`; they must stay within what both targets sustain, so latency is compared under equal load.

## Rules

- Never hand-edit numbers in `bench/results/` or in README tables; rerun the benchmark and `bench/report`.
- Keep raw results (`*.k6.json`, `*.stats.csv`, `*.startup.json`, `env.json`) of the run the README cites.
- Run with nothing else heavy on the machine (no contract suite, builds or other containers under load), and say in the README when that was not possible.
- Report what was measured, including scenarios where the port is not faster.
