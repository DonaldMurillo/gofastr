.PHONY: build build-all build-cmd build-examples test test-pg test-pg-env test-pg-only test-race bench bench-sqlite bench-pg bench-tier1 bench-tier2 bench-tier3 bench-tier4 lint generate dev clean security security-full hooks install

# ---- Build ----
#
# Every build artifact goes into $(DIST_DIR) so the source tree stays clean
# and a single `make clean` removes everything. The directory is gitignored.
# Dev-loop binaries (scripts/dev-watch.sh) still write to /tmp because they
# are ephemeral and watched-tree pollution causes rebuild storms.
DIST_DIR ?= dist

build: build-cmd

build-cmd: $(DIST_DIR)
	go build -o $(DIST_DIR)/gofastr ./cmd/gofastr
	go build -o $(DIST_DIR)/kiln    ./cmd/kiln

build-examples: $(DIST_DIR)
	@for dir in examples/api-tour examples/blog examples/core-ui-demo \
	            examples/demo examples/spa examples/static-site \
	            examples/website examples/widgets-demo; do \
		name=$$(basename $$dir); \
		echo "  building $$name → $(DIST_DIR)/examples/$$name"; \
		go build -o $(DIST_DIR)/examples/$$name ./$$dir || exit 1; \
	done

build-all: build-cmd build-examples

$(DIST_DIR):
	@mkdir -p $(DIST_DIR)

test:
	go test -count=1 -short ./...

# Run framework tests against Postgres via TEST_POSTGRES_DSN. Set the DSN to
# point at a local PG you don't mind us creating per-test schemas in.
# Each test creates and drops its own schema, so leftover state is bounded
# to the duration of the test.
test-pg-env:
	@if [ -z "$$TEST_POSTGRES_DSN" ]; then \
		echo "✗ TEST_POSTGRES_DSN is not set. Example:"; \
		echo "    export TEST_POSTGRES_DSN='postgres://test:test@localhost:5432/framework_test?sslmode=disable'"; \
		exit 1; \
	fi
	TEST_POSTGRES_DSN="$$TEST_POSTGRES_DSN" go test -count=1 ./framework/...

# Run framework tests against an ephemeral testcontainers-spawned Postgres.
# Requires Docker; the container is reused across tests inside a single `go
# test` invocation, then torn down on exit.
test-pg:
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "✗ Docker not found. Install Docker or use `make test-pg-env`."; \
		exit 1; \
	fi
	go test -count=1 ./framework/...

# Subset: only the Postgres halves of the dual-dialect subtests. Useful when
# iterating on a Postgres-specific bug to skip the SQLite branch's noise.
test-pg-only:
	@if ! command -v docker >/dev/null 2>&1 && [ -z "$$TEST_POSTGRES_DSN" ]; then \
		echo "✗ Need Docker or TEST_POSTGRES_DSN."; exit 1; \
	fi
	go test -count=1 -run '/postgres' ./framework/...

test-race:
	go test -race -count=1 ./...

# ---- Benchmarks ----
#
# `make bench` runs every Benchmark across the repo with stable defaults.
# Postgres tiers are SKIPPED when no PG is reachable — set TEST_POSTGRES_DSN
# or have Docker running to exercise them.
#
# Output is captured to dist/bench/ for diff'ing with benchstat.

BENCH_OUT       ?= $(DIST_DIR)/bench
BENCH_PKGS      ?= ./framework/... ./core/router/... ./battery/search/...
BENCHTIME       ?= 1s
BENCH_COUNT     ?= 3
BENCH_TIMEOUT   ?= 30m

$(BENCH_OUT):
	@mkdir -p $(BENCH_OUT)

bench: $(BENCH_OUT)
	go test -run=^$$ -bench=. -benchmem -benchtime=$(BENCHTIME) -count=$(BENCH_COUNT) \
		-timeout=$(BENCH_TIMEOUT) $(BENCH_PKGS) | tee $(BENCH_OUT)/all.txt

bench-sqlite: $(BENCH_OUT)
	@# BENCH_SKIP_PG=1 makes forEachBenchDialect skip the postgres branch even
	@# when PG is reachable, so SQLite-only runs are deterministic regardless
	@# of Docker state.
	BENCH_SKIP_PG=1 go test -run=^$$ -bench=. -benchmem -benchtime=$(BENCHTIME) \
		-count=$(BENCH_COUNT) -timeout=$(BENCH_TIMEOUT) $(BENCH_PKGS) | tee $(BENCH_OUT)/sqlite.txt

