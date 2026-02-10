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
  - [Update Existing Installation](#update-existing-installation)
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
  - [Cache Management Commands](#cache-management-commands)
  - [Smart Search Learning](#smart-search-learning)
- [Connectivity Tests](#connectivity-tests)
- [VPN Inspection](#vpn-inspection)
- [Interactive TUI](#interactive-tui)
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
- 💾 Intelligent local cache with smart search learning for instant connections
- 🧠 Search affinity system that learns your patterns and prioritizes relevant projects
- 🔎 Global resource search across 22 GCP resource types with fuzzy matching and highlight
- 🖥️ Interactive TUI (terminal UI) with keyboard-driven navigation (k9s-style)
- 🧮 Advanced filtering with AND (spaces), OR (`|`), and NOT (`-`) operators across all views
- 📊 Structured logging with configurable verbosity and clean spinner-based progress
- ⚡ Zero configuration—relies on existing `gcloud` authentication
- 🔁 In-place upgrades via `compass update` to pull the latest GitHub release
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
task build
dist/compass --help
```

**With go-task (for development):**
```bash
task build  # Builds with version metadata
```

### Update Existing Installation

Keep your local binary up to date with the newest GitHub release:

```bash
compass update
```

The command detects your platform, downloads the matching artifact, verifies the SHA-256 checksum reported by the GitHub release API, and replaces the running executable in place. On Windows the binary cannot be swapped while the process is running, so `compass update` writes a fresh executable alongside your current one (e.g. `compass.new.exe`) and prints follow-up instructions to complete the swap after you exit the CLI.

- Dry run without download: `compass update --check`
- Reinstall the latest version even when already up to date: `compass update --force`

### Shell Completion

Enable autocompletion for your shell (make sure your shell is properly configured to load autocompletion from this directory):

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
compass gcp ip lookup 192.168.0.208

# Import projects for multi-project operations (interactive)
compass gcp projects import

# Import projects matching a regex pattern (non-interactive)
compass gcp projects import --regex "^prod-"

# Search cached projects for instances with matching names
compass gcp search piou

# Inspect VPN gateways
compass gcp vpn list --project prod

# Launch the interactive TUI
compass interactive

# Update to the latest published release
compass update

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
$ compass gcp ssh db-instance-1
INFO  Starting connection process for: db-instance-1
INFO  Connecting to instance: db-instance-1 of project my-project in zone: us-central1-b
INFO  Establishing SSH connection via IAP tunnel...
user@db-instance-1:~$
```

**First-time connection with project:**
```bash
compass gcp ssh web-server --project prod-project
```

**Connect to a specific zone:**
```bash
compass gcp ssh api-server --project prod --zone europe-south1-b
```

**Connect to a Managed Instance Group (MIG):**
```bash
# Auto-detects MIG and lets you choose an instance
compass gcp ssh my-mig --project prod --type mig
```

**Force or disable IAP tunneling:**
```bash
# Always go through IAP even if the VM has a public IP
compass gcp ssh bastion --project prod --iap=true

# Prefer a direct SSH session when the VM exposes a public IP
compass gcp ssh metrics-host --project prod --iap=false
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

### Resource Search Examples

Use `compass gcp search` to scan every cached project (or a `--project` override) for resource names containing your query. The search covers a wide range of GCP resources and prints a table with type, project, location, name, and details.

```console
$ compass gcp search piou
TYPE              PROJECT       LOCATION         NAME          DETAILS
compute.instance  prod-project  us-central1-b    piou-runner   status=RUNNING, machineType=e2-medium
```

**Searchable resource types (22):**

| Type | Kind | Details shown |
|------|------|---------------|
| Compute Engine instances | `compute.instance` | Status, machine type |
| Managed Instance Groups | `compute.mig` | Location, regional/zonal |
| Instance templates | `compute.instanceTemplate` | Machine type |
| IP address reservations | `compute.address` | Address, type, status, description, subnetwork, users |
| Persistent disks | `compute.disk` | Size, type, status |
| Disk snapshots | `compute.snapshot` | Size, source disk, status |
| Cloud Storage buckets | `storage.bucket` | Location, storage class |
| Forwarding rules | `compute.forwardingRule` | IP, protocol, port range, scheme |
| Backend services | `compute.backendService` | Protocol, scheme, backend count |
| Target pools | `compute.targetPool` | Session affinity, instance count |
| Health checks | `compute.healthCheck` | Type, port |
| URL maps | `compute.urlMap` | Default service, host rules |
| Cloud SQL instances | `sqladmin.instance` | Region, version, tier, state |
| GKE clusters | `container.cluster` | Location, status, version, node count |
| GKE node pools | `container.nodePool` | Cluster, machine type, node count |
| VPC networks | `compute.network` | Auto-create subnets, subnet count |
| VPC subnets | `compute.subnet` | Region, network, CIDR, purpose |
| Cloud Run services | `run.service` | Region, URL, latest revision |
| Firewall rules | `compute.firewall` | Network, direction, priority, description, source ranges, target tags, allowed protocols |
| Secret Manager secrets | `secretmanager.secret` | Replication type |
| HA VPN gateways | `compute.vpnGateway` | Network, interface count, IPs |
| VPN tunnels | `compute.vpnTunnel` | Status, peer IP, IKE version, gateway |
| VPC routes | `compute.route` | Destination range, network, priority, next hop, route type, tags |

- Run `compass gcp projects import` first so the search knows which projects to inspect.
- Use `--project <id>` when you want to bypass the cache and only inspect a single project.
- Search matches against resource names and detail fields (e.g. description, IP addresses, tags).
- In the TUI, press `Tab` to toggle fuzzy matching and `/` to filter results with AND/OR/NOT operators.

### IP Lookup Examples

**Basic IP lookup (scans cached projects):**
```bash
compass gcp ip lookup 192.168.0.208
```

**Output example:**
```console
$ cps gcp ip lookup 192.168.0.208
Found 3 association(s):

- gcp-dev-apps • Reserved address
  Resource: app-lb-internal-devops-platform
  IP:       192.168.0.208/20
  Path:     gcp-dev-apps > europe-south1 > default-subnet
  Details:  status=in_use, purpose=shared_loadbalancer_vip, tier=premium, type=internal

- gcp-dev-apps • Forwarding rule
  Resource: fwr-internal-devops-platform-1234
  IP:       192.168.0.208/20
  Path:     gcp-dev-apps > app-net > global > default-subnet
  Details:  scheme=internal_managed, ports=8080-8080, target=tp-internal-devops-platform-1234

- gcp-dev-apps • Subnet range
  Resource: default-subnet
  Subnet:   default-subnet (192.168.0.0/20)
  Path:     gcp-dev-apps > app-net > europe-south1 > default-subnet
  Details:  range=primary, usable=192.168.0.1-192.168.15.254, gateway=192.168.0.1
  Notes:    Subnet range 192.168.0.0/20 (primary)
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
compass gcp ip lookup 192.168.0.208 --output table
```

### VPN Inspection Examples

**List all VPN gateways:**
```console
$ compass gcp vpn list --project prod

🔐 Gateway: vpn-esp-office (europe-south1)
  Description: VPN example
  Network:     hub-net
  Interfaces:
    - #0 IP: 34.56.78.1
    - #1 IP: 34.56.79.1
  Tunnels:
    • ha-tun-vpn-esp-office-a (europe-south1)
      IPSec Peer:  <local 34.56.78.1>  ↔  <remote 185.70.0.2>
      Peer Gateway: peer-vpn-esp-office
      Router:       router-esp-office
      Status:       ESTABLISHED
      Detail:       Tunnel is up and running.
      IKE Version:  2
      BGP Peers:
        - bgp-0-ha-tun-vpn-esp-office-a endpoints <local 169.254.0.5 AS64531> ↔ <remote 169.254.0.6 AS65502> status UP/ESTABLISHED, received 1, advertised 1
            Advertised: 192.168.89.128/29
            Received:   192.168.90.0/24
    • ha-tun-vpn-esp-office-b (europe-south1)
      IPSec Peer:  <local 34.56.79.1>  ↔  <remote 185.70.0.2>
      Peer Gateway: peer-vpn-esp-office
      Router:       router-esp-office
      Status:       ESTABLISHED
      Detail:       Tunnel is up and running.
      IKE Version:  2
      BGP Peers:
        - bgp-0-ha-tun-vpn-esp-office-b endpoints <local 169.254.44.5 AS64531> ↔ <remote 169.254.44.6 AS65510> status UP/ESTABLISHED, received 1, advertised 1
            Advertised: 192.168.89.128/29
            Received:   192.168.90.0/24

⚠️  Orphan Tunnels (not attached to HA VPN gateways):
  • tun-vpn-fr-a (europe-south1) peers <local ?>  ↔  <remote 15.68.34.23>
    Status: ESTABLISHED
  • tun-vpn-uk-b (europe-south1) peers <local ?>  ↔  <remote 37.48.54.102>
    Status: ESTABLISHED
  • tun-vpn-nyc-a (europe-south1) peers <local ?>  ↔  <remote 92.167.34.152>
    Status: ESTABLISHED

⚠️  Orphan BGP Sessions (no tunnel association):
  • vpn-bgp-session-1234 on router router-vpn-main (europe-south1) endpoints <local ? AS65501> ↔ <remote ? AS0> status UNKNOWN, received 0, advertised 0

⚠️  Gateways With No Tunnels:
  • ha-vpn-gw-dev-app-net (europe-south1) - 2 interface(s) configured but no tunnels

⚠️  Tunnels Not Receiving BGP Routes:
  • ha-tun-apps-health-eusouth1-a (europe-south1) on router rt-apps-europe-south1 - peer bgp-0-ha-tun-apps-health-eusouth1-a status UP/ESTABLISHED
  • ha-tun-apps-health-eusouth1-b (europe-south1) on router rt-apps-europe-south1 - peer bgp-0-ha-tun-apps-health-eusouth1-b status UP/ESTABLISHED
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

**Get an axisting test**
```bash
compass gcp ct get my-test
✓ Connectivity Test: my-test
  Console URL:   https://console.cloud.google.com/net-intelligence/connectivity/tests/details/my-test?project=testing-project
  Forward Status: REACHABLE
  Return Status:  REACHABLE
  Source:        10.0.0.1
  Destination:   192.168.0.1:8080
  Protocol:      TCP

  Path Analysis:
    Forward Path
    # | Step | Type        | Resource                                            | Status
    1 | →    | VM Instance | gke-health-dev-default-pool-1234-1234               | OK
    2 | →    | Firewall    | default-allow-egress                                | ALLOWED
    3 | →    | Route       | peering-route-1234                                  | OK
    4 | →    | VM Instance | gke-test-dev-europe-wes-default2-pool-1234-1234     | OK
    5 | →    | Firewall    | gce-1234                                            | ALLOWED
    6 | ✓    | Step        | Final state: packet delivered to instance.          | DELIVER

    Return Path
    # | Step | Type        | Resource                                             | Status
    1 | →    | VM Instance | gke-test-dev-europe-wes-default2-pool-1234-1234      | OK
    2 | →    | Step        | Config checking state: verify EGRESS firewall rule.  | APPLY_EGRESS_FIREWALL_RULE
    3 | →    | Route       | peering-route-1234                                   | OK
    4 | →    | VM Instance | gke-health-dev-default-pool-1234-1234                | OK
    5 | →    | Step        | Config checking state: verify INGRESS firewall rule. | APPLY_INGRESS_FIREWALL_RULE
    6 | ✓    | Step        | Final state: packet delivered to instance.           | DELIVER

  Result: Connection successful ✓
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
| `--iap` | | Force or disable IAP tunneling (`true`/`false`). Compass remembers your choice per instance via the cache. | Automatic (IAP only when the instance lacks an external IP) |

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

**Regex filtering (non-interactive):**

Use `--regex` / `-r` to import projects matching a pattern without interactive selection:

```bash
# Import all projects starting with "prod-"
compass gcp projects import --regex "^prod-"

# Import projects containing "dev" or "staging"
compass gcp projects import -r "dev|staging"

# Case-insensitive matching
compass gcp projects import -r "(?i)^PROD-"
```

This is useful for automation, CI/CD pipelines, or when you want to quickly import a known set of projects.

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

`compass` maintains a local SQLite cache to avoid repeated API discovery calls. The cache lives at `~/.cache/compass/cache.db` with `0600` permissions and is updated transparently.

**What's cached:**

- **Project list**: Projects you've selected via `compass gcp projects import` are stored for multi-project operations. These are used when you run commands like `compass gcp ssh` or `compass gcp ip lookup` without specifying a `--project` flag. Entries expire after 30 days of inactivity.

- **Resource locations**: Once `compass gcp ssh` resolves a VM or MIG, its project, zone/region, and resource type are stored for 30 days. When you omit `--project`, `--zone`, or `--type` on subsequent connections, the CLI reuses the cached metadata instantly without making API calls. Providing any of these flags bypasses the cache for that specific parameter.

- **IAP preferences**: When you pass `--iap=true` or `--iap=false`, the selection is stored alongside the instance metadata so future `compass gcp ssh` runs reuse the same tunneling preference automatically.

- **Zone listings**: Discovered zones for a project are cached for 30 days to speed up future region/zone discovery without additional API calls.

- **Subnet metadata**: As `compass gcp ip lookup` crawls projects, it records subnets (primary/secondary CIDRs, IPv6 range, gateway, network, and region). Future IP lookups check these cached subnet ranges first to identify which projects likely contain the IP, dramatically reducing the number of projects that need to be scanned.

- **Search affinity**: The cache learns which search terms find results in which projects. This data is used to prioritize projects during future searches, making repeat searches significantly faster.

**Cache behavior:**

Every cache access updates its `last_used` timestamp, keeping frequently-used entries fresh. Resources and projects are ordered by most recently used, so your commonly accessed items appear first. Stale entries are pruned automatically when they exceed 30 days of inactivity. The SQLite database uses WAL (Write-Ahead Logging) mode for efficient concurrent access.

**Migration from JSON cache:**

If you're upgrading from a previous version that used JSON-based caching (`~/.compass.cache.json`), your data will be automatically migrated to SQLite on first run. The old JSON file is renamed to `.compass.cache.json.migrated` after successful migration.

### Cache Management Commands

```bash
# View cache information (size, entry counts, TTL settings)
compass cache info

# Configure TTL settings per resource type
compass cache ttl get                    # Show all TTL settings
compass cache ttl get instances          # Show specific TTL
compass cache ttl set instances 168h     # Set instance TTL to 7 days
compass cache ttl clear instances        # Reset to default

# Run database optimization (VACUUM and ANALYZE)
compass cache optimize

# Clear all cached entries (preserves TTL settings)
compass cache clear

# Execute custom SQL queries on the cache
compass cache sql "SELECT * FROM instances LIMIT 10"
compass cache sql -f query.sql

# Disable cache for a single command
compass --cache=false gcp ip lookup 10.0.0.1
compass --cache=false gcp ssh my-instance
```

### Smart Search Learning

The cache learns from your search patterns to prioritize projects more intelligently:

1. **Search affinity tracking**: When you search for "netmgt-dt40" and find results in "project-network", this association is recorded
2. **Prefix learning**: The prefix "netmgt" is also associated with "project-network"
3. **Smart prioritization**: Future searches for anything starting with "netmgt" will search "project-network" first
4. **Scoring algorithm**: Combines frequency (how often), recency (how recent), and volume (how many results) to rank projects

This means the more you use compass, the faster your searches become as it learns your patterns.

**Why SQLite?**

The SQLite cache provides several advantages over the previous JSON-based approach:
- **Efficient updates**: Individual records are updated without rewriting the entire cache
- **Better concurrency**: Native transaction support with WAL mode
- **ACID compliance**: Safe writes with crash recovery
- **Indexed lookups**: Faster subnet IP matching and project searches
- **Smart learning**: Search affinity data enables intelligent project prioritization

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

## Interactive TUI

Compass includes a full terminal UI for interactive exploration, accessible via:

```bash
compass interactive   # or: compass i / compass tui
```

The TUI provides keyboard-driven navigation similar to [k9s](https://k9scli.io/):

**Main view — Instance browser:**
- `s` — SSH to the selected instance
- `d` — Show instance details
- `b` — Open in Cloud Console (browser)
- `/` — Filter the displayed list
- `Shift+S` — Global resource search
- `Shift+R` — Refresh instance list
- `v` — VPN view
- `c` — Connectivity tests view
- `i` — IP lookup view
- `?` — Help
- `Esc` — Quit (or clear active filter)

**Global search (`Shift+S`):**
- Searches across all 22 resource types in cached projects
- Results appear progressively as they're found
- `Tab` — Toggle fuzzy matching (e.g. "prd" matches "production")
- `/` — Filter displayed results
- `d` — Show details for a result
- `s` — SSH to an instance result
- `b` / `o` — Open in browser

### Filtering

All TUI views share the same filter syntax. Press `/` to enter filter mode, then type your query:

| Operator | Syntax | Example | Meaning |
|----------|--------|---------|---------|
| AND | spaces | `web prod` | Must contain both "web" AND "prod" |
| OR | pipe `\|` | `web\|api` | Must contain "web" OR "api" |
| NOT | dash `-` | `-dev` | Must NOT contain "dev" |

Operators can be combined: `web|api prod -staging` matches resources containing ("web" or "api") AND "prod" but NOT "staging".

Press `Enter` to apply the filter, `Esc` to cancel.

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
- [ ] Interactive MIG instance selection improvements

## License

Licensed under the [Apache License 2.0](LICENSE).

## Support

For issues or feature requests, open an issue on [GitHub](https://github.com/kedare/compass/issues).
