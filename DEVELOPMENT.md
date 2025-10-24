# Development Guide

This guide covers the development setup and workflows for the Compass project.

## 🚀 Quick Start

### Option 1: DevContainer (Recommended)
The fastest way to get started with a complete development environment:

1. **Prerequisites**:
   - [VS Code](https://code.visualstudio.com/)
   - [Dev Containers Extension](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers)
   - [Docker Desktop](https://www.docker.com/products/docker-desktop)

2. **Open in Container**:
   ```
   F1 → "Dev Containers: Reopen in Container"
   ```

3. **Wait for Setup** (~5-10 min first time, <30s after)

4. **Start Coding!** Everything is pre-configured:
   - Go 1.23
   - All development tools
   - GCP SDK
   - Zsh with helpful aliases

📖 **[Full DevContainer Documentation](.devcontainer/README.md)**

### Option 2: Local Setup
If you prefer to develop locally:

1. **Install Go**: [go.dev/doc/install](https://go.dev/doc/install)
2. **Install Tools**:
   ```bash
   go install golang.org/x/tools/cmd/goimports@latest
   go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
   go install github.com/go-delve/delve/cmd/dlv@latest
   ```
3. **Install GCP SDK**: [cloud.google.com/sdk/docs/install](https://cloud.google.com/sdk/docs/install)
4. **Open in VS Code**: Configure with the included `.vscode/` settings

📖 **[VSCode Configuration Guide](.vscode/README.md)**

## 📦 Project Structure

```
compass/
├── .devcontainer/       # Docker development environment
│   ├── devcontainer.json
│   ├── Dockerfile
│   ├── docker-compose.yml
│   ├── post-create.sh
│   └── README.md
├── .vscode/            # VS Code configuration
│   ├── settings.json   # Workspace settings
│   ├── launch.json     # Debug configurations
│   ├── tasks.json      # Build tasks
│   └── README.md       # VSCode usage guide
├── cmd/                # Command implementations
├── internal/           # Internal packages
│   ├── cache/         # Caching utilities
│   ├── gcp/           # GCP API clients
│   ├── logger/        # Logging infrastructure
│   ├── output/        # Output formatting
│   ├── ssh/           # SSH utilities
│   ├── update/        # Self-update logic
│   └── version/       # Version information
├── main.go            # Application entry point
└── go.mod             # Go module definition
```

## 🧪 Testing

### Running Tests

#### All Tests
```bash
go test ./...                    # Basic
go test -v ./...                 # Verbose
go test -v -race ./...           # With race detection
```

#### Specific Package
```bash
go test ./internal/output
go test -v ./internal/output
```

#### Single Test
```bash
go test -run TestSpinnerStart ./internal/output
```

#### With Coverage
```bash
# Generate coverage report
go test -coverprofile=coverage.out ./...

# View in terminal
go tool cover -func=coverage.out

# View in browser
go tool cover -html=coverage.out
```

### Coverage by Package

Current coverage status:

| Package | Coverage |
|---------|----------|
| **internal/logger** | 90.2% |
| **internal/ssh** | 74.4% |
| **internal/output** | 72.0% |
| **internal/cache** | 54.4% |
| **internal/update** | 52.9% |
| **cmd** | 21.4% |
| **internal/gcp** | 20.9% |
| **internal/version** | 100.0% |

### Testing in VS Code

See the [VSCode README](.vscode/README.md) for details on:
- Visual coverage in editor (green/red gutters)
- Debugging tests with breakpoints
- Running specific tests

### Writing Tests

Follow these conventions:

```go
// File: mypackage/myfile_test.go
package mypackage

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestMyFunction(t *testing.T) {
    t.Run("descriptive_test_case", func(t *testing.T) {
        // Arrange
        input := "test"

        // Act
        result := MyFunction(input)

        // Assert
        assert.Equal(t, "expected", result)
    })
}
```

**Use `testify` assertions**:
- `assert.*` - Continues on failure
- `require.*` - Stops on failure (use for setup)

## 🔨 Building

### Build Binary
```bash
go build -v -o compass .
```

### Build with Version Info
```bash
VERSION=$(git describe --tags)
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')
go build -ldflags "-X main.Version=$VERSION -X main.BuildTime=$BUILD_TIME" -o compass .
```

### Cross-Compilation
```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o compass-linux-amd64 .

# Windows
GOOS=windows GOARCH=amd64 go build -o compass-windows-amd64.exe .

# macOS
GOOS=darwin GOARCH=amd64 go build -o compass-darwin-amd64 .
GOOS=darwin GOARCH=arm64 go build -o compass-darwin-arm64 .
```

## 🎨 Code Quality

### Formatting
```bash
# Format all code
go fmt ./...

# Or use goimports (preferred - organizes imports too)
goimports -w .
```

**VS Code**: Code is automatically formatted on save

### Linting
```bash
# Run golangci-lint
golangci-lint run ./...

# Fix auto-fixable issues
golangci-lint run --fix ./...
```

**VS Code**: Linting runs automatically on save

### Pre-commit Checks
```bash
# Run this before committing
go fmt ./...
go vet ./...
golangci-lint run ./...
go test ./...
```

## 🔧 Development Tools

### Installed in DevContainer

- **goimports** - Import management and formatting
- **golangci-lint** - Comprehensive linter
- **delve (dlv)** - Go debugger
- **air** - Live reload for development
- **task** - Task runner (Taskfile)
- **gotestsum** - Enhanced test output
- **staticcheck** - Static analysis
- **mockery** - Mock generation

### Installing Tools Locally
```bash
go install golang.org/x/tools/cmd/goimports@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install github.com/go-delve/delve/cmd/dlv@latest
go install github.com/cosmtrek/air@latest
go install github.com/go-task/task/v3/cmd/task@latest
go install gotest.tools/gotestsum@latest
```

## 🐳 Docker

### Build Docker Image
```bash
docker build -t compass:latest .
```

### Run in Container
```bash
docker run --rm compass:latest --help
```

## 📝 Git Workflow

### Branches
- `main` - Stable release branch
- `develop` - Development branch
- `feature/*` - Feature branches
- `bugfix/*` - Bug fix branches

### Commit Messages
Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add VPN tunnel status command
fix: correct coverage calculation in output
docs: update development guide
test: add tests for spinner component
refactor: simplify GCP client initialization
```

### Pre-commit Hook
The devcontainer automatically sets up a pre-commit hook that runs tests:

```bash
#!/bin/bash
echo "Running tests before commit..."
go test ./...
if [ $? -ne 0 ]; then
    echo "Tests failed. Commit aborted."
    exit 1
fi
```

## 🌐 GCP Development

### Authentication
```bash
# Login to GCP
gcloud auth login

# Set project
gcloud config set project YOUR_PROJECT_ID

# Application default credentials (for API calls)
gcloud auth application-default login
```

### Testing GCP Features
```bash
# List instances
./compass gcp ssh --list

# Test connectivity
./compass gcp connectivity-test list

# VPN inspection
./compass gcp vpn list
```

## 🐛 Debugging

### Using Delve Directly
```bash
# Debug main package
dlv debug .

# Debug with arguments
dlv debug . -- gcp ssh my-instance

# Debug tests
dlv test ./internal/output

# Debug specific test
dlv test ./internal/output -- -test.run TestSpinnerStart
```

### Debug in VS Code
See [VSCode README](.vscode/README.md) for:
- Setting breakpoints
- Inspecting variables
- Step-through debugging
- Conditional breakpoints

## 📊 Performance

### Benchmarking
```bash
# Run benchmarks
go test -bench=. ./...

# With memory stats
go test -bench=. -benchmem ./...

# Specific benchmark
go test -bench=BenchmarkMyFunction ./internal/output
```

### Profiling
```bash
# CPU profile
go test -cpuprofile=cpu.prof ./...
go tool pprof cpu.prof

# Memory profile
go test -memprofile=mem.prof ./...
go tool pprof mem.prof
```

## 🔐 Security

### Vulnerability Scanning
```bash
# Check for known vulnerabilities
go list -json -m all | nancy sleuth
```

### Dependency Updates
```bash
# Check for updates
go list -u -m all

# Update dependencies
go get -u ./...
go mod tidy
```

## 📚 Documentation

### Generate Go Docs
```bash
# Start doc server
godoc -http=:6060

# View at http://localhost:6060
```

### Code Comments
Follow Go documentation conventions:

```go
// MyFunction does something useful.
// It takes a string parameter and returns an error.
//
// Example:
//   result, err := MyFunction("input")
//   if err != nil {
//       return err
//   }
func MyFunction(input string) error {
    // Implementation
}
```

## 🎯 Best Practices

### Code Organization
- Keep packages focused and cohesive
- Use internal/ for non-public packages
- Avoid circular dependencies
- Use dependency injection

### Error Handling
```go
// Good
if err != nil {
    return fmt.Errorf("failed to connect: %w", err)
}

// Use errors.Is for checking
if errors.Is(err, ErrNotFound) {
    // Handle not found
}
```

### Testing
- Test public API, not internal details
- Use table-driven tests for multiple cases
- Mock external dependencies
- Aim for >70% coverage on new code

### Performance
- Use benchmarks to verify optimizations
- Profile before optimizing
- Consider memory allocations
- Use sync.Pool for frequently allocated objects

## 🆘 Troubleshooting

### Common Issues

#### "Command not found: go"
**Solution**: Install Go from [go.dev](https://go.dev/doc/install)

#### "Module not found"
**Solution**:
```bash
go mod download
go mod tidy
```

#### "Delve not working"
**Solution**:
```bash
go install github.com/go-delve/delve/cmd/dlv@latest
```

#### "Tests failing in CI but passing locally"
**Possible causes**:
- Race conditions (run with `-race`)
- File path assumptions
- Time zone differences
- Environment variables

## 📞 Getting Help

- **Documentation**: Check `.vscode/README.md` and `.devcontainer/README.md`
- **Go Documentation**: [go.dev/doc](https://go.dev/doc/)
- **VS Code Go Extension**: [github.com/golang/vscode-go](https://github.com/golang/vscode-go)
- **GCP SDK**: [cloud.google.com/sdk/docs](https://cloud.google.com/sdk/docs)

---

Happy coding! 🚀