bench-pg: $(BENCH_OUT)
	@if [ -z "$$TEST_POSTGRES_DSN" ] && ! command -v docker >/dev/null 2>&1; then \
		echo "✗ Need TEST_POSTGRES_DSN or Docker for Postgres benchmarks"; exit 1; \
	fi
	go test -run=^$$ -bench=postgres -benchmem -benchtime=$(BENCHTIME) -count=$(BENCH_COUNT) \
		-timeout=$(BENCH_TIMEOUT) $(BENCH_PKGS) | tee $(BENCH_OUT)/pg.txt

bench-tier1: $(BENCH_OUT)
	go test -run=^$$ -bench=BenchmarkTier1 -benchmem -benchtime=$(BENCHTIME) \
		-count=$(BENCH_COUNT) -timeout=$(BENCH_TIMEOUT) ./framework/ | tee $(BENCH_OUT)/tier1.txt

bench-tier2: $(BENCH_OUT)
	go test -run=^$$ -bench='BenchmarkMiddleware|BenchmarkJSONCasing|BenchmarkDSLParse|BenchmarkRouter' \
		-benchmem -benchtime=$(BENCHTIME) -count=$(BENCH_COUNT) -timeout=$(BENCH_TIMEOUT) \
		./framework/ ./core/router/ | tee $(BENCH_OUT)/tier2.txt

bench-tier3: $(BENCH_OUT)
	go test -run=^$$ -bench='BenchmarkEventBus|BenchmarkSSE|BenchmarkCron' \
		-benchmem -benchtime=$(BENCHTIME) -count=$(BENCH_COUNT) -timeout=$(BENCH_TIMEOUT) \
		./framework/ | tee $(BENCH_OUT)/tier3.txt

bench-tier4: $(BENCH_OUT)
	go test -run=^$$ -bench='BenchmarkAutoMigrate|BenchmarkSchemaDiff|BenchmarkMemory' \
		-benchmem -benchtime=$(BENCHTIME) -count=$(BENCH_COUNT) -timeout=$(BENCH_TIMEOUT) \
		./framework/ ./battery/search/ | tee $(BENCH_OUT)/tier4.txt

lint:
	golangci-lint run ./...

generate:
	@echo "No codegen yet"

dev:
	@echo "Use: gofastr dev"

clean:
	rm -rf $(DIST_DIR)/ bin/ .gofastr/

# ---- Security ----

# Quick security check (runs locally, fast)
security: fmt-check vet secret-scan test
	@echo ""
	@echo "  ✓ Quick security check passed"

# Full security check (run before releases)
security-full: fmt-check vet secret-scan test-race vulncheck mod-verify
	@echo ""
	@echo "  ✓ Full security check passed"

fmt-check:
	@gofmt_output=$$(gofmt -l .); \
	if [ -n "$$gofmt_output" ]; then \
		echo "✗ Files not formatted:"; \
		echo "$$gofmt_output"; \
		echo "Fix: gofmt -w ."; \
		exit 1; \
	fi
	@echo "  ✓ Code formatted"

vet:
	go vet ./...
	@echo "  ✓ go vet clean"

secret-scan:
	@echo "  Scanning for secrets..."
	@found=""; \
	for file in $$(find . -name "*.go" -not -path "./.git/*" -not -path "./vendor/*"); do \
		for pattern in 'BEGIN RSA PRIVATE KEY' 'BEGIN PRIVATE KEY' 'BEGIN OPENSSH PRIVATE KEY' \
			'password.*=.*"' 'secret_key.*=.*"' 'api_key.*=.*"' \
			'sk_live_' 'sk_test_' 'ghp_' 'AKIA' 'xoxb-'; do \
			matches=$$(grep -n -i "$$pattern" "$$file" 2>/dev/null || true); \
			if [ -n "$$matches" ]; then \
				found="$$found\n  $$file: $$matches"; \
			fi; \
		done; \
	done; \
	if [ -n "$$found" ]; then \
		echo "✗ Potential secrets found:"; \
		printf "$$found\n"; \
		exit 1; \
	fi
	@echo "  ✓ No secrets detected"

vulncheck:
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
		echo "  ✓ No known vulnerabilities"; \
	else \
		echo "  ! govulncheck not installed"; \
		echo "    Install: go install golang.org/x/vuln/cmd/govolncheck@latest"; \
	fi

mod-verify:
	go mod verify
	@echo "  ✓ Module integrity verified"

# ---- Git Hooks ----

hooks:
	git config core.hooksPath .githooks
	@echo "  ✓ Git hooks activated"

install: hooks
	@echo "  ✓ GoFastr development environment ready"
