# contract

How the black-box contract suite proves that this port behaves like upstream Shlink 5.1.8.

## Principle

- The suite talks to a running Shlink only through HTTP, the CLI (`docker exec ... shlink`) and the database (to seed fixtures and read the schema). It never imports the port's packages.
- The same tests run against upstream (`TARGET=php`, image `shlinkio/shlink:5.1.8`) and this port (`TARGET=go`). A test is valid only once it passes against upstream; then the port must pass it too.
- Matrix: both targets × SQLite, PostgreSQL, MySQL, MariaDB, MS SQL Server, plus the env profiles below.

## Running

```
scripts/contract.sh <php|go> <sqlite|postgres|mysql|maria|mssql> [go test args]
PROFILE=custom scripts/contract.sh go postgres
KEEP_STACK=1 scripts/contract.sh go sqlite    # leave the target container running afterwards
```

- The script recreates only the target's container, starts from an empty database (fresh SQLite file, or the server database dropped), waits for `/rest/health` and runs `go test -p 1 ./contract/...`. With `PROFILE=integrations` it also starts the Redis, RabbitMQ and Mercure services and runs only `./contract/integration/...`: the integrations' async listeners write to the database while other suites purge it between tests. Any other non-default profile runs only `diff` and `schema`, because `rest` and `cli` port upstream's tests, which assume the default configuration.
- Settings reach the tests through env vars: `SHLINK_TARGET_URL`, `SHLINK_CLI`, `CONTRACT_DB_DRIVER`, `CONTRACT_DB_DSN`, `CONTRACT_TARGET`, `CONTRACT_PROFILE`.
- Upstream on MS SQL Server needs the amd64 image (no `pdo_sqlsrv` in the arm64 build); the script adds `docker/compose.php-amd64.yml` for that case.

## Packages

- `internal/harness`: config, HTTP/CLI helpers, assertions, fixtures (a port of upstream's `module/*/test-api/Fixtures` and `test-cli/Fixtures`), DB purge/seed, call recording for coverage.
- `rest`: ports of every file in upstream's `module/Rest/test-api` and `module/Core/test-api`, one Go file per PHP file, same test names in Go style, plus tests for operations upstream does not cover.
- `cli`: ports of `module/CLI/test-cli` plus command-list and help gates.
- `schema`: schema-diff gate. The target's schema (columns, indexes, foreign keys, migrations rows) must equal `schema/testdata/<engine>.json`, captured from upstream.
- `diff`: differential suite. `diff/testdata/<profile>.cases.json` lists requests; responses (status, selected headers, normalized body) must equal `<profile>.golden.json`, captured from upstream.
- `integration`: Mercure, RabbitMQ, Redis and Matomo checks, run with `PROFILE=integrations`.
- `webclient`: a Playwright script (Node) that drives shlink-web-client 4.8.1 through its main screens and fails on any unexpected API error. Run it with `scripts/webclient.sh <php|go>` (`BROWSER_CHANNEL=chrome` to use an installed Chrome instead of Playwright's Chromium).

## Profiles

`docker/env/profiles/<name>.env` holds extra env vars for both targets. `default` is the plain compose config, `custom` exercises non-default options (base path, multi-segment slugs, trailing slash, 301 + cache headers, extra path, not-found redirects, CORS, robots), `integrations` enables the real-time and Matomo integrations. Profile-specific tests skip unless `CONTRACT_PROFILE` matches.

## Gates

- `rest/zz_swagger_coverage_test.go`: every operation of upstream's `swagger.json` (extracted into `rest/testdata/swagger_operations.txt`) is exercised by some test. Skipped for partial runs (`-run`).
- Schema gate: see `schema` above.
- CLI command-list gate: `shlink list` output matches upstream.

## Goldens

- Capture only from upstream: `CONTRACT_UPDATE_GOLDEN=1` (diff) or `CONTRACT_UPDATE_SCHEMA=1` (schema) with `TARGET=php`; the tests refuse to write goldens from the port.
- Never hand-edit a golden to make the port pass. If upstream's output is engine-specific, mark the case with `skipEngines` and say why.

## Adding a test

1. Read the upstream code for the behavior; find the upstream test if one exists and port it rather than inventing one.
2. Make it pass against upstream on SQLite and PostgreSQL first, then run it against the port.
3. Upstream results that depend on undefined SQL ordering go into `internal/harness/known_failures.go` with the reason, for that engine only.
4. Fake data only: fixtures keep upstream's fake API keys (`valid_api_key`, ...) and example domains.
