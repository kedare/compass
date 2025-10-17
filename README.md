# compass - Cloud Instance SSH Connector

A fast and intuitive CLI tool to connect to cloud instances using SSH with support for IAP tunneling, Managed Instance Groups (MIGs), and advanced SSH features.

> Prefer something shorter? All commands are also available through the `cps` alias.

## Features

- 🚀 **Quick SSH connections** to GCP instances with auto-discovery
- 🔒 **Identity-Aware Proxy (IAP) tunneling** support
- 🎯 **Managed Instance Group (MIG)** support - both regional and zonal
- 🌐 **Auto-discovery** of zones and regions when not specified
- 🔧 **SSH parameter passing** for tunneling, port forwarding, and custom flags
- 🔍 **Network connectivity testing** with Google Cloud Connectivity Tests API
- 📊 **Structured logging** with configurable levels
- ⚡ **Zero-configuration** - uses existing gcloud credentials
- 🎨 **Intuitive CLI** with helpful error messages

## Installation

### Prerequisites

- Go 1.19+ (for building from source)
- [go-task](https://taskfile.dev/) for task automation
- `gcloud` CLI installed and authenticated
- `ssh` binary available in PATH

### Installing go-task

**macOS:**
```bash
brew install go-task
```

**Linux:**
```bash
sh -c "$(curl -fsSL https://taskfile.dev/install.sh)" -- -d -b ~/.local/bin
```

**Windows:**
```bash
choco install go-task
```

Or download binaries from [releases page](https://github.com/go-task/task/releases).

### From Source

```bash
git clone <repository-url>
cd compass
task build
./compass --help
```

## Quick Start

```bash
# Connect to an instance (auto-discovers zone)
compass gcp my-instance --project my-gcp-project

# Connect with specific zone
compass gcp my-instance --project my-gcp-project --zone us-central1-a

# Connect to a MIG instance (finds first running instance)
compass gcp my-mig-name --project my-gcp-project

# Setup SSH tunnel through IAP
compass gcp my-instance --project my-gcp-project --ssh-flag "-L 8080:localhost:8080"
```

## Usage

### Basic Connection

```bash
compass gcp [instance-name] --project [project-id]
```

### Advanced Usage

```bash
# Multiple SSH flags
compass gcp instance-name \
  --project my-project \
  --ssh-flag "-L 8080:localhost:8080" \
  --ssh-flag "-D 1080" \
  --ssh-flag "-X"

# Enable debug logging
compass gcp instance-name --project my-project --log-level debug

# Connect to regional MIG
compass gcp my-regional-mig --project my-project --zone us-central1
```

### SSH Tunneling Examples

```bash
# Local port forwarding
compass gcp app-server --project prod --ssh-flag "-L 3000:localhost:3000"

# Remote port forwarding
compass gcp jump-host --project staging --ssh-flag "-R 8080:localhost:8080"

# Dynamic port forwarding (SOCKS proxy)
compass gcp proxy-server --project dev --ssh-flag "-D 1080"

# X11 forwarding
compass gcp desktop-instance --project dev --ssh-flag "-X"

# Multiple tunnels
compass gcp multi-service \
  --project prod \
  --ssh-flag "-L 3000:service1:3000" \
  --ssh-flag "-L 4000:service2:4000" \
  --ssh-flag "-L 5000:database:5432"
```

## Command Reference

### Global Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--log-level` | Set logging level (trace, debug, info, warn, error, fatal, panic) | `info` |

### GCP Command Flags

| Flag | Short | Description | Default |
|------|-------|-------------|---------|
| `--project` | `-p` | GCP project ID | Auto-detected from gcloud config |
| `--zone` | `-z` | GCP zone (auto-discovered if not specified) | Auto-discovery |
| `--ssh-flag` | | Additional SSH flags (can be used multiple times) | None |

## How It Works

1. **Instance Discovery**: compass first attempts to find the target as a MIG (Managed Instance Group), then falls back to searching for a standalone instance
2. **Zone Auto-Discovery**: When zone is not specified, compass searches through all available zones
3. **Connection Method Selection**:
   - If instance has external IP and IAP is disabled: Direct SSH connection
   - If IAP is available: Connection through `gcloud compute ssh` with IAP tunneling
4. **SSH Flag Handling**:
   - For IAP connections: Flags are passed as `--ssh-flag="<flag>"` to gcloud
   - For direct connections: Flags are passed directly to ssh command

## Configuration

compass uses your existing gcloud configuration:

```bash
# Set default project
gcloud config set project my-default-project

# Authenticate
gcloud auth login
gcloud auth application-default login
```

## Examples

### Connecting to Different Instance Types

```bash
# Standard VM instance
compass gcp web-server-1 --project production

# Instance in specific zone
compass gcp database-primary --project production --zone us-east1-b

# First running instance in a zonal MIG
compass gcp web-servers-mig --project production --zone us-central1-a

# First running instance in a regional MIG
compass gcp api-servers-mig --project production --zone us-central1
```

### Development Workflows

```bash
# Connect to development instance with port forwarding for web app
compass gcp dev-instance --project dev-project --ssh-flag "-L 3000:localhost:3000"

# Setup database tunnel
compass gcp db-proxy --project staging --ssh-flag "-L 5432:database.internal:5432"

# Create SOCKS proxy for accessing internal services
compass gcp bastion --project production --ssh-flag "-D 8080"
```

### Troubleshooting

```bash
# Enable debug logging to see detailed connection process
compass gcp problematic-instance --project my-project --log-level debug

# Enable trace logging for maximum verbosity
compass gcp instance-name --project my-project --log-level trace
```

## Logging

compass provides structured logging with configurable levels:

- `trace`: Maximum verbosity, shows all API calls and decisions
- `debug`: Detailed information about discovery and connection process
- `info`: General information about connection progress (default)
- `warn`: Warning messages about fallbacks or potential issues
- `error`: Error messages for failed operations
- `fatal`: Critical errors that cause the program to exit
- `panic`: Severe errors that cause panic

## Error Handling

compass provides clear error messages for common scenarios:

- **Instance not found**: Searches through all zones and provides suggestions
- **No external IP and IAP disabled**: Clear explanation of connectivity requirements
- **Missing gcloud/ssh binaries**: Instructions for installing required tools
- **Authentication issues**: Guidance on setting up gcloud authentication

## Security Considerations

- compass uses your existing gcloud authentication and respects IAM policies
- IAP tunneling provides secure access without exposing instances to the internet
- SSH keys are managed through your GCP project's OS Login or metadata SSH keys
- All connections go through Google's secure infrastructure when using IAP

## Performance

- **Fast discovery**: Parallel zone searches for quick instance location
- **Minimal overhead**: Direct process replacement with ssh/gcloud (no persistent processes)
- **Efficient API usage**: Batch operations and intelligent caching

## Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature-name`
3. Make your changes with tests
4. Run linting: `task lint`
5. Run tests: `go test ./...`
6. Submit a pull request

### Development Setup

```bash
# Install dependencies
go mod download

# See all available tasks
task --list-all

# Build the application
task build

# Run tests
task test

# Run tests with coverage
task test-coverage

# Run linting
task lint

# Format code
task fmt

# Run all checks (fmt, vet, lint, test)
task check
```

### Available Tasks

The project uses [go-task](https://taskfile.dev/) for task automation. Here are the most commonly used tasks:

**Building:**
- `task build` - Build the application
- `task build-all` - Build for multiple platforms (Linux, macOS, Windows)
- `task install` - Install the application to `$GOPATH/bin`
- `task clean` - Clean build artifacts

**Testing:**
- `task test` - Run all tests
- `task test-short` - Run tests in short mode (skip integration tests)
- `task test-coverage` - Run tests with coverage report (HTML)
- `task test-coverage-func` - Run tests with function coverage report
- `task test-race` - Run tests with race detector
- `task test-unit` - Run only unit tests
- `task test-integration` - Run integration tests

**Code Quality:**
- `task lint` - Run golangci-lint
- `task lint-fix` - Run golangci-lint and auto-fix issues
- `task fmt` - Format code with go fmt
- `task vet` - Run go vet
- `task check` - Run all checks (fmt, vet, lint, test)

**Development:**
- `task run -- [args]` - Run the application with arguments
- `task dev` - Development mode with auto-reload (requires air)

**Other:**
- `task mod-tidy` - Tidy module dependencies
- `task ci` - Run CI checks
- `task release` - Build release binaries

Run `task --list-all` to see all available tasks with descriptions.

## Connectivity Testing

compass includes built-in support for Google Cloud Network Connectivity Tests, allowing you to validate network paths and diagnose connectivity issues between resources.

### Quick Start

```bash
# Test connectivity between two instances
compass gcp connectivity-test create web-to-db \
  --project my-project \
  --source-instance web-server-1 \
  --destination-instance db-server-1 \
  --destination-port 5432

# Get test results
compass gcp connectivity-test get web-to-db --project my-project

# List all tests
compass gcp connectivity-test list --project my-project
```

### Connectivity Test Commands

**Create a test:**
```bash
# Instance-to-instance test (automatic IP resolution)
compass gcp connectivity-test create web-to-db \
  --project my-project \
  --source-instance web-server \
  --destination-instance db-server \
  --destination-port 5432 \
  --protocol TCP

# IP-based test
compass gcp connectivity-test create ip-test \
  --project my-project \
  --source-ip 10.128.0.5 \
  --destination-ip 10.138.0.10 \
  --destination-port 443

# MIG to instance test
compass gcp connectivity-test create mig-test \
  --project my-project \
  --source-instance api-mig \
  --source-type mig \
  --destination-instance backend \
  --destination-port 8080

# With labels and description
compass gcp connectivity-test create tagged-test \
  --project my-project \
  --source-instance web \
  --destination-instance db \
  --destination-port 3306 \
  --description "Production database connectivity check" \
  --labels "env=prod,team=platform"
```

**Run an existing test:**
```bash
compass gcp connectivity-test run web-to-db --project my-project
```

**Get test results:**
```bash
# One-time check
compass gcp connectivity-test get web-to-db --project my-project

# Watch mode (poll until complete)
compass gcp connectivity-test get web-to-db --project my-project --watch

# Detailed output
compass gcp connectivity-test get web-to-db --project my-project --output detailed

# JSON output for automation
compass gcp connectivity-test get web-to-db --project my-project --output json
```

**List tests:**
```bash
# List all tests
compass gcp connectivity-test list --project my-project

# Table format
compass gcp connectivity-test list --project my-project --output table

# With filter
compass gcp connectivity-test list --project my-project --filter "labels.env=prod"
```

**Delete a test:**
```bash
# With confirmation prompt
compass gcp connectivity-test delete web-to-db --project my-project

# Force delete
compass gcp connectivity-test delete web-to-db --project my-project --force
```

### Connectivity Test Output

**Success Example:**
```
✓ Connectivity Test: web-to-db
  Status:        REACHABLE
  Source:        web-server-1 (10.128.0.5)
  Destination:   db-server-1 (10.138.0.10:5432)
  Protocol:      TCP

  Path Analysis:
  └─ VM Instance (web-server-1) → VPC Network → Firewall (allow-internal) → VM Instance (db-server-1)

  Result: Connection successful ✓
```

**Failure Example:**
```
✗ Connectivity Test: web-to-db
  Status:        UNREACHABLE
  Source:        web-server-1 (10.128.0.5)
  Destination:   db-server-1 (10.138.0.10:5432)
  Protocol:      TCP

  Path Analysis:
  └─ VM Instance (web-server-1) → VPC Network → Firewall (BLOCKED) ✗

  Result: Connection failed ✗

  Suggested Fix:
  Add firewall rule allowing TCP traffic from 10.128.0.5 to 10.138.0.10:5432
```

### Features

- **Automatic Instance Discovery**: Resolves instance names to IP addresses automatically
- **MIG Support**: Test connectivity from/to Managed Instance Groups
- **Zone Auto-Discovery**: Automatically finds instances across zones
- **Path Visualization**: Shows network path with each hop
- **Failure Diagnosis**: Provides suggestions for fixing connectivity issues
- **Multiple Output Formats**: Text, JSON, detailed, and table formats
- **Watch Mode**: Poll for test completion with automatic updates
- **Label Support**: Tag tests with custom labels for organization

### Use Cases

**Troubleshooting Connectivity Issues:**
```bash
# Quickly verify if application can reach database
compass gcp connectivity-test create quick-check \
  --project prod \
  --source-instance app-1 \
  --destination-instance db-primary \
  --destination-port 5432 \
  --watch
```

**Pre-deployment Validation:**
```bash
# Verify new service can reach required endpoints
compass gcp connectivity-test create deploy-validation \
  --project staging \
  --source-instance new-service \
  --destination-ip 10.0.1.100 \
  --destination-port 443
```

**Network Policy Validation:**
```bash
# Test firewall rules after changes
for port in 80 443 8080; do
  compass gcp connectivity-test create "web-port-${port}" \
    --project prod \
    --source-instance web-frontend \
    --destination-instance backend \
    --destination-port $port
done

# Review results
compass gcp connectivity-test list --project prod --output table
```

**CI/CD Integration:**
```bash
# Automated connectivity validation
compass gcp connectivity-test create ci-check \
  --project staging \
  --source-instance app \
  --destination-instance db \
  --destination-port 5432 \
  --output json | jq -e '.reachabilityDetails.result == "REACHABLE"'
```

## Roadmap

- [ ] AWS support for EC2 instances and ASG
- [ ] Connectivity test templates and presets
- [ ] Bulk connectivity test operations
- [ ] Export test results to various formats

## License

Private for now :)
OSS soon ?

## Support

For issues and feature requests, please use the GitHub issue tracker.
