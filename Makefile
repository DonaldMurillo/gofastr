.PHONY: build build-all build-cmd build-examples csp-check test test-pg test-pg-env test-pg-only test-race bench bench-sqlite bench-pg bench-tier1 bench-tier2 bench-tier3 bench-tier4 bench-tier5 bench-tier6 bench-tier7 bench-tier8 bench-tier9 bench-techempower bench-overhead bench-resources lint generate dev clean security security-full fuzz hooks install ollama-up ollama-down ollama-logs embed-live

# ---- Build ----
#
# Every build artifact goes into $(DIST_DIR) so the source tree stays clean
# and a single `make clean` removes everything. The directory is gitignored.
# Dev-loop binaries (scripts/dev-watch.sh) still write to /tmp because they
# are ephemeral and watched-tree pollution causes rebuild storms.
DIST_DIR ?= dist

build: csp-check build-cmd

build-cmd: $(DIST_DIR)
	go build -o $(DIST_DIR)/gofastr ./cmd/gofastr
	go build -o $(DIST_DIR)/kiln    ./cmd/kiln

build-examples: csp-check $(DIST_DIR)
	@for dir in examples/api-tour examples/blog examples/embed-demo \
	            examples/spa examples/static-site examples/website; do \
		name=$$(basename $$dir); \
		echo "  building $$name → $(DIST_DIR)/examples/$$name"; \
		go build -o $(DIST_DIR)/examples/$$name ./$$dir || exit 1; \
	done

build-all: csp-check build-cmd build-examples

# csp-check refuses to build when production source emits inline
# <script> blocks. The framework's default Content-Security-Policy
# (default-src 'self') blocks inline JS, so an inline emission
# silently breaks the page in the browser. Make build depends on
# this so a regression fails the build, not the eyeball test.
csp-check:
	@go run ./cmd/check-csp .

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

# Tier 5 — TechEmpower-style endpoints (Plaintext, JSON, SingleQuery,
# MultiQuery, Fortunes-like, Updates). Numbers here are cross-comparable
# with the published TechEmpower Framework Benchmarks.
bench-tier5: $(BENCH_OUT)
	go test -run=^$$ -bench=BenchmarkT5 -benchmem -benchtime=$(BENCHTIME) \
		-count=$(BENCH_COUNT) -timeout=$(BENCH_TIMEOUT) ./framework/ | tee $(BENCH_OUT)/tier5.txt

# Tier 6 — Latency percentiles + concurrency. Reports p50/p90/p99/p999_ns
# per concurrency level via b.ReportMetric.
bench-tier6: $(BENCH_OUT)
	go test -run=^$$ -bench=BenchmarkT6 -benchmem -benchtime=$(BENCHTIME) \
		-count=$(BENCH_COUNT) -timeout=$(BENCH_TIMEOUT) ./framework/ | tee $(BENCH_OUT)/tier6.txt

# Tier 7 — Stdlib baselines (net/http + database/sql) paired with the
# framework equivalents. The delta is the framework's overhead tax.
bench-tier7: $(BENCH_OUT)
	go test -run=^$$ -bench=BenchmarkT7 -benchmem -benchtime=$(BENCHTIME) \
		-count=$(BENCH_COUNT) -timeout=$(BENCH_TIMEOUT) ./framework/ | tee $(BENCH_OUT)/tier7.txt

# Tier 8 — Operational (cold start, sustained memory, goroutine leaks).
# Use benchtime=1x for property-style metrics.
bench-tier8: $(BENCH_OUT)
	go test -run=^$$ -bench=BenchmarkT8 -benchmem -benchtime=1x \
		-count=$(BENCH_COUNT) -timeout=$(BENCH_TIMEOUT) ./framework/ | tee $(BENCH_OUT)/tier8.txt

