# AGENTS.md - AI Agent Guidelines for givetypst

## Project Overview

givetypst is a Go HTTP server that generates PDFs from Typst templates stored in S3-compatible cloud storage. Single-package application with three main source files:

- `main.go` - Entry point, CLI parsing, HTTP server setup
- `server.go` - HTTP handlers, Server struct, request/response types
- `typst.go` - Typst compilation logic

## Build Commands

```bash
make all              # Build to bin/givetypst
make clean            # Remove build artifacts
```

## Lint Commands

```bash
make check            # Run golangci-lint (70+ linters enabled)
make fix              # Auto-fix lint issues
make vet              # Run go vet
make tidy             # Run go mod tidy
```

## Test Commands

```bash
make test                                    # All unit tests with race detector
go test -v -run TestFunctionName ./...       # Single test by name
go test -v -run TestFetch ./...              # Tests matching pattern
make test-integration                        # Integration tests (requires Docker, -tags=integration)
make test-all                                # Unit + integration
make test-coverage                           # With coverage report
```

## Code Style Guidelines

### Import Organization

Organize imports in groups separated by blank lines: (1) Standard library, (2) Third-party packages, (3) Local packages (`github.com/boringbin/givetypst`)

```go
import (
    "context"
    "fmt"

    "log/slog"

    _ "gocloud.dev/blob/s3blob"

    "gocloud.dev/blob"
)
```

### Naming Conventions

| Element              | Convention | Example                            |
| -------------------- | ---------- | ---------------------------------- |
| Constants            | camelCase  | `defaultPort`, `readHeaderTimeout` |
| Types/Structs        | PascalCase | `Server`, `ServerConfig`           |
| Exported functions   | PascalCase | `NewServer`, `Handler`             |
| Unexported functions | camelCase  | `fetchTemplate`, `compileTypst`    |
| Variables            | camelCase  | `bucketURL`, `portNum`             |

### Error Handling

Always wrap errors with context using `%w`:

```go
return "", fmt.Errorf("open bucket: %w", err)
```

For HTTP handlers: `http.Error(w, "template not found", http.StatusBadRequest)`

### Logging

Use `log/slog` for structured logging:

```go
s.logger.Info("Server starting", "port", port)
s.logger.Error("Failed to fetch template", "error", err, "key", key)
```

### Documentation

Comments must end with periods (enforced by `godot` linter). Define constants for timeouts, exit codes, and config values. File permissions use octal: `0600`, `0644`, `0755`.

### Variable Shadowing

Avoid variable shadowing - the linter enforces strict shadow checking:

```go
// Bad - shadows outer err
if err := json.Unmarshal(data, &result); err != nil { }

// Good - use unique name
if unmarshalErr := json.Unmarshal(data, &result); unmarshalErr != nil { }
```

## Linter Configuration

Key limits enforced by `.golangci.yml`:

- **Complexity**: `gocognit` < 20, `cyclop` < 30
- **Function size**: max 100 lines, 50 statements
- **Line length**: max 120 characters
- **Security**: `gosec` enabled
- **HTTP**: `bodyclose`, `noctx` enforced
- **Logging**: `sloglint` enforces structured logging

Rules relaxed for test files: `funlen`, `dupl`, `errcheck`, `gosec`, `noctx`.

## Architecture Patterns

### Dependency Injection

```go
func NewServer(logger *slog.Logger, config ServerConfig) *Server {
    return &Server{logger: logger, config: config}
}
```

### HTTP Server

- Use `http.NewServeMux()` for routing
- Configure timeouts on `http.Server`
- Graceful shutdown with `signal.NotifyContext`

### Testing with Blob Storage

Use `fileblob` with temp directories for tests:

```go
func setupTestBucket(t *testing.T, files map[string][]byte) string {
    dir := t.TempDir()
    for key, content := range files {
        os.WriteFile(filepath.Join(dir, key), content, 0644)
    }
    return "file://" + dir
}
```

## Common Tasks

### Adding a New Endpoint

1. Define request/response types in `server.go`
2. Add handler method to `Server` struct
3. Register route with `mux.HandleFunc("METHOD /path", s.handler)`

### Running Locally

```bash
export BUCKET_URL="s3://my-bucket?region=us-east-1"
make all && ./bin/givetypst -v
```

### Docker Build

The `Dockerfile` accepts a `TYPST_VERSION` build argument (default: `0.14.2`):

```bash
docker build -t givetypst .                              # Uses default Typst version
docker build --build-arg TYPST_VERSION=0.15.0 -t givetypst .  # Custom version
```
