# compass - Cloud connectivity toolset

> Main project on [Github](https://github.com/kedare/compass)

> Mirror: [Codeberg](https://codeberg.org/kedare/compass)


`compass` is a fast, intuitive CLI for reaching Google Cloud Platform (GCP) instances over SSH. It handles Identity-Aware Proxy (IAP) tunneling, Managed Instance Groups (MIGs), connectivity tests, and advanced SSH scenarios without extra configuration.

## Table of Contents
- [Overview](#overview)
- [Features](#features)
- [Installation](#installation)
  - [Download Pre-built Binary (Recommended)](#download-pre-built-binary-recommended)
  - [Build From Source](#build-from-source)
  - [Shell Completion](#shell-completion)
- [Quick Start](#quick-start)
- [Command Examples](#command-examples)
  - [SSH Connection Examples](#ssh-connection-examples)
  - [IP Lookup Examples](#ip-lookup-examples)
  - [VPN Inspection Examples](#vpn-inspection-examples)
  - [Connectivity Test Examples](#connectivity-test-examples)
- [Detailed Usage](#detailed-usage)
  - [SSH to Instances](#ssh-to-instances)
  - [Project Management](#project-management)
  - [IP Address Lookup](#ip-address-lookup)
  - [SSH Tunneling Recipes](#ssh-tunneling-recipes)
- [Local Cache](#local-cache)
- [Connectivity Tests](#connectivity-tests)
- [VPN Inspection](#vpn-inspection)
- [Development](#development)
- [CI/CD](#cicd)
- [Roadmap](#roadmap)
- [License](#license)
- [Support](#support)

## Overview

`compass` (or its alias `cps`) is a one-stop CLI for operating Google Cloud fleets. It discovers instances and Managed Instance Groups, builds the right SSH or IAP tunnel automatically, and reuses your `gcloud` credentials so access stays seamless. Beyond remote access, it can launch Connectivity Tests, surface VPN topologies, and stream structured logs in whatever format you need for automation. Whether you are diagnosing reachability, crafting port-forwarding recipes, or auditing gateways, `compass` keeps workflows fast and scriptable.

## Features

- 🚀 Quick SSH connections with automatic instance and zone discovery
- 🔒 Identity-Aware Proxy (IAP) tunneling support
- 🎯 Managed Instance Group (MIG) support for regional and zonal groups
- 🌐 Zone and region auto-discovery when omitted
- 📁 Multi-project support with interactive project selection and caching
- 🔧 Pass arbitrary SSH flags for tunneling, forwarding, or X11
- 🔍 IP address lookup across projects with subnet caching for fast resolution
- 🌐 Network connectivity tests powered by Google Cloud Connectivity Tests API
- 🔭 Cloud VPN inventory across gateways, tunnels, and BGP peers
- 💾 Intelligent local cache for instant connections to known resources
- 📊 Structured logging with configurable verbosity and clean spinner-based progress
- ⚡ Zero configuration—relies on existing `gcloud` authentication
- 🎨 Helpful CLI UX with actionable errors

## Installation

### Prerequisites

- `gcloud` CLI installed and authenticated ([installation guide](https://cloud.google.com/sdk/docs/install))
- `ssh` available in your `PATH`

### Download Pre-built Binary (Recommended)

Download the latest release for your platform from [GitHub Releases](https://github.com/kedare/compass/releases).

**macOS (Intel):**
```bash
curl -L https://github.com/kedare/compass/releases/latest/download/compass-darwin-amd64 -o compass
chmod +x compass
sudo mv compass /usr/local/bin/
compass version
```

**macOS (Apple Silicon):**
```bash
curl -L https://github.com/kedare/compass/releases/latest/download/compass-darwin-arm64 -o compass
chmod +x compass
sudo mv compass /usr/local/bin/
compass version
```

**Linux (amd64):**
```bash
curl -L https://github.com/kedare/compass/releases/latest/download/compass-linux-amd64 -o compass
chmod +x compass
sudo mv compass /usr/local/bin/
compass version
```

**Linux (arm64):**
```bash
curl -L https://github.com/kedare/compass/releases/latest/download/compass-linux-arm64 -o compass
chmod +x compass
sudo mv compass /usr/local/bin/
compass version
```

**Windows:**

Download `compass-windows-amd64.exe` from the [releases page](https://github.com/kedare/compass/releases), rename it to `compass.exe`, and add it to your PATH.

### Build From Source

If you prefer to build from source or want to contribute:

**Prerequisites:**
- Go 1.19 or newer
- [`go-task`](https://taskfile.dev/) (optional, for development tasks)

**Build:**
```bash
git clone https://github.com/kedare/compass.git
cd compass
go build -o compass .
./compass --help
```

**With go-task (for development):**
```bash
task build  # Builds with version metadata
```

### Shell Completion

Enable autocompletion for your shell:

```bash
# Bash
compass completion bash > /etc/bash_completion.d/compass

# Zsh
compass completion zsh > "${fpath[1]}/_compass"

# Fish
compass completion fish > ~/.config/fish/completions/compass.fish

# PowerShell
compass completion powershell | Out-String | Invoke-Expression
```

## Quick Start

```bash
# Simple SSH to an instance (discovers project, zone automatically if cached)
compass gcp ssh my-instance

# First time connecting - specify the project
compass gcp ssh my-instance --project my-gcp-project

# Look up which resources use a specific IP address
compass gcp ip lookup 10.201.0.208

# Import projects for multi-project operations
compass gcp projects import

# Inspect VPN gateways
compass gcp vpn list --project prod

# Test connectivity between instances
compass gcp connectivity-test create web-to-db \
  --project prod \
  --source-instance web-1 \
  --destination-instance db-1 \
  --destination-port 5432
```

## Command Examples

### SSH Connection Examples

**Basic connection (cached project and zone):**
```console
$ compass gcp ssh db-healer-1
INFO  Starting connection process for: db-healer-1
INFO  Connecting to instance: db-healer-1 of project my-project in zone: us-central1-b
INFO  Establishing SSH connection via IAP tunnel...
user@db-healer-1:~$
```

**First-time connection with project:**
```bash
compass gcp ssh web-server --project prod-project
```

**Connect to a specific zone:**
```bash
compass gcp ssh api-server --project prod --zone europe-west1-b
```

**Connect to a Managed Instance Group (MIG):**
```bash
# Auto-detects MIG and lets you choose an instance
compass gcp ssh my-mig --project prod --type mig
```

**Port forwarding through IAP:**
```bash
# Forward local port 3000 to instance port 3000
compass gcp ssh app-server --project prod --ssh-flag "-L 3000:localhost:3000"

# Multiple ports
compass gcp ssh multi-service --project prod \
  --ssh-flag "-L 3000:localhost:3000" \
  --ssh-flag "-L 5432:database:5432"
```

**SOCKS proxy:**
```bash
compass gcp ssh jump-host --project prod --ssh-flag "-D 1080"
```

**Multi-project search:**
```console
$ compass gcp ssh unknown-instance
INFO  Starting connection process for: unknown-instance
Searching for instance unknown-instance across 5 projects
INFO  Completed project staging (1/5 done)
INFO  Completed project dev (2/5 done)
INFO  Found in project prod
INFO  Connecting to instance: unknown-instance of project prod in zone: us-west1-a
INFO  Establishing SSH connection via IAP tunnel...
user@unknown-instance:~$
```

### IP Lookup Examples

**Basic IP lookup (scans cached projects):**
```bash
compass gcp ip lookup 10.201.0.208
```

**Output example:**
```console
$ compass gcp ip lookup 10.201.0.208
INFO  Starting IP lookup for: 10.201.0.208
Searching IP associations
INFO  Completed project my-project (1/3 done)
INFO  Completed project staging-project (2/3 done)
INFO  Completed project prod-project (3/3 done)
✓ Lookup complete

IP Address: 10.201.0.208

Instances:
  • db-server-1 (my-project, us-central1-a)
    Internal interface, network=default, subnet=default

Subnets:
  • default (my-project, us-central1)
    network=default, cidr=10.201.0.0/24, range=primary
```

**Lookup in specific project:**
```bash
compass gcp ip lookup 10.0.0.5 --project prod
```

**JSON output for automation:**
```bash
compass gcp ip lookup 34.120.0.10 --output json | jq '.[] | select(.kind == "instance_internal")'
```

**Table format:**
```bash
compass gcp ip lookup 10.201.0.208 --output table
```

### VPN Inspection Examples

**List all VPN gateways:**
```console
$ compass gcp vpn list --project prod
🔐 Gateway: prod-ha-vpn (us-central1)
  Network:     prod-vpc
  Interfaces:
    - #0 IP: 35.242.106.234
    - #1 IP: 35.220.88.140
  Tunnels:
    • prod-to-eu (us-central1)
      Peer IP:      203.0.113.10
      Router:       prod-router
      Status:       ESTABLISHED
      IKE Version:  2
      BGP Peers:
        - prod-peer-eu (169.254.0.2, ASN 65001, enabled)
          Advertised Routes: 5
          Learned Routes: 3
```

**Table view:**
```bash
compass gcp vpn list --project prod --output table
```

**Inspect specific gateway:**
```bash
compass gcp vpn get prod-ha-vpn --type gateway --region us-central1
```

**Inspect specific tunnel:**
```bash
compass gcp vpn get prod-to-eu --type tunnel --region us-central1 --output json
```

**Hide warnings:**
```bash
compass gcp vpn list --project prod --warnings=false
```

### Connectivity Test Examples

**Create a connectivity test:**
```bash
compass gcp connectivity-test create web-to-db \
  --project prod \
  --source-instance web-server-1 \
  --destination-instance db-server-1 \
  --destination-port 5432
```

**Watch test progress:**
```bash
compass gcp connectivity-test get web-to-db --project prod --watch
```

**List all tests:**
```bash
# Text format
compass gcp connectivity-test list --project prod

# Table format
compass gcp ct list --project prod --output table
```

**IP-to-IP test:**
```bash
compass gcp ct create ip-test \
  --project prod \
  --source-ip 10.128.0.5 \
  --destination-ip 10.138.0.10 \
  --destination-port 443 \
  --protocol TCP
```

**Rerun existing test:**
```bash
compass gcp ct run web-to-db --project prod
```

**Delete test:**
```bash
compass gcp ct delete web-to-db --project prod --force
```

## Detailed Usage

### SSH to Instances

**Basic command:**
```bash
compass gcp ssh [instance-name] [flags]
```

**Flags:**

| Flag | Aliases | Description | Default |
|------|---------|-------------|---------|
| `--project` | `-p` | GCP project ID | Auto-discovered from cache if available |
| `--zone` | `-z` | GCP zone | Auto-discovered from cache or API |
| `--type` | `-t` | Resource type: `instance` or `mig` | Auto-detected (tries MIG first, then instance) |
| `--ssh-flag` | | Additional SSH flags (can be used multiple times) | None |

**Global flags:**

| Flag | Description | Default |
|------|-------------|---------|
| `--cache` | Enable/disable cache usage | `true` |
| `--concurrency` | Maximum number of concurrent operations (worker pool size) | `10` |
| `--log-level` | Set logging level (`trace`, `debug`, `info`, `warn`, `error`, `fatal`) | `info` |

> **Note:** The `--concurrency` flag controls how many projects are scanned simultaneously during multi-project searches (SSH instance discovery, IP lookups) and also limits the number of concurrent progress spinners displayed. Increase this value for faster searches across many projects, or decrease it to reduce API rate limit usage.

**When you target a managed instance group**, `compass gcp ssh` lists the running members and lets you choose the instance to connect to; if there's only one, it connects automatically.

### Project Management

Import and manage the list of GCP projects that Compass will search across for multi-project operations:

```bash
compass gcp projects import
```

This command discovers all GCP projects you have access to and presents an interactive selection menu. Choose which projects to cache for future operations like IP lookups or instance searches. Only selected projects will be scanned during multi-project operations.

**Output example:**
```console
INFO  Discovering all accessible GCP projects...
INFO  Found 47 projects
Select projects to cache (use arrow keys, space to select, enter to confirm):
  [ ] project-dev-123
  [x] project-staging-456
  [x] project-prod-789
  [ ] project-archive-old
  ...
INFO  Selected 2 projects
INFO  Successfully cached 2 projects
✓ Projects imported successfully! You can now use 'compass gcp ssh' without --project flag.
```

### IP Address Lookup

Identify which resources are assigned to a specific IP address:

```bash
# Scan all cached projects
compass gcp ip lookup <ip-address>

# Specific project
compass gcp ip lookup <ip-address> --project <project-id>

# Different output formats
compass gcp ip lookup <ip-address> --output [text|table|json]
```

**What it finds:**
- Compute Engine VM instances (internal and external IPs)
- Forwarding rules (load balancers)
- Reserved addresses
- Subnet ranges (primary, secondary, IPv6)

**How it works:**
1. When no project is specified, scans all cached projects
2. Remembers subnets during scanning
3. On subsequent runs, checks cached subnet ranges first to identify likely projects
4. Falls back to full scan if cache miss occurs

**Requirements:**
- At least one cached project (use `compass gcp projects import`), OR
- Explicit `--project` flag

### SSH Tunneling Recipes

**Local port forwarding:**
```bash
compass gcp ssh app-server --project prod --ssh-flag "-L 3000:localhost:3000"
```

**Remote port forwarding:**
```bash
compass gcp ssh jump-host --project staging --ssh-flag "-R 8080:localhost:8080"
```

**SOCKS proxy:**
```bash
compass gcp ssh proxy-server --project dev --ssh-flag "-D 1080"
```

**X11 forwarding:**
```bash
compass gcp ssh desktop-instance --project dev --ssh-flag "-X"
```

**Multiple tunnels at once:**
```bash
compass gcp ssh multi-service \
  --project prod \
  --ssh-flag "-L 3000:service1:3000" \
  --ssh-flag "-L 4000:service2:4000" \
  --ssh-flag "-L 5000:database:5432"
```

**Jump host / bastion:**
```bash
compass gcp ssh internal-server \
  --project prod \
  --ssh-flag "-J bastion.example.com"
```

## Local Cache

`compass` keeps a small JSON cache on disk so you do not have to repeat the same discovery calls on every run. The cache lives at `~/.compass.cache.json` with `0600` permissions and is refreshed transparently.

**What's cached:**

- **Project list**: Projects you've selected via `compass gcp projects import` are stored for multi-project operations. These are used when you run commands like `compass gcp ssh` or `compass gcp ip lookup` without specifying a `--project` flag. Entries expire after 30 days of inactivity.

- **Resource locations**: Once `compass gcp ssh` resolves a VM or MIG, its project, zone/region, and resource type are stored for 30 days. When you omit `--project`, `--zone`, or `--type` on subsequent connections, the CLI reuses the cached metadata instantly without making API calls. Providing any of these flags bypasses the cache for that specific parameter.

- **Zone listings**: Discovered zones for a project are cached for 30 days to speed up future region/zone discovery without additional API calls.

- **Subnet metadata**: As `compass gcp ip lookup` crawls projects, it records subnets (primary/secondary CIDRs, IPv6 range, gateway, network, and region). Future IP lookups check these cached subnet ranges first to identify which projects likely contain the IP, dramatically reducing the number of projects that need to be scanned.

**Cache behavior:**

Every cache access updates its timestamp, keeping frequently-used entries fresh. Stale entries are pruned automatically when they exceed 30 days of inactivity.

**Cache management:**

```bash
# Reset cache completely
rm ~/.compass.cache.json

# Disable cache for a single command
compass --cache=false gcp ip lookup 10.0.0.1
compass --cache=false gcp ssh my-instance
```

## Connectivity Tests

Connectivity tests let you validate reachability between GCP resources using the Google Cloud Connectivity Tests API.

> **Tip:** Use the shorter `ct` alias for connectivity-test commands: `compass gcp ct list` instead of `compass gcp connectivity-test list`

**Environment variable:** Set `COMPASS_OUTPUT` to change the default output format (supported values: `text`, `table`, `json`, `detailed`). If the variable is unset, list commands default to `table` while detailed views use `text`.

### Create a Test

```bash
compass gcp connectivity-test create <test-name> \
  --project <project> \
  --source-instance <instance> \
  --destination-instance <instance> \
  --destination-port <port>
```

**Options:**
- `--source-instance` / `--source-ip` - Source endpoint
- `--destination-instance` / `--destination-ip` - Destination endpoint
- `--source-type` / `--destination-type` - Resource type (`instance` or `mig`)
- `--destination-port` - Port number
- `--protocol` - Protocol (TCP, UDP, ICMP, ESP, AH, SCTP, GRE)
- `--labels` - Labels in `key=value,key2=value2` format
- `--source-network` / `--destination-network` - Custom VPC networks
- `--source-project` / `--destination-project` - Cross-project tests

### Watch, Get, Rerun

```bash
# Watch test until completion
compass gcp ct get <test-name> --project <project> --watch

# Get test results
compass gcp ct get <test-name> --project <project>

# Rerun test
compass gcp ct run <test-name> --project <project>
```

### List and Delete

```bash
# List tests
compass gcp ct list --project <project>
compass gcp ct list --project <project> --output table

# Filter by labels
compass gcp ct list --project <project> --filter "labels.env=prod"

# Limit results
compass gcp ct list --project <project> --limit 10

# Delete test
compass gcp ct delete <test-name> --project <project>
compass gcp ct delete <test-name> --project <project> --force  # Skip confirmation
```

**See [Command Examples](#connectivity-test-examples) for detailed output examples.**

## VPN Inspection

Inspect Cloud VPN gateways, tunnels, and Cloud Router BGP sessions across your project.

```bash
# List all VPN resources
compass gcp vpn list --project <project>

# Table format
compass gcp vpn list --project <project> --output table

# JSON format for automation
compass gcp vpn list --project <project> --output json

# Hide warnings
compass gcp vpn list --project <project> --warnings=false

# Inspect specific gateway
compass gcp vpn get <gateway-name> --type gateway --region <region>

# Inspect specific tunnel
compass gcp vpn get <tunnel-name> --type tunnel --region <region>
```

**Output includes:**
- Gateway interfaces and IPs
- Tunnel status and peer information
- BGP peer configuration
- Advertised and learned routes
- Configuration warnings (orphaned tunnels, missing configurations, etc.)

**See [VPN Inspection Examples](#vpn-inspection-examples) for detailed output examples.**

## Development

Use the `Taskfile.yml` to build, lint, and test consistently.

**Common tasks:**
```bash
task build              # Compile binary with version metadata
task run -- gcp ssh my-instance --project my-project  # Run without building
task fmt                # Format code
task lint               # Run linters
task vet                # Static analysis
task test               # Run all tests
task test-short         # Run unit tests only
task test-integration   # Run integration tests
task test-coverage      # Generate coverage reports
task dev                # Hot-reload development (requires air)
```

**Requirements for development:**
- Go 1.19 or newer
- [`go-task`](https://taskfile.dev/)
- [`golangci-lint`](https://golangci-lint.run/) for linting
- [`air`](https://github.com/cosmtrek/air) for hot-reload (optional)

Remove compiled binaries like `./compass` before committing changes.

## CI/CD

GitHub Actions run `task check` (fmt, vet, lint, test) and `task test-race` on pull requests and pushes to `main`. Tagging a commit triggers an automated release that repeats `task check`, verifies formatting stays clean, rebuilds via `task build`, and publishes release notes with pre-built binaries for multiple platforms.

## Roadmap

- [ ] AWS support for EC2 instances and Auto Scaling Groups
- [ ] Connectivity test templates and presets
- [ ] Bulk connectivity test operations
- [ ] Export connectivity results to additional formats
- [ ] Cache management commands (list, remove specific entries)
- [ ] Interactive MIG instance selection improvements

## License

Licensed under the [Apache License 2.0](LICENSE).

## Support

For issues or feature requests, open an issue on [GitHub](https://github.com/kedare/compass/issues).
