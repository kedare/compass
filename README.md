# compass - Cloud Connectiviy toolset

> Main project location : [Codeberg](https://codeberg.org/kedare/compass) : This is where you should open issues and PR and download releases

> Mirror on [Github](https://github.com/kedare/compass) : Read only mirror

`compass` is a fast, intuitive CLI for reaching Google Cloud Platform (GCP) instances over SSH. It handles Identity-Aware Proxy (IAP) tunneling, Managed Instance Groups (MIGs), connectivity tests, and advanced SSH scenarios without extra configuration.

## Table of Contents
- [Overview](#overview)
- [Features](#features)
- [Installation](#installation)
  - [Prerequisites](#prerequisites)
  - [Install go-task](#install-go-task)
  - [Build From Source](#build-from-source)
  - [Shell Completion](#shell-completion)
- [Quick Start](#quick-start)
- [Usage](#usage)
  - [Basic Command](#basic-command)
  - [Advanced Options](#advanced-options)
  - [SSH Tunneling Recipes](#ssh-tunneling-recipes)
- [Connectivity Tests](#connectivity-tests)
  - [Create](#create)
  - [Watch Progress](#watch-progress)
  - [Get Results](#get-results)
  - [Rerun a Test](#rerun-a-test)
  - [List and Delete](#list-and-delete)
  - [Output Formats](#output-formats)
  - [Common Use Cases](#common-use-cases)
- [VPN Overview](#vpn-overview)
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
- 🔧 Pass arbitrary SSH flags for tunneling, forwarding, or X11
- 🔍 Network connectivity tests powered by Google Cloud Connectivity Tests API
- 🔭 Cloud VPN inventory across gateways, tunnels, and BGP peers
- 📊 Structured logging with configurable verbosity
- ⚡ Zero configuration—relies on existing `gcloud` authentication
- 🎨 Helpful CLI UX with actionable errors

## Installation

### Prerequisites

- Go 1.19 or newer
- [`go-task`](https://taskfile.dev/)
- `gcloud` CLI installed and authenticated
- `ssh` available in your `PATH`

### Install go-task

macOS:
```bash
brew install go-task
```

Linux:
```bash
sh -c "$(curl -fsSL https://taskfile.dev/install.sh)" -- -d -b ~/.local/bin
```

Windows:
```powershell
choco install go-task
```

Alternatively, download binaries from the [go-task releases](https://github.com/go-task/task/releases).

### Build From Source

```bash
git clone <repository-url>
cd compass
task build
./compass --help
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
# Connect to an instance (auto-discovers zone and resource type)
compass gcp my-instance --project my-gcp-project

# Specify a zone explicitly
compass gcp my-instance --project my-gcp-project --zone us-central1-a

# Explicitly specify resource type for faster discovery
compass gcp my-instance --project my-gcp-project --type instance

# Connect to the first running instance from a MIG
compass gcp my-mig-name --project my-gcp-project --type mig

# Establish a tunnel through IAP
compass gcp my-instance --project my-gcp-project --ssh-flag "-L 8080:localhost:8080"

# Display build metadata
compass version

# Inspect a VPN gateway
compass gcp vpn get prod-ha-vpn --type gateway --region us-central1
```

## Usage

### Basic Command

```bash
compass gcp [instance-name] --project [project-id]
```

### Advanced Options

```bash
# Provide multiple SSH flags
compass gcp instance-name \
  --project my-project \
  --ssh-flag "-L 8080:localhost:8080" \
  --ssh-flag "-D 1080" \
  --ssh-flag "-X"

# Enable verbose logging
compass gcp instance-name --project my-project --log-level debug

# Target a regional MIG
compass gcp my-regional-mig --project my-project --zone us-central1
```

**GCP SSH Flags**

| Flag | Aliases | Description | Default |
|------|---------|-------------|---------|
| `--project` | `-p` | GCP project ID | (required on first use, then cached) |
| `--zone` | `-z` | GCP zone | Auto-discovered if not specified |
| `--type` | `-t` | Resource type: `instance` or `mig` | Auto-detected (tries MIG first, then instance) |
| `--ssh-flag` | | Additional SSH flags (can be used multiple times) | None |

**Global Flags**

| Flag | Description | Default |
|------|-------------|---------|
| `--log-level` | Set logging level (`trace`, `debug`, `info`, `warn`, `error`, `fatal`, `panic`) | `info` |

### SSH Tunneling Recipes

```bash
# Local port forwarding
compass gcp app-server --project prod --ssh-flag "-L 3000:localhost:3000"

# Remote port forwarding
compass gcp jump-host --project staging --ssh-flag "-R 8080:localhost:8080"

# SOCKS proxy
compass gcp proxy-server --project dev --ssh-flag "-D 1080"

# X11 forwarding
compass gcp desktop-instance --project dev --ssh-flag "-X"

# Multiple tunnels at once
compass gcp multi-service \
  --project prod \
  --ssh-flag "-L 3000:service1:3000" \
  --ssh-flag "-L 4000:service2:4000" \
  --ssh-flag "-L 5000:database:5432"
```

## Connectivity Tests

Connectivity tests let you validate reachability between GCP resources using the Google Cloud Connectivity Tests API.

> **Tip:** Use the shorter `ct` alias for connectivity-test commands: `compass gcp ct list` instead of `compass gcp connectivity-test list`

Set `COMPASS_OUTPUT` to change the default output format (supported values: `text`, `table`, `json`, `detailed`). If the variable is unset, list commands default to `table` while detailed views use `text`.

### Create

```console
$ compass gcp connectivity-test create web-to-db \
    --project prod \
    --source-instance web-server-1 \
    --destination-instance db-server-1 \
    --destination-port 5432
time="12:07:15" level=info msg="Creating connectivity test: web-to-db"
Creating connectivity test...
✓ Connectivity test created
time="12:07:21" level=info msg="Connectivity test created successfully"
✓ Connectivity Test: web-to-db
  Console URL:   https://console.cloud.google.com/net-intelligence/connectivity/tests/details/projects/prod/locations/global/tests/web-to-db?project=prod
  Forward Status: REACHABLE
  Return Status:  N/A
  Source:        web-server-1 (10.128.0.5)
  Destination:   db-server-1 (10.138.0.10:5432)
  Protocol:      TCP

  Path Analysis:
    +---+------+-------------+---------------------+---------+
    | # | Step | Type        | Resource            | Status  |
    +---+------+-------------+---------------------+---------+
    | 1 | →    | VM Instance | web-server-1        | OK      |
    +---+------+-------------+---------------------+---------+
    | 2 | →    | VPC         | prod-network        | OK      |
    +---+------+-------------+---------------------+---------+
    | 3 | →    | Firewall    | allow-internal      | ALLOWED |
    +---+------+-------------+---------------------+---------+
    | 4 | ✓    | VM Instance | db-server-1         | OK      |
    +---+------+-------------+---------------------+---------+

  Result: Connection successful ✓
```

You can specify sources and destinations using instance names, IP addresses, or network URIs. Use `--source-type` and `--destination-type` flags (or `-s` and `-d` short forms) to explicitly set resource types (`instance` or `mig`).

MIG-to-instance test example:

```bash
compass gcp connectivity-test create api-to-backend \
  --project prod \
  --source-instance api-mig \
  --source-type mig \
  --destination-instance backend \
  --destination-port 8080
```

IP-based test:

```bash
compass gcp connectivity-test create ip-test \
  --project prod \
  --source-ip 10.128.0.5 \
  --destination-ip 10.138.0.10 \
  --destination-port 443 \
  --protocol TCP
```

**Supported protocols:** TCP (default), UDP, ICMP, ESP, AH, SCTP, GRE

Add labels when you create the test:

```bash
--labels env=prod,service=payments
```

Cross-project tests are supported using `--source-project` and `--destination-project` flags. You can also specify custom VPC networks using `--source-network` and `--destination-network`.

### Watch Progress

```console
$ compass gcp connectivity-test get web-to-db --project prod --watch
time="12:08:04" level=info msg="Watching connectivity test (polling every 5 seconds)..."
Checking connectivity test status...
Waiting for connectivity test completion (elapsed 5s)...
✓ Connectivity test completed
✓ Connectivity Test: web-to-db
  Forward Status: REACHABLE
  Return Status:  N/A
  Source:        web-server-1 (10.128.0.5)
  Destination:   db-server-1 (10.138.0.10:5432)
  Protocol:      TCP
  ...
```

The command finishes by printing the same detailed summary as `get` (shown below).

### Get Results

```console
$ compass gcp connectivity-test get web-to-db --project prod
Fetching connectivity test details...
✓ Connectivity test details received
✓ Connectivity Test: web-to-db
  Console URL:   https://console.cloud.google.com/net-intelligence/connectivity/tests/details/projects/prod/locations/global/tests/web-to-db?project=prod
  Forward Status: REACHABLE
  Return Status:  N/A
  Source:        web-server-1 (10.128.0.5)
  Destination:   db-server-1 (10.138.0.10:5432)
  Protocol:      TCP

  Path Analysis:
    +---+------+-------------+---------------------+---------+
    | # | Step | Type        | Resource            | Status  |
    +---+------+-------------+---------------------+---------+
    | 1 | →    | VM Instance | web-server-1        | OK      |
    +---+------+-------------+---------------------+---------+
    | 2 | →    | VPC         | prod-network        | OK      |
    +---+------+-------------+---------------------+---------+
    | 3 | →    | Firewall    | allow-internal      | ALLOWED |
    +---+------+-------------+---------------------+---------+
    | 4 | ✓    | VM Instance | db-server-1         | OK      |
    +---+------+-------------+---------------------+---------+

  Result: Connection successful ✓
```

Switch to JSON or detailed output with `--output json` or `--output detailed`. Use `--timeout` with `--watch` to specify a custom timeout in seconds (default: 300).

### Rerun a Test

Rerun an existing test with the same configuration to get updated results:

```console
$ compass gcp connectivity-test run web-to-db --project prod
Rerunning connectivity test...
✓ Connectivity test rerun completed
✓ Connectivity Test: web-to-db
  Forward Status: REACHABLE
  Return Status:  N/A
  Source:        web-server-1 (10.128.0.5)
  Destination:   db-server-1 (10.138.0.10:5432)
  Protocol:      TCP
  ...
```

This is useful for periodic validation or after making network changes. Use `--output detailed` or `--output json` for different output formats.

### List and Delete

List tests in text mode:

```console
$ compass gcp connectivity-test list --project prod
Fetching connectivity tests...
✓ Connectivity tests retrieved
Found 2 connectivity test(s):

✓ web-to-db
  Forward Status: REACHABLE
  Return Status:  N/A
  Source: web-server-1 (10.128.0.5)
  Dest:   db-server-1 (10.138.0.10:5432)

✗ web-to-cache
  Forward Status: UNREACHABLE
  Return Status:  N/A
  Source: web-server-1 (10.128.0.5)
  Dest:   cache-proxy (10.138.0.20:6379)
```

Render a compact table instead:

```console
$ compass gcp connectivity-test list --project prod --output table
Fetching connectivity tests...
✓ Connectivity tests retrieved
ST  NAME                          FORWARD STATUS              RETURN STATUS               SOURCE                        DESTINATION
---------------------------------------------------------------------------------------------------------------------------------------------------
✓   web-to-db                     REACHABLE                   N/A                         web-server-1 (10.128.0.5)     db-server-1 (10.138.0.10:5432)
✗   web-to-cache                  UNREACHABLE                 N/A                         web-server-1 (10.128.0.5)     cache-proxy (10.138.0.20:6379)
```

Filter and limit results:

```bash
# Filter by labels
compass gcp connectivity-test list --project prod --filter "labels.env=prod"

# Limit results
compass gcp connectivity-test list --project prod --limit 10
```

Delete a test with confirmation:

```console
$ compass gcp connectivity-test delete web-to-db --project prod
Are you sure you want to delete connectivity test 'web-to-db'? (y/N): y
time="12:12:40" level=info msg="Deleting connectivity test: web-to-db"
Deleting connectivity test...
✓ Connectivity test deleted
time="12:12:43" level=info msg="Connectivity test 'web-to-db' deleted successfully"
```

Use `--force` to skip the prompt.

### Output Formats

Success example:
```
✓ Connectivity Test: web-to-db
  Console URL:   https://console.cloud.google.com/net-intelligence/connectivity/tests/details/projects/prod/locations/global/tests/web-to-db?project=prod
  Forward Status: REACHABLE
  Return Status:  N/A
  Source:        web-server-1 (10.128.0.5)
  Destination:   db-server-1 (10.138.0.10:5432)
  Protocol:      TCP

  Path Analysis:
    +---+------+-------------+---------------------+---------+
    | # | Step | Type        | Resource            | Status  |
    +---+------+-------------+---------------------+---------+
    | 1 | →    | VM Instance | web-server-1        | OK      |
    +---+------+-------------+---------------------+---------+
    | 2 | →    | VPC         | prod-network        | OK      |
    +---+------+-------------+---------------------+---------+
    | 3 | →    | Firewall    | allow-internal      | ALLOWED |
    +---+------+-------------+---------------------+---------+
    | 4 | ✓    | VM Instance | db-server-1         | OK      |
    +---+------+-------------+---------------------+---------+

  Result: Connection successful ✓
```

Failure example:
```
✗ Connectivity Test: web-to-db
  Console URL:   https://console.cloud.google.com/net-intelligence/connectivity/tests/details/projects/prod/locations/global/tests/web-to-db?project=prod
  Forward Status: UNREACHABLE
  Return Status:  N/A
  Source:        web-server-1 (10.128.0.5)
  Destination:   db-server-1 (10.138.0.10:5432)
  Protocol:      TCP

  Path Analysis:
    +---+------+-------------+---------------------+----------+
    | # | Step | Type        | Resource            | Status   |
    +---+------+-------------+---------------------+----------+
    | 1 | →    | VM Instance | web-server-1        | OK       |
    +---+------+-------------+---------------------+----------+
    | 2 | →    | VPC         | prod-network        | OK       |
    +---+------+-------------+---------------------+----------+
    | 3 | ✗    | Firewall    | deny-egress         | BLOCKED  |
    +---+------+-------------+---------------------+----------+

  Result: Connection failed ✗

  Suggested Fix:
  Add firewall rule allowing TCP traffic from 10.128.0.5 to 10.138.0.10:5432
```

### Common Use Cases

**Troubleshoot connectivity**

```console
$ compass gcp connectivity-test create quick-check \
    --project prod \
    --source-instance app-1 \
    --destination-instance db-primary \
    --destination-port 5432
time="12:20:11" level=info msg="Creating connectivity test: quick-check"
...

$ compass gcp connectivity-test get quick-check --project prod --watch
time="12:20:20" level=info msg="Watching connectivity test (polling every 5 seconds)..."
...
```

**Pre-deployment validation**

```bash
for service in auth billing; do
  compass gcp connectivity-test create "${service}-egress" \
    --project staging \
    --source-instance "new-${service}" \
    --destination-ip 10.0.1.100 \
    --destination-port 443 \
    --labels env=staging
done
```

**Network policy verification**

```bash
for port in 80 443 8080; do
  compass gcp connectivity-test create "web-port-${port}" \
    --project prod \
    --source-instance web-frontend \
    --destination-instance backend \
    --destination-port $port
done

compass gcp connectivity-test list --project prod --output table
```

**CI/CD automation**

```bash
compass gcp connectivity-test create ci-check \
  --project staging \
  --source-instance app \
  --destination-instance db \
  --destination-port 5432 \
  --output json | jq -e '.reachabilityDetails.result == "REACHABLE"'
```

## VPN Overview

Inspect Cloud VPN gateways, tunnels, and Cloud Router BGP sessions across your project. The output includes BGP route information (advertised and learned routes) and configuration warnings.

```console
$ compass gcp vpn list --project prod
🔐 Gateway: prod-ha-vpn (us-central1)
  Network:     prod-vpc
  Interfaces:
    - #0 IP: 10.10.0.2
    - #1 IP: 10.10.1.2
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

⚠️  Orphan Tunnels (not attached to HA VPN gateways):
  • legacy-hub (us-east1) peer 198.51.100.10
    Router: legacy-router

⚠️  Configuration Warnings:
  • Gateway 'staging-vpn' has no attached tunnels
  • BGP peer 'backup-peer' has routing issues
```

Use `--warnings=false` to hide the warnings section if you only want to see the active configuration.

Switch to a concise table summary:

```console
$ compass gcp vpn list --project prod --output table
┌────────────┬──────────────┬──────────────┬─────────────┬──────────┐
│ GATEWAY    │ REGION       │ NETWORK      │ #INTERFACES │ #TUNNELS │
├────────────┼──────────────┼──────────────┼─────────────┼──────────┤
│ prod-ha-vpn│ us-central1  │ prod-vpc     │           2 │        2 │
└────────────┴──────────────┴──────────────┴─────────────┴──────────┘

┌────────────┬──────────────┬────────────┬─────────────┬─────────┬────────────┐
│ GATEWAY    │ TUNNEL       │ REGION     │ PEER IP     │ ROUTER  │ BGP PEERS  │
├────────────┼──────────────┼────────────┼─────────────┼─────────┼────────────┤
│ prod-ha-vpn│ prod-to-eu   │ us-central1│ 203.0.113.10│ prod-router │ prod-peer-eu │
└────────────┴──────────────┴────────────┴─────────────┴─────────┴────────────┘
```

Use `--output json` to consume the inventory programmatically.

### Inspect a Single Gateway or Tunnel

```console
$ compass gcp vpn get prod-ha-vpn --type gateway --region us-central1

$ compass gcp vpn get prod-to-eu --type tunnel --region us-central1 --output json
```

## Development

Use the `Taskfile.yml` to build, lint, and test consistently.

- `task build` – compile the `compass` binary with version metadata
- `task run -- gcp <instance> --project <id>` – run the CLI without generating a binary
- `task fmt` / `task lint` / `task vet` – formatting, linting, and static analysis
- `task test` / `task test-short` / `task test-integration` – full, unit, and integration test suites
- `task test-coverage` – generate coverage reports
- `task dev` – hot-reload workflow (requires [`air`](https://github.com/cosmtrek/air))

Remove compiled binaries like `./compass` before committing changes.

## CI/CD

GitHub Actions run `task check` (fmt, vet, lint, test) and `task test-race` on pull requests and pushes to `main`. Tagging a commit triggers an automated release that repeats `task check`, verifies formatting stays clean, rebuilds via `task build`, and publishes notes.

## Roadmap

- [ ] AWS support for EC2 instances and Auto Scaling Groups
- [ ] Connectivity test templates and presets
- [ ] Bulk connectivity test operations
- [ ] Export connectivity results to additional formats

## License

Licensed under the [Apache License 2.0](LICENSE).

## Support

For issues or feature requests, open an issue in this repository.
