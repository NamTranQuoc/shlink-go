# Build, test and parity tooling for the Go port of Shlink. See CLAUDE.md for what each target is for.
UPSTREAM_VERSION := 5.1.8
UPSTREAM_IMAGE := shlinkio/shlink:$(UPSTREAM_VERSION)
TARGET ?= go
DB ?= sqlite
PROFILE ?= default
ENGINES := sqlite postgres mysql maria mssql

.PHONY: build test image contract contract-all webclient bench bench-report licenses lint reference

build:
	CGO_ENABLED=0 go build -trimpath -o bin/shlink ./cmd/shlink
	ln -sf shlink bin/shlink-installer

test:
	go test ./internal/... ./cmd/...

image:
	docker build -f docker/Dockerfile -t shlink-go:dev .

contract:
	PROFILE=$(PROFILE) scripts/contract.sh $(TARGET) $(DB)

contract-all:
	@set -e; for db in $(ENGINES); do echo "== $(TARGET) $$db"; PROFILE=$(PROFILE) scripts/contract.sh $(TARGET) $$db; done

webclient:
	scripts/webclient.sh $(TARGET)

bench:
	bench/run.sh

bench-report:
	go run ./bench/report

licenses:
	go run github.com/google/go-licenses/v2@v2.0.1 check ./... --allowed_licenses=MIT,Apache-2.0,BSD-2-Clause,BSD-3-Clause,ISC,MPL-2.0

lint:
	go vet ./...
	@test -z "$$(gofmt -l cmd internal contract bench)" || (gofmt -l cmd internal contract bench; exit 1)

reference:
	rm -rf reference/shlink reference/shlink-vendor
	git clone --depth 1 --branch v$(UPSTREAM_VERSION) https://github.com/shlinkio/shlink.git reference/shlink
	docker create --name shlink-reference $(UPSTREAM_IMAGE) >/dev/null
	docker cp shlink-reference:/etc/shlink/vendor reference/shlink-vendor
	docker rm shlink-reference >/dev/null
