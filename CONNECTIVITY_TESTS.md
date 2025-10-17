# Connectivity Tests Implementation

This document provides an overview of the GCP Network Connectivity Tests implementation in the `compass` CLI tool.

## Architecture

### Components

1. **CLI Commands** (`cmd/connectivity*.go`)
   - `connectivity.go` - Main command and subcommand registration
   - `connectivity_create.go` - Create new connectivity tests
   - `connectivity_run.go` - Run existing tests
   - `connectivity_get.go` - Retrieve test results with watch mode
   - `connectivity_list.go` - List all tests with filtering
   - `connectivity_delete.go` - Delete tests

2. **GCP Client** (`internal/gcp/connectivity.go`)
   - `ConnectivityClient` - Wrapper around Google Cloud Network Management API
   - Test creation and management
   - Long-running operation handling
   - Type conversions between API and internal types

3. **Output Formatting** (`internal/output/connectivity.go`)
   - Text formatting with path visualization
   - JSON output for automation
   - Table format for listing
   - Detailed output with timestamps
   - Failure diagnosis and suggested fixes

## Features

### Automatic Instance Discovery
The create command integrates with the existing GCP client to automatically resolve instance names to IP addresses:
- Supports standalone instances
- Supports MIG instances (both zonal and regional)
- Zone auto-discovery when not specified
- Leverages existing caching mechanism

### Test Operations

**Create**: `compass gcp connectivity-test create <name>`
- Source: Instance name or IP
- Destination: Instance name or IP
- Automatic IP resolution for instances
- Supports multiple protocols (TCP, UDP, ICMP, ESP, AH, SCTP, GRE)
- Labels and descriptions for organization

**Run**: `compass gcp connectivity-test run <name>`
- Reruns existing test
- Waits for completion
- Returns updated results

**Get**: `compass gcp connectivity-test get <name>`
- Retrieves test results
- Watch mode polls until completion
- Multiple output formats (text, JSON, detailed)

**List**: `compass gcp connectivity-test list`
- Lists all tests in project
- Filter support
- Multiple output formats (text, table, JSON)

**Delete**: `compass gcp connectivity-test delete <name>`
- Confirmation prompt (unless --force)
- Clean test removal

### Output Formats

#### Text (Default)
```
✓ Connectivity Test: web-to-db
  Status:        REACHABLE
  Source:        web-server-1 (10.128.0.5)
  Destination:   db-server-1 (10.138.0.10:5432)
  Protocol:      TCP

  Path Analysis:
  └─ VM Instance (web-server-1) → VPC Network → Firewall (allow) → VM Instance (db-server-1)

  Result: Connection successful ✓
```

#### JSON
Full test details in JSON format for programmatic access.

#### Detailed
Text format plus creation/update timestamps and verification time.

#### Table (List only)
Compact tabular view of multiple tests.

### Path Visualization

The tool visualizes the network path showing:
- Source and destination endpoints
- Network hops (VPC, firewall, routes, load balancers)
- Drop points with failure indication (✗)
- Suggested fixes for common issues

### Error Handling

- Proper validation of inputs
- Clear error messages for missing parameters
- API error handling with retries
- Timeout handling for long-running operations

## API Integration

### Google Cloud Network Management API

The implementation uses the `google.golang.org/api/networkmanagement/v1` package:

- **ConnectivityTest**: Represents a connectivity test configuration
- **Endpoint**: Source/destination endpoint specification
- **ReachabilityDetails**: Test results and traces
- **Trace**: Network path simulation
- **Step**: Individual hop in the path

### Long-Running Operations

Test creation and execution are asynchronous operations:
1. API returns an operation ID
2. Client polls operation status every 5 seconds
3. Maximum wait time: 5 minutes
4. Returns test results when complete

## Usage Examples

### Troubleshooting

```bash
# Quick connectivity check
compass gcp connectivity-test create quick-check \
  --project prod \
  --source-instance app-server \
  --destination-instance db-primary \
  --destination-port 5432 \
  --watch
```

### Pre-deployment Validation

```bash
# Verify new service connectivity
compass gcp connectivity-test create pre-deploy-check \
  --project staging \
  --source-instance new-service-v2 \
  --destination-ip 10.0.1.100 \
  --destination-port 443 \
  --labels "deployment=v2,stage=pre-prod"
```

### Firewall Rule Testing

```bash
# Test multiple ports
for port in 80 443 8080; do
  compass gcp connectivity-test create "web-port-${port}" \
    --project prod \
    --source-instance web-frontend \
    --destination-instance backend \
    --destination-port $port
done

compass gcp connectivity-test list --project prod --output table
```

### CI/CD Integration

```bash
# Automated validation in pipeline
result=$(compass gcp connectivity-test create ci-validation \
  --project staging \
  --source-instance app \
  --destination-instance db \
  --destination-port 5432 \
  --output json)

# Check if reachable
echo "$result" | jq -e '.reachabilityDetails.result == "REACHABLE"' || exit 1
```

## Testing

The build has been verified with:
- Go build compilation
- Help command output
- Subcommand structure validation

Future testing should include:
- Unit tests for connectivity client
- Integration tests with mock GCP API
- CLI command tests

## Dependencies

New dependencies added:
- `google.golang.org/api/networkmanagement/v1` - Network Management API client

All dependencies are managed through `go.mod` and compatible with existing packages.

## Future Enhancements

Potential improvements:
- Test templates and presets
- Bulk test operations
- Test result history
- Export to various formats (CSV, PDF)
- Scheduled test execution
- Alert integration
- Cross-cloud provider testing
