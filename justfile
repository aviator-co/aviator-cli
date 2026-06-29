default:
    @just --list

# Build the aviator binary to ./aviator
build:
    go build -o aviator ./cmd/aviator

# Run all tests with vet
test:
    go test --vet=all ./...

# Run golangci-lint (must be installed)
lint:
    golangci-lint run

# Run the CLI: `just run -- verify --help`
run *ARGS:
    go run ./cmd/aviator {{ARGS}}

# Tidy modules
tidy:
    go mod tidy
