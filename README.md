# shlink-go

An unofficial Go port of [Shlink](https://github.com/shlinkio/shlink) **v5.1.8**, the self-hosted URL shortener, built as a drop-in replacement for it. It is not affiliated with or endorsed by the Shlink project. All credit for the design, the API and the behavior goes to Shlink and its authors; this repository only re-implements it in Go. Shlink is MIT-licensed, and so is this port (see [LICENSE](LICENSE) and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)).

## Quick start: server and web UI

You need Git and Docker. The steps start the server on http://localhost:8080 with a SQLite database kept in a Docker volume, and the web UI ([shlink-web-client](https://github.com/shlinkio/shlink-web-client) 4.8.1, the version the contract suite checks) on http://localhost:8090, already connected to the server.

1. Clone the repository and build the server image:

   ```
   git clone https://github.com/NamTranQuoc/shlink-go.git
   cd shlink-go
   docker build -f docker/Dockerfile -t shlink-go .
   ```

2. Start the server. On first start it creates and migrates the database and the API key `change-me`:

   ```
   docker run -d --name shlink -p 8080:8080 -v shlink-data:/etc/shlink/data \
     -e DEFAULT_DOMAIN=localhost:8080 -e IS_HTTPS_ENABLED=false -e INITIAL_API_KEY=change-me \
     shlink-go
   ```

   Check it with `curl http://localhost:8080/rest/health`, which answers `"status":"pass"`.

3. Start the web UI:

   ```
   docker run -d --name shlink-web -p 8090:8080 \
     -e SHLINK_SERVER_URL=http://localhost:8080 -e SHLINK_SERVER_API_KEY=change-me -e SHLINK_SERVER_NAME=Local \
     shlinkio/shlink-web-client:4.8.1
   ```

4. Open http://localhost:8090 and pick the server **Local**. Short URLs created there redirect from `http://localhost:8080/<short code>`.

The CLI runs inside the server container, as with upstream:

```
docker exec -it shlink shlink short-url:list
```

Stop with `docker stop shlink shlink-web` and start again with `docker start shlink shlink-web`; the data stays in the `shlink-data` volume. `docker rm -f shlink shlink-web && docker volume rm shlink-data` removes everything.

### Configuration

The env vars are upstream's: see the [Shlink documentation](https://shlink.io/documentation/) for all of them, including other databases (`DB_DRIVER`, `DB_HOST`, ...). To serve short URLs on a real domain, set `DEFAULT_DOMAIN` to it (and `IS_HTTPS_ENABLED=true` behind HTTPS), and use your own API key instead of `change-me`, in both containers. `SHLINK_SERVER_URL` is the address the browser calls the server on, not one the UI container uses.

### Running the server without Docker

With Go installed (1.21 or later downloads the toolchain `go.mod` asks for), the server can run as a binary; the web UI still runs as in step 3. Data (the SQLite database, locks) goes to `data/` under the current directory, which must exist:

```
make build
mkdir -p data
export DEFAULT_DOMAIN=localhost:8080 IS_HTTPS_ENABLED=false
bin/shlink-installer init --initial-api-key=change-me --skip-download-geolite
bin/shlink serve
```

`bin/shlink-installer init` creates and migrates the database and creates the API key; it only needs to run again after an upgrade. Other CLI commands run as `bin/shlink <command>` from the same directory.

## Documentation

- [Parity with Shlink 5.1.8](docs/parity.md): what stays compatible, the contract suite, current results and known differences from upstream.
- [Benchmark](docs/benchmark.md): method, results and findings against upstream.
