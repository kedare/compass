package output

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kedare/compass/internal/gcp"
	"github.com/mattn/go-runewidth"
	"github.com/stretchr/testify/require"
)

var stdoutCaptureMu sync.Mutex

// captureStdout executes fn while capturing everything written to stdout.
func captureStdout(t *testing.T, fn func()) string {
	stdoutCaptureMu.Lock()
	defer stdoutCaptureMu.Unlock()

	reader, writer, err := os.Pipe()
	require.NoError(t, err)

	origStdout := os.Stdout
	os.Stdout = writer

	outCh := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		outCh <- buf.String()
	}()

	fn()

	err = writer.Close()
	require.NoError(t, err)
	os.Stdout = origStdout
	output := <-outCh

	return output
}

// lineWithForwardAndReturn reports whether a rendered table shows both path headers on the same line.
func lineWithForwardAndReturn(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Forward Path") && strings.Contains(line, "Return Path") {
			return true
		}
	}

	return false
}

// makeTrace creates a minimal trace with a single descriptive step.
func makeTrace(label string) *gcp.Trace {
	return &gcp.Trace{
		Steps: []*gcp.TraceStep{
			{
				Description: label,
				State:       "REACHABLE",
			},
		},
	}
}

func TestDisplayForwardAndReturnPaths_RespectsTerminalWidth(t *testing.T) {
	forward := makeTrace("forward")
	backward := makeTrace("return")

	combined := renderCombinedTrace(forward, backward, 0, 1)
	required := maximumLineWidth(combined)

	narrow := required - 1
	if narrow < 1 {
		narrow = 1
	}

	t.Setenv("COLUMNS", strconv.Itoa(narrow))
	sequential := captureStdout(t, func() {
		displayForwardAndReturnPaths([]*gcp.Trace{forward}, []*gcp.Trace{backward}, true)
	})

	require.False(t, lineWithForwardAndReturn(sequential), "expected sequential layout on narrow terminal, got:\n%s", sequential)

	wide := required + 10
	t.Setenv("COLUMNS", strconv.Itoa(wide))

	width, ok := detectTerminalWidth()
	require.True(t, ok)
	require.Equal(t, wide, width)

	sideBySide := captureStdout(t, func() {
		displayForwardAndReturnPaths([]*gcp.Trace{forward}, []*gcp.Trace{backward}, true)
	})

	require.True(t, lineWithForwardAndReturn(sideBySide), "expected combined layout on wide terminal, got:\n%s", sideBySide)
}

func TestRenderCombinedTraceAlignment(t *testing.T) {
	forward := makeTrace("forward")
	forward.Steps = append(forward.Steps, []*gcp.TraceStep{
		{Description: "Forwarding state: arriving at a VPC VPN tunnel.", State: "ARRIVE_AT_VPN_TUNNEL"},
		{Description: "Forwarding state: arriving at a VPC VPN gateway.", State: "ARRIVE_AT_VPN_GATEWAY"},
		{Description: "Config checking state: analyze load balancer backend.", State: "ANALYZE_LOAD_BALANCER_BACKEND"},
		{Description: "Config checking state: match forwarding rule.", State: "APPLY_FORWARDING_RULE"},
	}...)

	backward := makeTrace("return")
	backward.Steps = append(backward.Steps, []*gcp.TraceStep{
		{Description: "Forwarding state: arriving at a VPC VPN tunnel.", State: "ARRIVE_AT_VPN_TUNNEL"},
		{Description: "Config checking state: verify INGRESS firewall rule.", State: "APPLY_INGRESS_FIREWALL_RULE"},
		{Description: "Final state: packet delivered to instance.", State: "DELIVER"},
	}...)

	combined := renderCombinedTrace(forward, backward, 0, 1)
	t.Setenv("COLUMNS", strconv.Itoa(maximumLineWidth(combined)+10))

	out := captureStdout(t, func() {
		fmt.Print(combined)
	})

	lines := strings.Split(out, "\n")
	column := -1

	for _, line := range lines {
		idx := firstTableColumn(line)
		if idx == -1 {
			continue
		}

		if column == -1 {
			column = idx

			continue
		}

		require.Equal(t, column, idx, "expected consistent column start %d, got %d on line %q", column, idx, line)
	}

	require.NotEqual(t, -1, column, "failed to detect right table column")
}

