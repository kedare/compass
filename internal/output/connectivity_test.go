package output

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/kedare/compass/internal/gcp"
	"github.com/mattn/go-runewidth"
)

func captureStdout(t *testing.T, fn func()) string {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	origStdout := os.Stdout
	os.Stdout = writer

	outCh := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		outCh <- buf.String()
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close pipe writer: %v", err)
	}
	os.Stdout = origStdout
	output := <-outCh

	return output
}

func lineWithForwardAndReturn(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Forward Path") && strings.Contains(line, "Return Path") {
			return true
		}
	}

	return false
}

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

	if lineWithForwardAndReturn(sequential) {
		t.Fatalf("expected sequential layout on narrow terminal, got:\n%s", sequential)
	}

	wide := required + 10
	t.Setenv("COLUMNS", strconv.Itoa(wide))

	if width, ok := detectTerminalWidth(); !ok || width != wide {
		t.Fatalf("expected terminal width %d, got %d (ok=%v)", wide, width, ok)
	}

	sideBySide := captureStdout(t, func() {
		displayForwardAndReturnPaths([]*gcp.Trace{forward}, []*gcp.Trace{backward}, true)
	})

	if !lineWithForwardAndReturn(sideBySide) {
		t.Fatalf("expected combined layout on wide terminal, got:\n%s", sideBySide)
	}
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

		if column != idx {
			t.Fatalf("expected consistent column start %d, got %d on line %q", column, idx, line)
		}
	}

	if column == -1 {
		t.Fatal("failed to detect right table column")
	}
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
