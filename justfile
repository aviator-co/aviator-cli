default:
    @just --list

# Build the aviator binary to ./aviator
build:
    go build -o aviator ./cmd/aviator

# Run all tests with vet
test:
    go test --vet=all ./...

# Check formatting and run golangci-lint (golangci-lint must be installed)
lint:
    #!/usr/bin/env bash
    set -euo pipefail
    unformatted="$(go tool gofumpt -l . ; go tool goimports -l .)"
    if [ -n "$unformatted" ]; then
        echo "not formatted, run 'just fmt':"
        echo "$unformatted"
        exit 1
    fi
    golangci-lint run

# Format Go code with gofumpt and goimports
fmt:
    go tool gofumpt -w .
    go tool goimports -w .

# Run the CLI: `just run -- verify --help`
run *ARGS:
    go run ./cmd/aviator {{ARGS}}

# Tidy modules
tidy:
    go mod tidy
