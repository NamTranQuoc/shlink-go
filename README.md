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
Run `20261022-220338`. Each cell is upstream / port; – means not run or no tests for that combination.

| Suite | Profile | SQLite | PostgreSQL | MySQL | MariaDB | MS SQL |
|---|---|---|---|---|---|---|
| rest | default | pass / pass | pass / pass | pass / pass | pass / pass | pass / pass |
| cli | default | pass / pass | pass / pass | pass / pass | pass / pass | pass / pass |
| diff | default | pass / pass | pass / pass | pass / pass | pass / pass | pass / pass |
| integration | default | pass / pass | pass / pass | pass / pass | pass / pass | pass / pass |
| schema | default | pass / pass | pass / pass | pass / pass | pass / pass | pass / pass |
| diff | custom | pass / pass | pass / pass | pass / pass | pass / pass | pass / pass |
| schema | custom | pass / pass | pass / pass | pass / pass | pass / pass | pass / pass |
| integration | integrations | – / pass | pass / pass | pass / pass | pass / pass | **fail** / pass |
<!-- matrix:end -->

In the `integrations` profile, upstream's runs on SQLite (container exits during start) and MS SQL (schema missing right after it reports healthy) fail only when run in the matrix sequence; both pass when rerun alone. The cause is still being investigated in the harness; the port passes in both cases.

## Benchmark

Method, fairness rules and how to rerun it are in [`bench/CLAUDE.md`](bench/CLAUDE.md). In short: both images run with the same limits (1 CPU, 512 MB) against the same PostgreSQL 17 instance, restored before every run from the same seed (100k short URLs, 1M visits, 200 tags, 5 domains); k6 drives eight scenarios at a fixed arrival rate (median of three runs), then a step test finds the highest sustained rate (p99 ≤ 500 ms, errors ≤ 1%). The tables below are generated from the raw results in `bench/results/` by `go run ./bench/report`.

<!-- bench:start -->
Not run yet.
<!-- bench:end -->

Threats to validity: the load generator, the database and the target share one machine and Docker Desktop's VM, so absolute numbers are specific to this machine; the comparison between targets under equal limits is the result. Upstream runs with its default RoadRunner worker count, which is sized from the CPUs the container sees, not from its CPU limit.

## Differences from upstream

Known differences, none of which changes what an API client, the web client or a script reading normal CLI output sees:

- **PHP internals do not exist.** Verbose (`-v`) CLI output does not include PHP exception traces, `init --download-rr-binary` prints a warning (there is no RoadRunner binary), and the installer's `-vv`/`-vvv` child process debug output is not reproduced.
- **`config/params/*.php` must be data.** The port reads the files the installer writes (literal arrays and scalars) and refuses any other PHP code with an error naming the line; upstream would execute it.
- **Installer type errors and endless prompts.** Where upstream's strict types throw a `TypeError` on unusual installer answers (an empty CORS allowlist, an empty trusted-proxies list, a non-integer redirect status in an imported config), or re-ask forever after input runs out, the port fails with a plain error instead. Installer messages for failed file writes are close to upstream's but not identical.
- **Bit.ly import** is ported but has no contract test, because the importer calls the real Bit.ly API.

Report any other behavior that differs from Shlink 5.1.8 as a bug.

## License

MIT. The upstream copyright notice is kept in [LICENSE](LICENSE); ported third-party code and data are credited in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
