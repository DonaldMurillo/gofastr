.PHONY: build test lint generate dev clean

build:
	go build ./...

test:
	go test ./...

lint:
	golangci-lint run ./...

generate:
	@echo "No codegen yet"

dev:
	@echo "No dev server yet"

clean:
	rm -rf bin/ .gofastr/
