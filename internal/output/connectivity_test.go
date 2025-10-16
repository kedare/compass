package output

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"cx/internal/gcp"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
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

	writer.Close()
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

	t.Setenv("COLUMNS", "175")
	sequential := captureStdout(t, func() {
		displayForwardAndReturnPaths([]*gcp.Trace{forward}, []*gcp.Trace{backward}, true)
	})

	if lineWithForwardAndReturn(sequential) {
		t.Fatalf("expected sequential layout on narrow terminal, got:\n%s", sequential)
	}

	t.Setenv("COLUMNS", "260")
	if width, ok := detectTerminalWidth(); !ok || width != 260 {
		t.Fatalf("expected terminal width 260, got %d (ok=%v)", width, ok)
	}

	forwardLines := renderTraceLines(forward)
	returnLines := renderTraceLines(backward)
	required := leftPaddingWidth + maxVisibleWidth(forwardLines) + separatorWidth + maxVisibleWidth(returnLines) + sideBySideSafetyMargin*2
	t.Logf("required width %d", required)
	sideBySide := captureStdout(t, func() {
		displayForwardAndReturnPaths([]*gcp.Trace{forward}, []*gcp.Trace{backward}, true)
	})

	if !lineWithForwardAndReturn(sideBySide) {
		t.Fatalf("expected side-by-side layout on wide terminal, got:\n%s", sideBySide)
	}
}

func renderTraceLines(trace *gcp.Trace) []string {
	tw := table.NewWriter()
	tw.SetStyle(table.StyleLight)
	tw.Style().Options.SeparateRows = true
	tw.AppendHeader(table.Row{"#", "Step", "Type", "Resource", "Status"})

	for i, step := range trace.Steps {
		stepNum := fmt.Sprintf("%d", i+1)
		stepType, resource, status := formatTraceStepForTable(step)

		if step.CausesDrop {
			stepNum = text.Bold.Sprint(text.FgRed.Sprint(stepNum))
			stepType = text.Bold.Sprint(text.FgRed.Sprint(stepType))
			resource = text.Bold.Sprint(text.FgRed.Sprint(resource))
			status = text.Bold.Sprint(text.FgRed.Sprint(status))
		} else {
			status = text.FgGreen.Sprint(status)
		}

		tw.AppendRow(table.Row{stepNum, getStepIcon(i, len(trace.Steps), step.CausesDrop), stepType, resource, status})
	}

	return strings.Split(tw.Render(), "\n")
}