func firstTableColumn(line string) int {
	for i, r := range line {
		switch r {
		// pterm table borders
		case '+', '|':
			return runewidth.StringWidth(stripAnsiCodes(line[:i]))
		// go-pretty table borders (for backwards compatibility)
		case '┌', '└', '┴', '┬', '┼', '│', '├', '┤':
			return runewidth.StringWidth(stripAnsiCodes(line[:i]))
		}
	}

	return -1
}

func maximumLineWidth(s string) int {
	max := 0
	for _, line := range strings.Split(s, "\n") {
		if w := runewidth.StringWidth(stripAnsiCodes(line)); w > max {
			max = w
		}
	}

	return max
}

func TestEvaluateConnectivityStatus(t *testing.T) {
	t.Parallel()

	t.Run("nil result", func(t *testing.T) {
		status := evaluateConnectivityStatus(nil)
		require.False(t, status.overall)
		require.Equal(t, "UNKNOWN", status.forwardStatus)
		require.False(t, status.forwardReachable)
		require.False(t, status.hasReturn)
	})

	t.Run("forward reachable only", func(t *testing.T) {
		result := &gcp.ConnectivityTestResult{
			ReachabilityDetails: &gcp.ReachabilityDetails{Result: "REACHABLE"},
		}

		status := evaluateConnectivityStatus(result)
		require.True(t, status.overall)
		require.Equal(t, "REACHABLE", status.forwardStatus)
		require.True(t, status.forwardReachable)
		require.False(t, status.hasReturn)
	})

	t.Run("return unreachable blocks overall", func(t *testing.T) {
		result := &gcp.ConnectivityTestResult{
			ReachabilityDetails:       &gcp.ReachabilityDetails{Result: "REACHABLE"},
			ReturnReachabilityDetails: &gcp.ReachabilityDetails{Result: "reachability_result_unreachable"},
		}

		status := evaluateConnectivityStatus(result)
		require.False(t, status.overall)
		require.True(t, status.forwardReachable)
		require.True(t, status.hasReturn)
		require.Equal(t, "UNREACHABLE", status.returnStatus)
		require.False(t, status.returnReachable)
	})

	t.Run("forward drop yields not reachable", func(t *testing.T) {
		result := &gcp.ConnectivityTestResult{
			ReachabilityDetails: &gcp.ReachabilityDetails{Result: "forward_drop"},
		}

		status := evaluateConnectivityStatus(result)
		require.False(t, status.overall)
		require.Equal(t, "FORWARD_DROP", status.forwardStatus)
		require.False(t, status.forwardReachable)
	})

	t.Run("return info only", func(t *testing.T) {
		result := &gcp.ConnectivityTestResult{
			ReturnReachabilityDetails: &gcp.ReachabilityDetails{Result: "reachable"},
		}

		status := evaluateConnectivityStatus(result)
		require.False(t, status.overall)
		require.Equal(t, "UNKNOWN", status.forwardStatus)
		require.False(t, status.forwardReachable)
		require.True(t, status.hasReturn)
		require.Equal(t, "REACHABLE", status.returnStatus)
		require.True(t, status.returnReachable)
	})
}

func TestNormalizeStatusAndReachability(t *testing.T) {
	t.Parallel()

	t.Run("normalize various labels", func(t *testing.T) {
		cases := map[string]string{
			"":                                "UNKNOWN",
			" reachable ":                     "REACHABLE",
			"reachability_result_unreachable": "UNREACHABLE",
			"FORWARD_DROP":                    "FORWARD_DROP",
		}

		for input, expected := range cases {
			require.Equal(t, expected, normalizeStatusLabel(input))
		}
	})

	t.Run("reachability detection", func(t *testing.T) {
		require.True(t, isStatusReachable("reachable"))
		require.True(t, isStatusReachable("REACHABILITY_RESULT_REACHABLE"))
		require.False(t, isStatusReachable("unreachable"))
		require.False(t, isStatusReachable("forward_drop"))
		require.False(t, isStatusReachable(""))
	})
}

