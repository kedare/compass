package output

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"golang.org/x/term"
)

// Spinner wraps a CLI spinner that gracefully degrades when stdout isn't a TTY.
type Spinner struct {
	mu      sync.Mutex
	active  bool
	enabled bool
	sp      *spinner.Spinner
	message string
	writer  *os.File
	stopped bool
}

// NewSpinner creates a new spinner with the provided message. Call Start before using.
func NewSpinner(message string) *Spinner {
	writer := os.Stderr
	enabled := term.IsTerminal(int(writer.Fd()))

	s := &Spinner{
		enabled: enabled,
		message: message,
		writer:  writer,
	}

	if enabled {
		sp := spinner.New(spinner.CharSets[14], 100*time.Millisecond, spinner.WithWriter(writer))
		sp.Suffix = " " + message
		sp.HideCursor = true
		sp.Color("cyan")
		s.sp = sp
	}

	return s
}

// Start begins rendering the spinner.
func (s *Spinner) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped || s.active {
		return
	}

	if s.enabled && s.sp != nil {
		s.sp.Start()
	} else {
		fmt.Fprintf(s.writer, "%s...\n", s.message)
	}
	s.active = true
}

// Update updates the spinner message.
func (s *Spinner) Update(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.message = message
	if !s.active || s.stopped {
		return
	}

	if s.enabled && s.sp != nil {
		s.sp.Suffix = " " + message
	} else {
		fmt.Fprintf(s.writer, "%s...\n", message)
	}
}

// Stop stops the spinner without printing an additional message.
func (s *Spinner) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return
	}
	s.stopped = true

	if s.enabled && s.sp != nil {
		s.sp.Stop()
		fmt.Fprint(s.writer, "\r")
	}
}

// Success stops the spinner and prints a success message.
func (s *Spinner) Success(message string) {
	s.stopWithMessage("✓", message)
}

// Fail stops the spinner and prints a failure message.
func (s *Spinner) Fail(message string) {
	s.stopWithMessage("✗", message)
}

func (s *Spinner) stopWithMessage(prefix, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		if message != "" {
			fmt.Fprintf(s.writer, "%s %s\n", prefix, message)
		}
		return
	}
	s.stopped = true

	if s.enabled && s.sp != nil {
		s.sp.Stop()
		fmt.Fprintf(s.writer, "\r%s %s\n", prefix, message)
		return
	}

	if message != "" {
		fmt.Fprintf(s.writer, "%s %s\n", prefix, message)
	}
}
