# compass - Cloud Instance SSH Connector

`compass` is a fast, intuitive CLI for reaching Google Cloud Platform (GCP) instances over SSH. It handles Identity-Aware Proxy (IAP) tunneling, Managed Instance Groups (MIGs), connectivity tests, and advanced SSH scenarios without extra configuration.

> Prefer something shorter? Every command is also available through the `cps` alias.

## Table of Contents
- [Overview](#overview)
- [Features](#features)
- [Installation](#installation)
  - [Prerequisites](#prerequisites)
  - [Install go-task](#install-go-task)
  - [Build From Source](#build-from-source)
- [Quick Start](#quick-start)
- [Usage](#usage)
  - [Basic Command](#basic-command)
  - [Advanced Options](#advanced-options)
  - [SSH Tunneling Recipes](#ssh-tunneling-recipes)
- [Connectivity Tests](#connectivity-tests)
  - [Create](#create)
  - [Watch Progress](#watch-progress)
  - [Get Results](#get-results)
  - [List and Delete](#list-and-delete)
  - [Output Formats](#output-formats)
  - [Common Use Cases](#common-use-cases)
- [Development](#development)
- [Roadmap](#roadmap)
- [License](#license)
- [Support](#support)

## Overview

`compass` discovers instances, selects the best SSH path, and launches connections quickly. It reuses your existing `gcloud` credentials, surfaces meaningful logs, and stays flexible enough for tunneling, port forwarding, or automation.

## Features

- 🚀 Quick SSH connections with automatic instance and zone discovery
- 🔒 Identity-Aware Proxy (IAP) tunneling support
- 🎯 Managed Instance Group (MIG) support for regional and zonal groups
- 🌐 Zone and region auto-discovery when omitted
- 🔧 Pass arbitrary SSH flags for tunneling, forwarding, or X11
- 🔍 Network connectivity tests powered by Google Cloud Connectivity Tests API
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

## Quick Start

```bash
# Connect to an instance (auto-discovers zone)
compass gcp my-instance --project my-gcp-project

# Specify a zone explicitly
compass gcp my-instance --project my-gcp-project --zone us-central1-a

# Connect to the first running instance from a MIG
compass gcp my-mig-name --project my-gcp-project

# Establish a tunnel through IAP
compass gcp my-instance --project my-gcp-project --ssh-flag "-L 8080:localhost:8080"

# Display build metadata
compass version
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

Add labels when you create the test:

```bash
--labels env=prod,service=payments
```

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

Switch to JSON or detailed output with `--output json` or `--output detailed`.

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

## Development

Use the `Taskfile.yml` to build, lint, and test consistently.

- `task build` – compile the `compass` binary with version metadata
- `task run -- gcp <instance> --project <id>` – run the CLI without generating a binary
- `task fmt` / `task lint` / `task vet` – formatting, linting, and static analysis
- `task test` / `task test-short` / `task test-integration` – full, unit, and integration test suites
- `task test-coverage` – generate coverage reports
- `task dev` – hot-reload workflow (requires [`air`](https://github.com/cosmtrek/air))

Remove compiled binaries like `./compass` before committing changes.

## Roadmap

- [ ] AWS support for EC2 instances and Auto Scaling Groups
- [ ] Connectivity test templates and presets
- [ ] Bulk connectivity test operations
- [ ] Export connectivity results to additional formats

## License

Private for now—OSS soon?

## Support

For issues or feature requests, open an issue in this repository.
