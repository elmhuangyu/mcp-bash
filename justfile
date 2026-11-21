# List all available recipes
default:
    @just --list

module := "github.com/elmhuangyu/mcp-bash"

# Format Go source files (goimports)
fmt:
    goimports -w -local "{{module}}" .

# Lint Go source files (golangci-lint)
lint:
    golangci-lint run ./...

# Build version: UTC build time and short commit
version := `date -u +%Y-%m-%d-%H-%M-%S` + "-" + `git rev-parse --short=8 HEAD`

# Build the mcp-bash binary into build/
build:
    mkdir -p build && go build -trimpath -o build/ ./cmd/mcp-bash

# Run tests (with race detector)
test:
    go test -race ./...

# Build the Docker image
build-image:
    docker build -t mcp-bash:main -t mcp-bash:{{version}} .

# Run go mod tidy
tidy:
    go mod tidy

# Update Go module dependencies
update-deps:
    go get -u -t ./...
    @just tidy

# Run the mcp-bash server locally (default :8080, streamable-http)
run:
    go run ./cmd/mcp-bash
