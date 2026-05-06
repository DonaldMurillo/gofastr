.PHONY: build test lint generate dev clean security security-full hooks install

# ---- Build ----
build:
	go build ./...

test:
	go test -count=1 -short ./...

test-race:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

generate:
	@echo "No codegen yet"

dev:
	@echo "Use: gofastr dev"

clean:
	rm -rf bin/ .gofastr/

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

test-race:
	go test -race -count=1 ./...
	@echo "  ✓ Race detector clean"

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
