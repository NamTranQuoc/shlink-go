# Shlink Go clone — implementation plan

Plan for re-implementing Shlink v5.1.8 in Go as a drop-in replacement, proving feature parity with a shared contract suite, and benchmarking both.

## Goal

Build a Go implementation of [Shlink](https://github.com/shlinkio/shlink) pinned to tag **v5.1.8** (latest stable on 2026-10-21) that an existing Shlink deployment can switch to by changing only the Docker image. Measure speed and resource use of both under identical conditions and publish the results in `README.md`.

- Public repo `https://github.com/NamTranQuoc/shlink-go` (remote `origin`, branch `main`). Go module path: `github.com/NamTranQuoc/shlink-go`. This file is the plan and the progress record; it lives in the repo at `docs/plan.md`.
- Upstream source for reading: `reference/shlink` (shallow clone of v5.1.8) and `reference/shlink-vendor` (its `vendor/` tree), gitignored and never pushed; `make reference` recreates them. Behavior is derived from reading this source, `docs/swagger/swagger.json` and `docs/async-api/async-api.json`.
- The project is a port only: it reproduces upstream behavior in Go and adds no features of its own.

## License and public-repo compliance

- `LICENSE`: MIT, same as upstream. It keeps the upstream notice `Copyright (c) 2016-2024 Alejandro Celaya` (from `reference/shlink/LICENSE`) and adds a line for the Go port's author, followed by the unchanged MIT permission text.
- `THIRD_PARTY_NOTICES.md`: full license text for every project whose code or data is ported rather than imported as a Go dependency (16 entries: the UA/crawler rule sets, Symfony Console/String/Lock/Mercure, lcobucci/jwt, Matomo tracker, the shlinkio packages, laminas-stdlib, safe-writer). A port of a new third-party file adds its entry in the same change.
  Go module dependencies keep their own licenses through `go.mod`; `make licenses` (go-licenses) lists them and fails on any non-permissive license.
- No GeoLite2 database is committed (MaxMind EULA forbids redistribution). MaxMind test `.mmdb` files are committed only after checking each file's license in `maxmind/MaxMind-DB` (repo is Apache-2.0); otherwise tests download them at run time.
- `README.md` states at the top: unofficial Go port of Shlink v5.1.8, not affiliated with or endorsed by the Shlink project, with a link to upstream and its license.
- Each Go file ported from a specific upstream file carries no per-file header; attribution lives in `LICENSE` and `THIRD_PARTY_NOTICES.md` only.

## CLAUDE.md files

Written in phase 0 and kept current:

- `CLAUDE.md` (root): what the project is (public, MIT, unofficial Go port of Shlink v5.1.8, port only — no new features, upstream behavior wins any disagreement), where upstream source lives (`reference/shlink`, gitignored), layout, build/test/contract/bench commands, drop-in rules (schema, JSON, env vars, CLI names must match), license rules (keep `LICENSE` and `THIRD_PARTY_NOTICES.md` in sync when porting third-party code or data), secret rules (no real keys, tokens, license keys or `.env` in the repo; dev credentials only in compose files and obviously fake).
- `contract/CLAUDE.md`: how the suite targets either implementation, fixture porting rules, coverage gates, how to add a test.
- `bench/CLAUDE.md`: fairness rules, how to run and regenerate README tables, never hand-edit result numbers.

## Scope (v5.1.8, everything below is in)

| Area | Upstream surface to match |
|---|---|
| Public routes | `/{shortCode}` redirect, `/{shortCode}/track` pixel, `/robots.txt`, `/rest/health`, special redirects (base URL, invalid short URL, regular 404) |
| REST API | `/rest/v{1,2,3}` prefix; short URLs (CRUD, `/shorten` single-step, list with filters/paging/ordering), visits (per short URL, tag, domain, global, orphan, non-orphan, deletes), tags (list, stats, rename, delete), domains (list, redirects), redirect rules (list, set), `/mercure-info` |
| HTTP behavior | `X-Api-Key` auth, API key roles `AUTHORED_SHORT_URLS` / `DOMAIN_SPECIFIC` / `NO_ORPHAN_VISITS`, RFC 7807 problem details, CORS, request id, implicit OPTIONS, body parsing, trusted proxies / client IP |
| Redirect logic | short code generation (nanoid alphabet + length), custom slugs, multi-segment slugs, trailing slash, `SHORT_URL_MODE`, extra path mode, `REDIRECT_STATUS_CODE` + cache headers, max visits / valid since / valid until, crawlable flag, title auto-resolve |
| Redirect rules | conditions `device`, `language`, `query-param`, `any-value-query-param`, `valueless-query-param`, `ip-address`, `geolocation-country-code`, `geolocation-city-name`, `before-date`, `after-date`, `browser` |
| Visit tracking | visit + orphan visit recording, all `DISABLE_*TRACKING*` options, IP anonymization, crawler detection, UA parsing (device/browser), visit counters tables |
| CLI | all 31 commands in `module/CLI/src` (`api-key:*`, `db:create`, `db:migrate`, `domain:*`, `env-var:read`, `integration:matomo:send-visits`, `short-url:*`, `tag:*`, `visit:*`), `short-url:import`, and the `shlink-installer` commands (`init`, `install`, `update`, `set-option`) |
| Config | all 67 env vars in `module/Core/src/Config/EnvVars.php`, including the `_FILE` suffix fallback |
| Persistence | MySQL, MariaDB, PostgreSQL, MS SQL Server, SQLite; schema identical to the one produced by the 19 Doctrine migrations in `module/Core/migrations`, same migrations table and version ids |
| Geolocation | GeoLite2 download with `GEOLITE_LICENSE_KEY`, `visit:locate`, `visit:download-db`, async location of new visits |
| Real-time updates | Mercure, RabbitMQ, Redis pub/sub; topics `new-visit`, `new-visit/{shortCode}`, `new-orphan-visit`, `new-short-url`; payloads per `async-api.json`; `REAL_TIME_UPDATES_TOPICS` filter |
| Other integrations | Redis/Valkey cache and locks (incl. sentinel), Matomo visit forwarding, importers Bitly / YOURLS / Kutt / CSV / Shlink |
| Docker image | port 8080, `/etc/shlink` layout and data dirs, `shlink` CLI on `PATH`, entrypoint doing `init` then serve, same env vars |

Out of scope: shlink-web-client (separate repository). It is used as a compatibility check against the Go server.

## Architecture

Single static binary `shlink`, where `shlink serve` (also the default with no arguments) runs the HTTP server and every other argument is a CLI command with the upstream name (`shlink short-url:list ...`). `shlink-installer` is a second entry point in the same binary (argv[0] or subcommand).

```
cmd/shlink/             entry point, service container, serve, CLI dispatch
internal/config/        env vars (_FILE fallback, PHP-style casting), option structs
internal/db/            database/sql per driver (pgx, go-sql-driver/mysql, go-mssqldb, modernc sqlite), dialect helpers,
                        embedded Doctrine DDL per engine, ported migrations
internal/core/          short URLs, visits, tags, domains, redirect rules, API keys: repositories + services
internal/web/           FastRoute-compatible router, Mezzio middleware pipeline, REST + public actions, problem details
internal/console/       symfony/console-compatible framework
internal/cli/           the Shlink CLI commands
internal/integration/   events (async listeners), realtime (Mercure/RabbitMQ/Redis), lock, matomo, geolocation, title
internal/phpx/          PHP semantics: json_encode flags, urlencode/parse_str, Guzzle URIs, date parsing
internal/valinor/       the CuyZ/Valinor mapping rules upstream relies on
internal/pcre/          backtracking regex engine with PCRE semantics for ported UA/crawler patterns
internal/useragent/  internal/crawler/   ports of donatj/phpuseragentparser and jaybizzle/crawler-detect
contract/               black-box contract suite (see below)
bench/                  benchmark harness (see below)
docker/                 Dockerfile, compose stack, per-DB and per-profile env files
```

- DB access: plain `database/sql` with hand-written, dialect-aware SQL that mirrors the queries Doctrine generates (including `DISTINCT`, subqueries and lock modes), because result order and edge cases must match upstream on every engine. The schema is created from the exact `orm:schema-tool:create --dump-sql` output of upstream for each engine; migrations are ported one by one, recorded in the same `migrations` table with the same version ids, and skipped migrations stay unrecorded like Doctrine does.
- HTTP: a port of Mezzio's pipeline (path-segregated middlewares, implicit HEAD/OPTIONS, problem details with JSON/XML negotiation) over a port of FastRoute's GroupCountBased dispatcher, served by `net/http` the way RoadRunner hands requests to PHP (static `public/` files first, parse_str query, form bodies pre-parsed).
- Async work that RoadRunner jobs do upstream runs in a bounded goroutine queue drained on shutdown.
- UA parsing and crawler detection port the rule data of `donatj/phpuseragentparser` and `jaybizzle/crawler-detect` and run them on `internal/pcre`, so classifications match exactly (verified against ~170k user agents).

## Proof of parity: contract suite

`contract/` is a Go test package that talks only HTTP, CLI and the target's database. The same suite runs against either implementation (`scripts/contract.sh <php|go> <engine>`, settings passed as `SHLINK_TARGET_URL`, `SHLINK_CLI`, `CONTRACT_DB_*`). Each run recreates only the target container and starts from an empty database, so the target creates and migrates the schema itself.

1. Fixtures: upstream's fixtures (`module/Rest/test-api/Fixtures`, `module/CLI/test-cli/Fixtures`) ported to Go seeding code that writes through plain SQL per engine; the shared schema lets the same seed load for both implementations.
2. Tests:
   - `rest`: ports of all 31 upstream API test files (`module/Rest/test-api`, `module/Core/test-api`), plus tests for the operations they leave out (pixel, mercure-info).
   - `cli`: ports of the 8 files in `module/CLI/test-cli` plus command-list and help gates.
   - `diff`: differential suite. Requests listed per env profile (`diff/testdata/<profile>.cases.json`); status, selected headers and normalized body must equal goldens captured from upstream. It covers protocol edge cases (Accept negotiation, XML problem details, malformed/odd bodies, parse_str queries, HEAD/OPTIONS, encoded paths, case of the `/rest` prefix) and non-default configurations.
   - `integration`: Mercure, RabbitMQ, Redis pub/sub and Matomo messages compared between targets.
3. Env profiles (`docker/env/profiles/<name>.env`, selected with `PROFILE=`): `default`, `custom` (base path, multi-segment slugs, trailing slash, loose mode, 301 + cache headers, extra path append, not-found redirects with placeholders, HTTPS, CORS origins + credentials, robots), `integrations`.
4. Gates, enforced by tests in the suite:
   - every operation in `swagger.json` is exercised by at least one test;
   - `shlink list` output (command names + options) matches upstream;
   - the env var set matches `EnvVars.php` (checked through `env-var:read`);
   - schema gate: columns, indexes, foreign keys and migrations rows introspected from the target equal the golden captured from upstream for each engine.
5. Matrix: suite × {upstream `shlinkio/shlink:5.1.8`, Go} × {PostgreSQL, MySQL, MariaDB, MSSQL, SQLite}. Upstream on MSSQL runs as the amd64 image (the arm64 build has no `pdo_sqlsrv`). Results that depend on undefined SQL ordering in upstream itself (tie order on SQLite and MSSQL) are listed per engine in `contract/internal/harness/known_failures.go` and skipped for both targets.
6. Goldens are only captured from upstream (`CONTRACT_UPDATE_GOLDEN=1` / `CONTRACT_UPDATE_SCHEMA=1` with `TARGET=php`); the tests refuse to write them from the port.
7. Web client check: `contract/webclient/check.mjs` (Playwright) drives shlink-web-client 4.8.1 through create, visit, list, visits, edit, redirect rules, tags, domains, orphan visits and delete; `scripts/webclient.sh <php|go>` runs it.
8. Profiles scope the suites: `default` runs everything, `integrations` runs only `integration`, any other profile runs only `diff` and `schema` (the ported `rest` and `cli` tests assume the default configuration).

The suite is first made fully green against upstream. That validates the suite itself before it judges the Go code.

Go unit tests cover logic-heavy parts in isolation and pin PHP semantics with outputs produced by the upstream image (json_encode, JWT, Matomo request bodies, UA/crawler goldens).

## Benchmark

`bench/` holds a compose file, seed generator, k6 scenarios, the runner (`bench/run.sh`) and the report generator (`bench/report`). Rules for running it live in `bench/CLAUDE.md`.

- Topology: one PostgreSQL 17 container (2 CPU / 2 GB), one app container at a time, k6 (`grafana/k6:1.3.0`) on the same Docker network. All on Docker Desktop on one machine; README records the machine the published numbers come from.
- Targets: `shlinkio/shlink:5.1.8` (default RoadRunner runtime and worker count) and the Go image, each with default runtime settings.
- Fairness: identical CPU and memory limits on the app container (1 CPU / 1 GiB; at 512 MB upstream with its default RoadRunner worker count is OOM-killed under load), same Postgres instance and config, same seed (100k short URLs, 1M visits, 200 tags, 5 domains; deterministic), DB restored from the same dump before each run, 30 s warm-up excluded from results, same env vars (geolocation and real-time updates off).
- Scenarios:
  1. redirect hot path with visit tracking;
  2. redirect with tracking disabled (`DISABLE_TRACK_PARAM`);
  3. create short URL (`POST /rest/v3/short-urls`);
  4. list short URLs with search, tag filter and paging;
  5. short URL visits list;
  6. tags stats;
  7. global visits stats (`/rest/v3/visits`);
  8. mixed workload (90% redirects, 10% API).
- Load: a fixed arrival rate per scenario that both targets sustain (latency comparison at equal load), then a step test that doubles the arrival rate (20 s per step) until p99 > 500 ms, errors > 1% or the achieved rate drops below 95% of the target; the last passing step is the highest sustained rate.
- Metrics: RPS, p50/p95/p99 latency, error rate, CPU avg/peak, memory avg/peak (sampled from `docker stats` every second), idle memory, image size, time from container start to healthy `/rest/health`.
- Each scenario runs 3 times at the fixed rate and the report uses the median; the step test runs once per scenario and target. Raw k6 JSON and sampler CSV are kept under `bench/results/<run-id>/`; `go run ./bench/report` renders the README tables from them.

## README sections

1. What the project is and the upstream version it matches.
2. Running it (Docker image, binary, env vars — same as upstream docs).
3. Feature parity: scope table, how the contract suite works, current matrix result, how to run it.
4. Benchmark: method, environment (machine, Docker limits, versions), results tables per scenario, resource table, findings stated as measured, including any scenario where Go is not faster, and threats to validity (Docker Desktop noise, single host for load generator and target).
5. Differences from upstream, if any remain at the end, each with the reason.

## Phases and exit criteria

| # | Phase | Exit criterion |
|---|---|---|
| 0 | Scaffold: `git init` + remote `origin`, `.gitignore` (`reference/`, `.env*` except `.env.example`, `data/`, `*.mmdb`, build output), module, layout, Makefile, compose with 5 DBs + upstream image, contract harness, `LICENSE`, `THIRD_PARTY_NOTICES.md`, the three `CLAUDE.md` files | `make contract TARGET=php DB=sqlite` runs a health test green |
| 1 | Contract suite | all ported + added tests and coverage gates green against upstream on all 5 engines |
| 2 | Config + DB: env vars, connections, migrations, `db:create`, `db:migrate`, `env-var:read` | schema-diff gate green on all 5 engines |
| 3 | Short URLs, redirects, visit tracking, redirect rules, robots, pixel | redirect + visit tests green for Go |
| 4 | REST API complete | all REST contract tests green for Go |
| 5 | CLI complete, incl. importer command and installer | all CLI contract tests + command-list gate green for Go |
| 6 | Integrations: GeoLite2, Mercure, RabbitMQ, Redis, Matomo, importer sources | integration tests green for Go |
| 7 | Docker image drop-in + web client check | full matrix green for Go on all 5 engines; Playwright web client script passes |
| 8 | Benchmark + README | results saved under `bench/results/`, README complete |
| 9 | Security review, license check, publish | review report has no open high/critical finding; secret scan clean; license check clean; then push after approval (see below) |

Done = phase 7 matrix fully green for both targets, phase 8 README written from measured data, phase 9 report clean and pushed.

### Status

| Phase | State |
|---|---|
| 0–6 | Done. REST (31 ported files + pixel, mercure-info), CLI (all 32 commands incl. `short-url:import` with 5 sources and installer `init`/`install`/`update`/`set-option`), differential, integration and schema suites; swagger and command-list gates. |
| 7 | Done: the matrix (3 profiles × 5 engines × 2 targets, `scripts/matrix.sh`) passes in all 30 runs; the table is in README. Web client check passes for both targets. |
| 8 | Done: run `bench/results/20261023-003134` (48 runs, no target crashed), tables and findings in README. |
| 9 | Done up to publishing: secret scan, scanners, license check and manual review are clean (checklist below); commit and push wait for approval. |

Phase 9 checklist:

- [x] gitleaks `dir` mode: clean outside `reference/` (gitignored). Two fake Mercure JWTs in `internal/integration/realtime/mercure_test.go` are marked `gitleaks:allow`.
- [x] gitleaks `git` mode over the pushed commit.
- [x] govulncheck with `toolchain go1.27.3`: no reachable vulnerability. Only GO-2026-5932 (`golang.org/x/crypto/openpgp`, no fix) is in a required module; the code does not import it.
- [x] gosec: 34 findings, all triaged as not exploitable (env var names, config-path file reads as upstream, upstream's file modes, `math/rand` only for counter slots, temp names and lock retry jitter, md5 for Matomo ids as upstream). Short codes and API keys use `crypto/rand`.
- [x] Request body limit: 1000 MiB with RoadRunner's 413 response, measured against upstream (exactly 1048576000 bytes passes).
- [x] `make licenses` clean.
- [x] Manual review (step 2 below), report at `~/Desktop/memory/shlink/doc/security-review.md`: no open high or critical finding; auth and roles identical to upstream on every REST operation (368-request probe, SQLite and PostgreSQL); upstream weaknesses kept are listed there.

Open items:

- D2: the benchmark runs both apps at 1 CPU / 1 GiB instead of the planned 512 MB, because upstream with its default RoadRunner worker count is OOM-killed under load at 512 MB. Awaiting confirmation. Owner: Nam.

## Resuming on another machine

1. Prerequisites: Go (the toolchain line in `go.mod` downloads 1.27.3), Docker with at least 8 CPU / 8 GB for the VM, Node 20+ and Google Chrome for the web client check, Python 3.
2. `git clone https://github.com/NamTranQuoc/shlink-go && cd shlink-go && make reference` (needs Docker; pulls `shlinkio/shlink:5.1.8`).
3. Sanity check: `make test lint`, `make image`, `scripts/contract.sh go sqlite`.
4. Full checks: `scripts/matrix.sh` (about 40 min), `BROWSER_CHANNEL=chrome scripts/webclient.sh go|php`, and `make bench` then `make bench-report` on a quiet machine (about 2.5 h; read `bench/CLAUDE.md` first).

## Phase 9: security review and publish

1. Secret scan of the working tree and of every commit about to be pushed: `gitleaks` (Docker image `zricethezav/gitleaks`) in `dir` and `git` modes, plus a manual pass over compose files, fixtures, `bench/results/`, test data and docs for hosts, tokens, API keys, MaxMind keys, Mercure JWT secrets, RabbitMQ/Redis passwords, and personal data. Upstream fixture API keys and compose dev passwords stay only if they are clearly fake; each one goes into `.gitleaks.toml` allowlist with a reason.
2. Code review for security: `govulncheck ./...`, `gosec ./...`, and a manual review of:
   - API key auth and role enforcement on every REST route (`AUTHORED_SHORT_URLS`, `DOMAIN_SPECIFIC`, `NO_ORPHAN_VISITS`), API key storage (hashed as upstream does);
   - SQL built from request input (ordering, filters, search) — parameterized or allowlisted only;
   - outbound HTTP (title auto-resolve, importers, Matomo, GeoLite2 download): timeouts, size limits, redirect limits;
   - header handling (trusted proxies / client IP, CORS, redirect `Location`), error responses not leaking internals;
   - Docker image: non-root user (`USER_ID=1001` as upstream), no secrets baked in.
   Where upstream has the same weakness, the port keeps upstream behavior and the README lists it under differences only if it was changed.
3. License check: `LICENSE` and `THIRD_PARTY_NOTICES.md` cover all ported code and data; `make licenses` clean; README disclaimer present.
4. Write the report to `~/Desktop/memory/shlink/doc/security-review.md` and stop for approval.

Commits and pushes happen only when asked; every push is preceded by the secret scan of step 1.

## Prerequisites and risks

- Docker Desktop VM: 8 CPU / 8 GB / 62 GB disk, enough for all five engines, both targets and the benchmark.
- MSSQL on Apple Silicon runs `mcr.microsoft.com/mssql/server:2022-latest` and the upstream image under amd64 emulation; those runs are slower but results are compared, not timed.
- GeoLite2 download tests need a MaxMind license key; without one, geolocation is verified with MaxMind's public test `.mmdb` (not committed, pointed to by `SHLINK_GEOIP_TEST_DB`) and the download path stays unverified against MaxMind.
- Load generator and target share one machine, so absolute numbers are machine-specific; the comparison between targets under the same limits is what the README reports.

## Key decisions locked

- Parity target is Shlink v5.1.8; later upstream releases are out of scope.
- Drop-in compatibility: same DB schema and migrations table, same JSON and error format, same env vars, same CLI names, same Docker layout.
- All 5 DB engines supported and tested.
- Done is judged by one Go contract suite (REST, CLI, differential, integration, schema) that passes against both implementations on all engines and env profiles, plus swagger/CLI/env/schema gates.
- QR code generation is not in scope: upstream removed it in 5.0.0 (CHANGELOG #2514).
- REA is not used; behavior comes from reading upstream source.
- Benchmark: k6 + `docker stats`, local Docker Desktop, upstream on RoadRunner, equal container limits (1 CPU / 1 GiB), median of 3 runs, step test for highest sustained rate.
- Database access is hand-written SQL over `database/sql` mirroring Doctrine's queries; routing is a FastRoute port, not a Go router library.
- Public repo `NamTranQuoc/shlink-go`, MIT with upstream copyright kept, third-party ported code/data credited in `THIRD_PARTY_NOTICES.md`.
- `reference/` (upstream source) is gitignored and never pushed; no GeoLite2 database is committed.
- Commits and pushes only on request, each push after a clean secret scan.
