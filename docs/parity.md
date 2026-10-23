# Parity with Shlink 5.1.8

What the drop-in guarantee covers, how parity with upstream is tested, the current results and the known differences.

## Drop-in compatibility

A Shlink 5.1.8 deployment can switch to this image without other changes, because it keeps the same

- database schema and `migrations` table on MySQL, MariaDB, PostgreSQL, MS SQL Server and SQLite;
- REST API paths, JSON bodies and RFC 7807 error responses;
- env vars, including the `_FILE` suffix fallback;
- CLI command names, options and output;
- Docker image layout (`/etc/shlink`, data in `/etc/shlink/data`, port 8080, `shlink` on the `PATH`).

It adds no features of its own. Where it behaves differently from upstream, that is a bug.

## Contract suite

Parity is checked by a black-box contract suite in [`contract/`](../contract/CLAUDE.md) that runs, unchanged, against both the upstream image `shlinkio/shlink:5.1.8` and this port:

```
scripts/contract.sh php postgres        # against upstream
scripts/contract.sh go postgres         # against this port
PROFILE=custom scripts/contract.sh go sqlite
make contract-all TARGET=go             # all five engines
scripts/matrix.sh                       # every target × engine × profile; rewrites the table below
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
