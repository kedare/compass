# Development Container

This directory contains the configuration for a VS Code development container that provides a consistent, reproducible development environment for the Compass project.

## Features

### 🔧 Pre-installed Tools

- **Go 1.23**: Latest stable Go version
- **GCP SDK**: Google Cloud SDK with gke-gcloud-auth-plugin
- **Go Development Tools**:
  - `goimports` - Import management
  - `golangci-lint` - Comprehensive linter
  - `delve` (dlv) - Go debugger
  - `air` - Live reload
  - `task` - Task runner
  - `gotestsum` - Enhanced test output
  - `staticcheck` - Static analysis
  - `mockery` - Mock generation
- **Shell**: Zsh with Oh My Zsh
- **Git Tools**: Git, Git LFS, GitHub CLI
- **Docker**: Docker-in-Docker support

### 📦 VS Code Extensions

Automatically installed extensions:
- Go (official)
- GitLens
- Docker
- YAML support
- Markdown linting
- GitHub Pull Requests
- IntelliCode

### 🎯 Development Features

- **Code Coverage**: Visual coverage indicators in the editor gutter
- **Debugging**: Full debugging support with breakpoints
- **Testing**: Integrated test explorer with coverage
- **Formatting**: Auto-format on save with goimports
- **Linting**: Real-time linting with golangci-lint
- **Git Integration**: SSH keys and git config mounted from host

## Quick Start

### Prerequisites

1. **VS Code**: Install [Visual Studio Code](https://code.visualstudio.com/)
2. **Dev Containers Extension**: Install the [Dev Containers extension](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers)
3. **Docker**: Install [Docker Desktop](https://www.docker.com/products/docker-desktop)

### Opening in Container

1. Open the project in VS Code
2. Press `F1` and select **"Dev Containers: Reopen in Container"**
3. Wait for the container to build (first time only, ~5-10 minutes)
4. The post-create script will automatically:
   - Download Go dependencies
   - Install development tools
   - Build the project
   - Run initial tests
   - Set up helpful aliases

### First Time Setup

After opening in the container for the first time:

```bash
# Verify everything is working
go version
gcloud version

# Run tests
go test ./...

# Run with coverage
go test -cover ./...

# Build the project
go build ./...
```

## Useful Commands

The devcontainer includes helpful shell aliases:

### Testing

```bash
gt        # Run all tests
gtv       # Run tests (verbose)
gtc       # Run tests with coverage
cover     # Generate HTML coverage report
coverfunc # Show coverage by function
```

### Building & Linting

```bash
gb        # Build the project
gf        # Format code
gl        # Run linter
goclean   # Clean Go cache
```

### Task Runner

If using Taskfile:

```bash
t         # Run default task
tl        # List all tasks
```

## Configuration Files

### `devcontainer.json`

Main configuration file that defines:
- Base Docker image and features
- VS Code extensions and settings
- Port forwarding
- Mount points
- Environment variables
- Lifecycle hooks

### `Dockerfile`

Custom image with:
- Go 1.23 on Debian Bookworm
- GCP SDK
- All development tools
- Zsh with Oh My Zsh
- Non-root user (vscode)

### `docker-compose.yml`

Orchestrates the development environment:
- Mounts workspace and Docker socket
- Sets up volumes for Go cache
- Configures network and security

### `post-create.sh`

Automation script that runs after container creation:
- Downloads dependencies
- Verifies installation
- Builds project
- Runs tests
- Sets up git hooks
- Configures shell aliases

## Volumes

The container uses named volumes for better performance:

- `compass-go-pkg`: Go package cache (persists between rebuilds)
- `compass-go-cache`: Go build cache (persists between rebuilds)

These volumes ensure fast rebuilds and preserve downloaded dependencies.

## Mounted Files

From your host machine:

- `~/.ssh` → `/home/vscode/.ssh` (read-only) - SSH keys for git
- `~/.gitconfig` → `/home/vscode/.gitconfig` (read-only) - Git configuration

## Debugging

### Running Tests with Debugger

1. Open a test file
2. Set breakpoints by clicking in the gutter
3. Press `F5` or use the debug panel
4. Select "Test Current File" or "Test Current Package"

### Debugging with Coverage

1. Press `F5`
2. Select "Test Current Package with Coverage"
3. After tests complete, coverage will be displayed in the editor

### Launch Configurations

Available in `.vscode/launch.json`:
- Launch Package
- Launch Current File
- Test Current File
- Test Current Package
- Test with Coverage
- Attach to Process
- Debug Specific Test

## Tips & Tricks

### GCP Authentication

To use GCP services:

```bash
gcloud auth login
gcloud config set project YOUR_PROJECT_ID
```

### Running Coverage Report

```bash
# Generate and open HTML coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Using Docker Inside Container

The container has Docker-in-Docker enabled:

```bash
docker ps
docker build -t myimage .
```

### Customizing the Environment

You can customize the devcontainer by:

1. **Add more tools**: Edit `Dockerfile` to install additional packages
2. **Add extensions**: Edit `devcontainer.json` extensions list
3. **Modify settings**: Edit VS Code settings in `devcontainer.json`
4. **Add lifecycle scripts**: Modify `post-create.sh` for setup automation

## Troubleshooting

### Container won't start

1. Check Docker is running: `docker ps`
2. Rebuild container: `F1` → "Dev Containers: Rebuild Container"
3. Check Docker Desktop logs

### Slow performance

1. Ensure volumes are being used (check `docker volume ls`)
2. On Mac/Windows, make sure Docker Desktop has enough resources
3. Close unused applications

### Tools not found

Reload the shell or run:

```bash
source ~/.zshrc
```

### Permission issues

The container runs as `vscode` user (UID 1000). If you have permission issues:

```bash
sudo chown -R vscode:vscode /workspace
```

## Performance Notes

- **First Build**: 5-10 minutes (downloads base images and installs tools)
- **Subsequent Starts**: <30 seconds (uses cached layers)
- **Go Builds**: Fast due to persistent cache volumes

## Updating

To update the devcontainer:

1. Pull latest changes: `git pull`
2. Rebuild: `F1` → "Dev Containers: Rebuild Container"

To update Go tools:

```bash
go install golang.org/x/tools/cmd/goimports@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
# ... etc
```

## Resources

- [VS Code Dev Containers Documentation](https://code.visualstudio.com/docs/devcontainers/containers)
- [Dev Container Specification](https://containers.dev/)
- [Go in VS Code](https://code.visualstudio.com/docs/languages/go)
