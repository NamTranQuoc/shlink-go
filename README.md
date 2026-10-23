# shlink-go

An unofficial Go port of [Shlink](https://github.com/shlinkio/shlink) **v5.1.8**, the self-hosted URL shortener. It is not affiliated with or endorsed by the Shlink project. All credit for the design, the API and the behavior goes to Shlink and its authors; this repository only re-implements it in Go. Shlink is MIT-licensed, and so is this port (see [LICENSE](LICENSE) and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)).

The goal is a drop-in replacement: a Shlink 5.1.8 deployment can switch to this image without other changes, because it keeps the same

- database schema and `migrations` table on MySQL, MariaDB, PostgreSQL, MS SQL Server and SQLite;
- REST API paths, JSON bodies and RFC 7807 error responses;
- env vars, including the `_FILE` suffix fallback;
- CLI command names, options and output;
- Docker image layout (`/etc/shlink`, data in `/etc/shlink/data`, port 8080, `shlink` on the `PATH`).

It adds no features of its own. Where it behaves differently from upstream, that is a bug.

## Running it

The configuration is upstream's: see the [Shlink documentation](https://shlink.io/documentation/) for every env var.

```
docker build -f docker/Dockerfile -t shlink-go .
docker run -p 8080:8080 -e DEFAULT_DOMAIN=s.test -e IS_HTTPS_ENABLED=false -e INITIAL_API_KEY=change-me shlink-go
```

On start, the container runs the same entrypoint as upstream: `shlink-installer init` creates the database if needed, runs the migrations and creates the initial API key, then the server starts. CLI commands run inside the container as with upstream:

```
docker exec -it <container> shlink short-url:create https://example.com
```

To build the binary instead: `make build` writes `bin/shlink` and its installer link `bin/shlink-installer`. `bin/shlink-installer init` creates and migrates the database, then `bin/shlink serve` starts the server.

## Testing and parity evaluation

Parity is checked by a black-box contract suite in [`contract/`](contract/CLAUDE.md) that runs, unchanged, against both the upstream image `shlinkio/shlink:5.1.8` and this port:

```
scripts/contract.sh php postgres        # against upstream
scripts/contract.sh go postgres         # against this port
PROFILE=custom scripts/contract.sh go sqlite
make contract-all TARGET=go             # all five engines
```

What it covers:

| Suite | Content |
|---|---|
| `contract/rest` | ports of all 31 upstream API test files, plus the operations they leave out; a gate fails if any operation in upstream's `swagger.json` is not exercised |
| `contract/cli` | ports of upstream's CLI tests, plus command list and help output gates |
| `contract/diff` | differential tests: responses (status, headers, normalized body) must equal goldens captured from upstream, for protocol edge cases and for non-default configurations (`custom` env profile) |
| `contract/integration` | Mercure, RabbitMQ, Redis pub/sub and Matomo messages compared with upstream |
| `contract/webclient` | [shlink-web-client](https://github.com/shlinkio/shlink-web-client) 4.8.1, driven by Playwright through creating, visiting, editing, listing and deleting short URLs, visits, tags, domains and redirect rules; any unexpected API error fails it (`scripts/webclient.sh <php\|go>`) |
| `contract/schema` | the schema each target creates and migrates (columns, indexes, foreign keys, migrations rows) must equal upstream's, per engine |

Goldens are only ever captured from upstream. The few results where upstream itself depends on undefined SQL ordering are listed per engine in `contract/internal/harness/known_failures.go` and skipped for both targets.

Unit tests (`make test`) pin the PHP semantics the port reproduces, using outputs produced by the upstream image: `json_encode` flags, `parse_str`, Guzzle URI handling, Valinor mapping rules, JWTs, Matomo requests, and the user agent and crawler rules (about 170k user agents compared).

### Current results

<!-- matrix:start -->
Run `20261022-233801`. Each cell is upstream / port; – means not run or no tests for that combination.

| Suite | Profile | SQLite | PostgreSQL | MySQL | MariaDB | MS SQL |
|---|---|---|---|---|---|---|
| rest | default | pass / pass | pass / pass | pass / pass | pass / pass | pass / pass |
| cli | default | pass / pass | pass / pass | pass / pass | pass / pass | pass / pass |
| diff | default | pass / pass | pass / pass | pass / pass | pass / pass | pass / pass |
| integration | default | pass / pass | pass / pass | pass / pass | pass / pass | pass / pass |
| schema | default | pass / pass | pass / pass | pass / pass | pass / pass | pass / pass |
| diff | custom | pass / pass | pass / pass | pass / pass | pass / pass | pass / pass |
| schema | custom | pass / pass | pass / pass | pass / pass | pass / pass | pass / pass |
| integration | integrations | pass / pass | pass / pass | pass / pass | pass / pass | pass / pass |
<!-- matrix:end -->

## Benchmark

Method, fairness rules and how to rerun it are in [`bench/CLAUDE.md`](bench/CLAUDE.md). In short: both images run with the same limits (1 CPU, 1 GiB; at 512 MB upstream with its default worker count is OOM-killed under load) against the same PostgreSQL 17 instance, restored before every run from the same seed (100k short URLs, 1M visits, 200 tags, 5 domains); k6 drives eight scenarios at a fixed arrival rate (median of three runs), then a step test finds the highest sustained rate (p99 ≤ 500 ms, errors ≤ 1%). The tables below are generated from the raw results in `bench/results/` by `go run ./bench/report`.

<!-- bench:start -->
Run `20261023-003134` on Apple M1 Pro, 8 cores, 16 GB; Docker 29.8.2, 8 CPU, 8319770624 bytes; app limits 1 CPU, 1g; k6 `grafana/k6:1.3.0`; warm-up 30s, measured 60s, median of 3 runs.

#### Latency at a fixed arrival rate

| Scenario | Target | RPS | p50 (ms) | p95 (ms) | p99 (ms) | Errors | CPU avg / peak | Memory avg / peak |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| redirect | Shlink 5.1.8 (PHP, RoadRunner) | 50 | 11.8 | 13.7 | 19.3 | 0.00% | 30% / 56% | 497 MiB / 504 MiB |
| redirect | shlink-go | 50 | 3.5 | 6.2 | 7.7 | 0.00% | 9% / 11% | 20 MiB / 21 MiB |
| redirect-no-tracking | Shlink 5.1.8 (PHP, RoadRunner) | 75 | 9.4 | 10.8 | 39.6 | 0.00% | 39% / 77% | 482 MiB / 484 MiB |
| redirect-no-tracking | shlink-go | 75 | 2.1 | 3.9 | 5.5 | 0.00% | 9% / 10% | 16 MiB / 17 MiB |
| create | Shlink 5.1.8 (PHP, RoadRunner) | 25 | 12.8 | 14.4 | 17.8 | 0.00% | 17% / 37% | 509 MiB / 513 MiB |
| create | shlink-go | 25 | 5.8 | 8.4 | 10.8 | 0.00% | 8% / 9% | 14 MiB / 14 MiB |
| list | Shlink 5.1.8 (PHP, RoadRunner) | 10 | 26.9 | 38.3 | 50.8 | 0.00% | 10% / 11% | 504 MiB / 504 MiB |
| list | shlink-go | 10 | 17.6 | 30.0 | 50.7 | 0.00% | 2% / 3% | 13 MiB / 14 MiB |
| short-url-visits | Shlink 5.1.8 (PHP, RoadRunner) | 10 | 28.4 | 143.5 | 147.9 | 0.00% | 7% / 9% | 487 MiB / 492 MiB |
| short-url-visits | shlink-go | 10 | 11.7 | 135.0 | 167.5 | 0.00% | 2% / 2% | 13 MiB / 13 MiB |
| tags-stats | Shlink 5.1.8 (PHP, RoadRunner) | 2 | 193.2 | 199.9 | 204.1 | 0.00% | 3% / 4% | 468 MiB / 470 MiB |
| tags-stats | shlink-go | 2 | 181.4 | 189.4 | 191.3 | 0.00% | 0% / 1% | 12 MiB / 13 MiB |
| global-visits | Shlink 5.1.8 (PHP, RoadRunner) | 5 | 58.6 | 64.7 | 69.1 | 0.00% | 4% / 5% | 468 MiB / 469 MiB |
| global-visits | shlink-go | 5 | 50.6 | 57.5 | 59.3 | 0.00% | 1% / 1% | 13 MiB / 13 MiB |
| mixed | Shlink 5.1.8 (PHP, RoadRunner) | 50 | 12.0 | 28.4 | 120.8 | 0.00% | 31% / 70% | 522 MiB / 528 MiB |
| mixed | shlink-go | 50 | 2.5 | 17.5 | 112.3 | 0.00% | 6% / 9% | 21 MiB / 21 MiB |

#### Highest sustained arrival rate (p99 ≤ 500 ms, errors ≤ 1%)

| Scenario | Shlink 5.1.8 (PHP, RoadRunner) | shlink-go | Ratio |
|---|---:|---:|---:|
| redirect | 100 req/s | 3200 req/s | 32.0x |
| redirect-no-tracking | 75 req/s | 4800 req/s | 64.0x |
| create | 50 req/s | 1600 req/s | 32.0x |
| list | 80 req/s | 80 req/s | 1.0x |
| short-url-visits | 20 req/s | 40 req/s | 2.0x |
| tags-stats | 4 req/s | 4 req/s | 1.0x |
| global-visits | 40 req/s | 20 req/s | 0.5x |
| mixed | 100 req/s | 200 req/s | 2.0x |

#### Footprint

| Metric | Shlink 5.1.8 (PHP, RoadRunner) | shlink-go |
|---|---:|---:|
| Image size | 430 MB | 48 MB |
| Start to healthy (median) | 2922 ms | 1004 ms |
| Idle memory (median) | 385 MiB | 9 MiB |
<!-- bench:end -->

Machine: MacBook Pro, Apple M1 Pro (8 cores), 16 GB RAM, macOS 26.6.2, Docker Desktop 29.8.2 with an 8 CPU / 8 GB VM. Nothing else ran during the measurement apart from macOS background services.

Findings, as measured:

- **Redirects and short URL creation**, where the application itself does the work: at the same load the port's p99 is 1.6–7 times lower with a quarter to a half of upstream's CPU, and its highest sustained rate is 32–64 times upstream's (upstream reaches 100 redirects/s, the port 3200).
- **Database-bound scenarios** (`list`, `tags-stats`, `short-url-visits`, `mixed`): both targets wait on the same PostgreSQL (2 CPU), so p99 at the fixed rate is within 15% (the port's is higher for `short-url-visits`, 168 vs 148 ms) and the port sustains 1–2 times upstream's rate.
- **`global-visits` is the one scenario where the port is not faster at saturation**: it is slightly faster at the fixed rate (p50 51 vs 59 ms) but sustains 20 requests/s against upstream's 40. The SQL is the same; the difference is in how it reaches PostgreSQL. The port reuses prepared statements, so PostgreSQL runs the bot-filtered count with a generic plan, which is parallel (faster alone, 24 vs 43 ms, but three backend processes per query), and the port keeps up to 32 queries in flight where upstream has at most its 8 RoadRunner HTTP workers (measured at 40 requests/s: about 30 active queries plus parallel workers, against 7 for upstream). Past the database's two CPUs, that costs throughput.
- **Footprint**: the image is about 9 times smaller, the port is healthy about 3 times sooner after start, and it idles at 9 MiB against 385 MiB.

Threats to validity: the load generator, the database and the target share one machine and Docker Desktop's VM, so absolute numbers are specific to this machine; the comparison between targets under equal limits is the result. Upstream runs with its default RoadRunner worker count, which is sized from the CPUs the container sees (8), not from its CPU limit; that is why it needs more than 512 MB and why the limit is 1 GiB. The fixed rates are about half of what upstream sustains with a low p99, so the latency tables compare both targets well below their limits.

## Differences from upstream

Known differences, none of which changes what an API client, the web client or a script reading normal CLI output sees in normal use:

- **PHP internals do not exist.** Verbose (`-v`) CLI output does not include PHP exception traces, `init --download-rr-binary` prints a warning (there is no RoadRunner binary), and the installer's `-vv`/`-vvv` child process debug output is not reproduced.
- **`config/params/*.php` must be data.** The port reads the files the installer writes (literal arrays and scalars) and refuses any other PHP code with an error naming the line; upstream would execute it.
- **Installer type errors and endless prompts.** Where upstream's strict types throw a `TypeError` on unusual installer answers (an empty CORS allowlist, an empty trusted-proxies list, a non-integer redirect status in an imported config), or re-ask forever after input runs out, the port fails with a plain error instead. Installer messages for failed file writes are close to upstream's but not identical.
- **Bit.ly import** is ported but has no contract test, because the importer calls the real Bit.ly API.
- **Locks for new tags and domains are released when the short URL is saved.** Upstream means to do the same, but its relation resolver only releases them during the first request each RoadRunner worker serves; after that they expire after 5 s with Redis, and without Redis they stay held until the same worker locks that name again, which makes requests on other workers that use the tag wait indefinitely.
- **Very large request bodies.** Above about 264 MB, upstream answers 500 because RoadRunner cannot pass the request to the PHP worker; the port handles them like any other body, up to the same 1000 MiB limit.
- **The GeoLite2 download from MaxMind is not verified** against the real endpoint, because that needs a MaxMind license key; it is tested against a local server.

Report any other behavior that differs from Shlink 5.1.8 as a bug.

## License

MIT. The upstream copyright notice is kept in [LICENSE](LICENSE); ported third-party code and data are credited in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