func TestFormatEndpoint(t *testing.T) {
	t.Parallel()

	require.Equal(t, "N/A", formatEndpoint(nil, true))

	noDetails := &gcp.EndpointInfo{}
	require.Equal(t, "N/A", formatEndpoint(noDetails, false))

	withInstanceAndIP := &gcp.EndpointInfo{
		Instance:  "projects/p/zones/z/instances/test-instance",
		IPAddress: "10.0.0.1",
		Port:      443,
	}

	require.Equal(t, "test-instance (10.0.0.1)", formatEndpoint(withInstanceAndIP, false))
	require.Equal(t, "test-instance (10.0.0.1:443)", formatEndpoint(withInstanceAndIP, true))

	ipOnly := &gcp.EndpointInfo{IPAddress: "192.168.1.10"}
	require.Equal(t, "192.168.1.10", formatEndpoint(ipOnly, false))

	portOnly := &gcp.EndpointInfo{Port: 8080}
	require.Equal(t, "port 8080", formatEndpoint(portOnly, true))

	instanceOnly := &gcp.EndpointInfo{
		Instance: "projects/p/zones/z/instances/solo",
	}
	require.Equal(t, "solo", formatEndpoint(instanceOnly, true))

	regionalResource := &gcp.EndpointInfo{
		Instance: "projects/p/regions/us-central1/endpoints/endpoint-a",
		Port:     0,
	}
	require.Equal(t, "endpoint-a", formatEndpoint(regionalResource, false))
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	require.Equal(t, "short", truncate("short", 10))
	require.Equal(t, "exa...", truncate("example", 6))
	require.Equal(t, "a", truncate("alpha", 1))
	require.Equal(t, "こ...", truncate("こんにちは", 6))
}

// newResult constructs a minimal connectivity test result.
func newResult(name string, forward string, returnStatus string) *gcp.ConnectivityTestResult {
	result := &gcp.ConnectivityTestResult{
		DisplayName: name,
		Name:        "projects/demo/tests/" + name,
		Source: &gcp.EndpointInfo{
			Instance:  "projects/demo/zones/us-central1-a/instances/src",
			IPAddress: "10.0.0.1",
		},
		Destination: &gcp.EndpointInfo{
			Instance:  "projects/demo/zones/us-central1-b/instances/dst",
			IPAddress: "10.0.0.2",
			Port:      443,
		},
		Protocol: "TCP",
	}

	if forward != "" {
		result.ReachabilityDetails = &gcp.ReachabilityDetails{Result: forward}
	}

	if returnStatus != "" {
		result.ReturnReachabilityDetails = &gcp.ReachabilityDetails{Result: returnStatus}
	}

	return result
}

func TestDisplayListText(t *testing.T) {
	t.Parallel()

	results := []*gcp.ConnectivityTestResult{
		newResult("Reachable", "REACHABLE", ""),
		newResult("ReturnFail", "REACHABLE", "UNREACHABLE"),
	}

	out := captureStdout(t, func() {
		require.NoError(t, displayListText(results))
	})

	require.Contains(t, out, "Found 2 connectivity test(s)")
	require.Contains(t, out, "✓ Reachable")
	require.Contains(t, out, "ReturnFail")
	require.Contains(t, out, "Return Status:")
}

func TestDisplayListTextEmpty(t *testing.T) {
	t.Parallel()

	out := captureStdout(t, func() {
		require.NoError(t, displayListText(nil))
	})

	require.Contains(t, out, "No connectivity tests found")
}

func TestDisplayTable(t *testing.T) {
	t.Parallel()

	results := []*gcp.ConnectivityTestResult{
		newResult("My Very Long Connectivity Test Name", "REACHABLE", ""),
	}

	out := captureStdout(t, func() {
		require.NoError(t, displayTable(results))
	})

	require.Contains(t, out, "NAME")
	require.Contains(t, out, "FORWARD STATUS")
	require.Contains(t, out, "SOURCE")
	require.Contains(t, out, "My Very Long Connectivity")
}

func TestDisplayDetailed(t *testing.T) {
	t.Parallel()

	result := newResult("Detailed", "REACHABLE", "")
	result.Description = "Sample connectivity test"
	result.CreateTime = time.Unix(1000, 0).UTC()
	result.UpdateTime = time.Unix(2000, 0).UTC()
	result.ReachabilityDetails.VerifyTime = time.Unix(3000, 0).UTC()

	out := captureStdout(t, func() {
		require.NoError(t, displayDetailed(result))
	})

	require.Contains(t, out, "Additional Details")
	require.Contains(t, out, "Sample connectivity test")
	require.Contains(t, out, "Created:")
	require.Contains(t, out, "Verified:")
}