# Tier 9 — UI runtime: real-volume streaming list, SSE EventStream
# end-to-end, island RPC swap, UI host page render.
bench-tier9: $(BENCH_OUT)
	go test -run=^$$ -bench=BenchmarkT9 -benchmem -benchtime=$(BENCHTIME) \
		-count=$(BENCH_COUNT) -timeout=$(BENCH_TIMEOUT) ./framework/ | tee $(BENCH_OUT)/tier9.txt

# Convenience aliases for the comparable-to-industry slices.
bench-techempower: bench-tier5
bench-overhead: bench-tier7

# Resource benchmarks: bundle size, peak RAM during `go build`, idle/loaded
# RAM of each running app. Builds three bench apps (minimal, crud, full) and
# the two cmd/* binaries. Output is Markdown to stdout + dist/bench/resources.md.
bench-resources: $(BENCH_OUT)
	@mkdir -p $(BENCH_OUT)/resources
	go run ./cmd/bench-resources -load=$${LOAD:-200} -out=$(BENCH_OUT)/resources \
		| tee $(BENCH_OUT)/resources.md

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
	@GVC="$$(command -v govulncheck 2>/dev/null || echo "$$(go env GOPATH)/bin/govulncheck")"; \
	if [ -x "$$GVC" ]; then \
		"$$GVC" ./... && echo "  ✓ No known vulnerabilities"; \
	else \
		echo "  ! govulncheck not installed — gate cannot run (failing closed)"; \
		echo "    Install with the repo's Go toolchain so it can load go1.26 packages:"; \
		echo "    GOTOOLCHAIN=go1.26.3 go install golang.org/x/vuln/cmd/govulncheck@latest"; \
		exit 1; \
	fi

mod-verify:
	go mod verify
	@echo "  ✓ Module integrity verified"

# Active fuzz discovery on the five parser surfaces. Non-deterministic, so
# it is NOT part of security-full (the crasher seeds in testdata/fuzz/
# already replay deterministically under `make test`). Run before releases
# and after touching a parser. Budget: make fuzz FUZZTIME=2m
FUZZTIME ?= 30s
fuzz:
	@set -e; \
	for t in \
		"core/upload FuzzSanitizeFilename" \
		"framework/pagination FuzzDecodeMultiCursor" \
		"framework/dsl FuzzParseDSL" \
		"core/markdown FuzzRenderHTML" \
		"framework/filter FuzzParseFilters"; do \
		set -- $$t; \
		echo "  → fuzzing $$2 ($$1) for $(FUZZTIME)"; \
		go test ./$$1/ -run='^$$' -fuzz="^$$2$$" -fuzztime=$(FUZZTIME) \
			|| { go clean -fuzzcache; echo "  ✗ crasher found in $$2 — seed saved under $$1/testdata/fuzz/"; exit 1; }; \
	done; \
	go clean -fuzzcache; \
	echo "  ✓ Fuzz smoke passed (cache reclaimed)"

# ---- Git Hooks ----

hooks:
	git config core.hooksPath .githooks
	@echo "  ✓ Git hooks activated"

install: hooks
	@echo "  ✓ GoFastr development environment ready"

# ---- Ollama (battery/embed live tests) ----
#
# These targets manage a local Ollama container declared in
# docker-compose.yml. Models cache to ./.ollama/ which is gitignored.

ollama-up:
	@./scripts/ollama-up.sh

ollama-down:
	@docker compose down
	@echo "  ✓ ollama stopped"

ollama-logs:
	@docker compose logs -f ollama

# embed-live runs the battery/embed tests that talk to a real Ollama
# server. They are guarded by `//go:build live` so the default
# `go test ./...` skips them entirely.
embed-live:
	@if ! curl -fsS http://localhost:11434/api/tags >/dev/null 2>&1; then \
		echo "✗ ollama is not reachable at http://localhost:11434"; \
		echo "  run: make ollama-up"; \
		exit 1; \
	fi
	go test -tags=live -count=1 -v ./battery/embed/...
