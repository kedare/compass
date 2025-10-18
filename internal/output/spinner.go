package output

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/kedare/compass/internal/logger"
	"golang.org/x/term"
)

// Spinner wraps a CLI spinner that gracefully degrades when stdout isn't a TTY.
type Spinner struct {
	sp      *spinner.Spinner
	writer  *os.File
	message string
	mu      sync.Mutex
	active  bool
	enabled bool
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
		if err := sp.Color("cyan"); err != nil {
			// Fall back to no color if setting color fails
			s.enabled = false
		}
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
		if _, err := fmt.Fprintf(s.writer, "%s...\n", s.message); err != nil {
			logger.Log.Debugf("Failed to write spinner message: %v", err)
		}
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
		if _, err := fmt.Fprintf(s.writer, "%s...\n", message); err != nil {
			logger.Log.Debugf("Failed to write spinner update: %v", err)
		}
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
		if _, err := fmt.Fprint(s.writer, "\r"); err != nil {
			logger.Log.Debugf("Failed to write carriage return: %v", err)
		}
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
			if _, err := fmt.Fprintf(s.writer, "%s %s\n", prefix, message); err != nil {
				logger.Log.Debugf("Failed to write stopped spinner message: %v", err)
			}
		}

		return
	}
	s.stopped = true

	if s.enabled && s.sp != nil {
		s.sp.Stop()
		if _, err := fmt.Fprintf(s.writer, "\r%s %s\n", prefix, message); err != nil {
			logger.Log.Debugf("Failed to write spinner stop message: %v", err)
		}

		return
	}

	if message != "" {
		if _, err := fmt.Fprintf(s.writer, "%s %s\n", prefix, message); err != nil {
			logger.Log.Debugf("Failed to write spinner message: %v", err)
		}
	}
}