func TestDisplayConnectivityTestResult(t *testing.T) {
	t.Parallel()

	result := newResult("FinalStatus", "REACHABLE", "")

	out := captureStdout(t, func() {
		require.NoError(t, DisplayConnectivityTestResult(result, "text"))
	})

	require.Contains(t, out, "Connectivity Test")
	require.Contains(t, out, "Result: Connection successful")
}

func TestDisplayConnectivityTestResultWithSuggestedFix(t *testing.T) {
	t.Parallel()

	result := newResult("NeedsFix", "FORWARD_DROP", "")
	result.ReachabilityDetails.Traces = []*gcp.Trace{
		{
			Steps: []*gcp.TraceStep{
				{
					Firewall:   "fw-rule",
					CausesDrop: true,
				},
			},
		},
	}

	out := captureStdout(t, func() {
		require.NoError(t, DisplayConnectivityTestResult(result, "text"))
	})

	require.Contains(t, out, "Suggested Fix")
	require.Contains(t, out, "Add firewall rule")
}

func TestDisplayConnectivityTestResultJSON(t *testing.T) {
	t.Parallel()

	result := newResult("JSONResult", "REACHABLE", "")

	out := captureStdout(t, func() {
		require.NoError(t, DisplayConnectivityTestResult(result, "json"))
	})

	require.Contains(t, out, "\"DisplayName\": \"JSONResult\"")
}

func TestDisplayConnectivityTestListJSON(t *testing.T) {
	t.Parallel()

	results := []*gcp.ConnectivityTestResult{
		newResult("JsonList", "REACHABLE", ""),
	}

	out := captureStdout(t, func() {
		require.NoError(t, DisplayConnectivityTestList(results, "json"))
	})

	require.Contains(t, out, "\"DisplayName\": \"JsonList\"")
}

func TestDisplaySuggestedFixesForRoute(t *testing.T) {
	t.Parallel()

	result := newResult("RouteFix", "FORWARD_DROP", "")
	result.ReachabilityDetails.Traces = []*gcp.Trace{
		{
			Steps: []*gcp.TraceStep{
				{
					Route:      "projects/demo/global/routes/default",
					CausesDrop: true,
				},
			},
		},
	}

	out := captureStdout(t, func() {
		require.NoError(t, DisplayConnectivityTestResult(result, "text"))
	})

	require.Contains(t, out, "Suggested Fix")
	require.Contains(t, out, "routing configuration")
}

func TestPadHelpers(t *testing.T) {
	t.Parallel()

	require.Equal(t, "name      ", padRight("name", 10))
	require.Equal(t, "colored   ", padRightWithPlain("colored", "colored", 10))
	require.Equal(t, "plain", stripAnsiCodes("\x1b[31mplain\x1b[0m"))
}

func TestFitsTerminalWidth(t *testing.T) {
	t.Parallel()

	require.True(t, fitsTerminalWidthWithLimit("ok", 10, true))
	require.False(t, fitsTerminalWidthWithLimit("this line is definitely longer than ten columns", 10, true))
	require.True(t, fitsTerminalWidthWithLimit("any width works when limit is unknown", 0, false))
}

func TestGetStepIcon(t *testing.T) {
	t.Parallel()

	require.Equal(t, "✗", getStepIcon(0, 1, true))
	require.Equal(t, "→", getStepIcon(0, 3, false))
	require.Equal(t, "✓", getStepIcon(2, 3, false))
}

func TestFormatTraceStepForTable(t *testing.T) {
	t.Parallel()

	step := &gcp.TraceStep{Firewall: "fw", CausesDrop: true}
	typ, resource, status := formatTraceStepForTable(step)
	require.Equal(t, "Firewall", typ)
	require.Equal(t, "fw", resource)
	require.Equal(t, "BLOCKED", status)

	step = &gcp.TraceStep{Instance: "projects/p/zones/z/instances/demo"}
	typ, resource, status = formatTraceStepForTable(step)
	require.Equal(t, "VM Instance", typ)
	require.Equal(t, "demo", resource)
	require.Equal(t, "OK", status)
}
