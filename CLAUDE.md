# shlink-go

Guidance for working in this repository: what the project is, the rules it follows, and how to build and test it.

## What this project is

- An unofficial Go port of [Shlink](https://github.com/shlinkio/shlink) **v5.1.8**, the self-hosted URL shortener.
- Public repository: https://github.com/NamTranQuoc/shlink-go. Module path `github.com/NamTranQuoc/shlink-go`.
- Port only. It reproduces upstream behavior; it adds no features of its own. When this code and upstream disagree, upstream is right and this code is the bug.
- Drop-in replacement: a Shlink 5.1.8 deployment switches to this image without other changes. That means the same
  - database schema and `migrations` table (all of MySQL, MariaDB, PostgreSQL, MS SQL Server, SQLite),
  - REST API paths, JSON bodies and RFC 7807 errors,
  - env vars (including the `_FILE` suffix fallback),
  - CLI command names, options and output,
  - Docker image layout (`/etc/shlink`, port 8080, `shlink` on `PATH`).

## Upstream source

- `reference/shlink` — upstream v5.1.8 source; `reference/shlink-vendor` — its `vendor/` tree (copied from the `shlinkio/shlink:5.1.8` image). Both are gitignored; never commit them.
- Recreate them with `make reference`.
- Before porting or changing behavior, read the upstream code for it. Read the `shlinkio/*` packages under `reference/shlink-vendor` too: the importer, installer, geolocation, event dispatcher and common helpers live there.

## License

- MIT, same as upstream. `LICENSE` keeps the upstream copyright line; do not remove it.
- Code or data ported from a third-party PHP library (not Shlink itself, not a Go module) must be credited in `THIRD_PARTY_NOTICES.md` with its full license text, in the same change that ports it.
- Never commit GeoLite2 databases (MaxMind EULA). Check the license of any test fixture taken from another project before committing it.

## Secrets

- The repository is public. Never commit real API keys, tokens, passwords, MaxMind license keys, Mercure JWT secrets, `.env` files or personal data.
- Dev credentials exist only in `docker/` compose and env files and must be obviously fake (`root`, `Passw0rd!`, ...).
- Test fixtures use the upstream fake keys (`valid_api_key`, ...).

## Layout

```
cmd/shlink/             entry point: `shlink serve`, any other args run the CLI, `shlink-installer` (argv[0]) the installer
internal/config/        env vars (`_FILE` fallback, PHP-style casting) and option structs
internal/db/            connections, dialects, schema (Doctrine DDL per engine in schema/), migrations
internal/core/          domain: short URLs, visits, tags, domains, redirect rules, API keys, repositories, services
internal/web/           HTTP layer: FastRoute-compatible router, Mezzio pipeline, REST and public actions
internal/console/       symfony/console-compatible framework (input parsing, help, tables, SymfonyStyle)
internal/cli/           Shlink CLI commands and the shlink-installer application
internal/importer/      port of shlinkio/shlink-importer sources
internal/integration/   events (async listeners), realtime (Mercure/RabbitMQ/Redis), matomo, geolocation, title
internal/phpx/          PHP semantics Go lacks: json_encode, urlencode/parse_str, Guzzle URIs, dates
internal/valinor/       the subset of CuyZ/Valinor mapping rules upstream relies on
internal/pcre/          backtracking regex engine with PCRE semantics, for the ported UA/crawler patterns
internal/useragent/     port of donatj/phpuseragentparser
internal/crawler/       port of jaybizzle/crawler-detect
contract/               black-box contract suite, runs against upstream and this port (see contract/CLAUDE.md)
bench/                  benchmark harness (see bench/CLAUDE.md)
docker/                 Dockerfile, compose stack, per-DB and per-profile env files
scripts/                contract, matrix and web client runners
docs/plan.md            implementation plan and progress (read it before resuming work)
```

## Commands

```
make build                          # bin/shlink
make test                           # go unit tests
make image                          # docker image shlink-go:dev
make contract TARGET=php DB=sqlite  # contract suite against upstream (TARGET=go for this port; PROFILE=<env profile>)
make contract-all TARGET=go         # all 5 engines, default profile
make webclient TARGET=go            # drive shlink-web-client 4.8.1 against the target
make bench                          # benchmark both targets (see bench/CLAUDE.md)
make licenses                       # dependency license report
make lint                           # go vet + gofmt check
make reference                      # recreate reference/ (upstream source and vendor tree)
```

## Conventions

- Go standard library first. Current dependencies are listed in `go.mod`; add one only when porting needs it.
- Keep the upstream names for concepts (short code, orphan visit, redirect condition, ...) so code maps back to upstream.
- Tests: unit tests next to the code; behavior parity is proved in `contract/`, not by unit tests.
- When Go has no exact equivalent of a PHP function upstream relies on (json_encode flags, parse_str, PCRE, date
  parsing), port the PHP semantics into `internal/phpx` or `internal/pcre` and pin them with outputs produced by the
  upstream image (`docker run --rm --entrypoint php shlinkio/shlink:5.1.8 -r '...'`).
