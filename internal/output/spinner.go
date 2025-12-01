package output

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/kedare/compass/internal/logger"
	"github.com/pterm/pterm"
	"golang.org/x/term"
)

// Spinner provides a terminal spinner using pterm when a TTY is available and gracefully
// degrades to log-style messages otherwise. Spinners are completely suppressed in JSON mode.
type Spinner struct {
	mu            sync.Mutex
	message       string
	writer        *os.File
	spinnerWriter io.Writer
	enabled       bool
	active        bool
	stopped       bool
	spinner       *pterm.SpinnerPrinter
	multi         *multiSpinnerManager
	multiInUse    bool
	jsonMode      bool // If true, suppress all output including fallback messages
}

// NewSpinner creates a spinner that renders to stderr. Call Start before recording progress.
// Spinners are automatically disabled when JSON output mode is active to avoid corrupting
// the JSON stream.
func NewSpinner(message string) *Spinner {
	writer := os.Stderr
	enabled := term.IsTerminal(int(writer.Fd()))
	jsonMode := IsJSONMode()

	// Disable spinners when in JSON mode to keep stdout clean
	if jsonMode {
		enabled = false
	}

	var multi *multiSpinnerManager
	if enabled {
		multi = defaultMultiManager()
	}

	return &Spinner{
		message:  message,
		writer:   writer,
		enabled:  enabled,
		multi:    multi,
		jsonMode: jsonMode,
	}
}

// Start begins rendering the spinner.
func (s *Spinner) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped || s.active {
		return
	}

	if s.enabled && s.multi != nil {
		writer, err := s.multi.acquireWriter()
		if err != nil {
			logger.Log.Debugf("Failed to initialize spinner writer: %v", err)
			s.enabled = false
		} else {
			s.spinnerWriter = writer
			if s.startSpinnerLocked(s.message) {
				s.multiInUse = true
			} else {
				s.spinnerWriter = nil
				s.enabled = false
				s.multi.release()
			}
		}
	}

	if !s.enabled || s.spinner == nil {
		// In JSON mode, suppress all spinner output to avoid corrupting the JSON stream
		if !s.jsonMode {
			if _, err := fmt.Fprintf(s.writer, "%s...\n", s.message); err != nil {
				logger.Log.Debugf("Failed to write spinner message: %v", err)
			}
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

	if s.spinner != nil {
		s.restartSpinnerLocked(message)

		return
	}

	// In JSON mode, suppress all spinner output
	if !s.jsonMode {
		if _, err := fmt.Fprintf(s.writer, "%s...\n", message); err != nil {
			logger.Log.Debugf("Failed to write spinner update: %v", err)
		}
	}
}

// Stop stops the spinner without printing an additional message.
func (s *Spinner) Stop() {
	s.stop(func(sp *pterm.SpinnerPrinter) {
		_ = sp.Stop()
	}, "", "")
}

// Success stops the spinner and prints a success message.
func (s *Spinner) Success(message string) {
	s.stop(func(sp *pterm.SpinnerPrinter) {
		sp.Success(message)
	}, "✓", message)
}

// Fail stops the spinner and prints a failure message.
func (s *Spinner) Fail(message string) {
	s.stop(func(sp *pterm.SpinnerPrinter) {
		sp.Fail(message)
	}, "✗", message)
}

// Info stops the spinner and prints an informational message.
func (s *Spinner) Info(message string) {
	s.stop(func(sp *pterm.SpinnerPrinter) {
		sp.Info(message)
	}, "•", message)
}

// stop is the internal implementation for stopping a spinner with various outcomes.
func (s *Spinner) stop(fn func(*pterm.SpinnerPrinter), fallbackPrefix, fallbackMessage string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return
	}
	s.stopped = true

	if s.spinner != nil {
		fn(s.spinner)
		// Don't clear here - cleanup happens in multiSpinnerManager.release() when all spinners are done
	} else if fallbackMessage != "" && !s.jsonMode {
		// In JSON mode, suppress all spinner output including completion messages
		if _, err := fmt.Fprintf(s.writer, "%s %s\n", fallbackPrefix, fallbackMessage); err != nil {
			logger.Log.Debugf("Failed to write spinner message: %v", err)
		}
	}

	if s.multiInUse && s.multi != nil {
		s.multi.release()
		s.multiInUse = false
	}

	s.spinnerWriter = nil
}

// startSpinnerLocked initializes the underlying pterm spinner with the current writer.
// The caller must hold s.mu.
func (s *Spinner) startSpinnerLocked(message string) bool {
	if s.spinnerWriter == nil {
		return false
	}

	sp, err := pterm.DefaultSpinner.
		WithShowTimer(false).
		WithRemoveWhenDone(true).
		WithWriter(s.spinnerWriter).
		Start(message)
	if err != nil {
		logger.Log.Debugf("Failed to start spinner: %v", err)

		return false
	}

	s.spinner = sp

	return true
}

func (s *Spinner) restartSpinnerLocked(message string) {
	if s.spinner == nil || s.spinnerWriter == nil {
		return
	}

	// Stopping avoids calling SpinnerPrinter.UpdateText, which can race with the
	// internal render loop and panic when the string header is partially written.
	if err := s.spinner.Stop(); err != nil {
		logger.Log.Debugf("Failed to stop spinner before update: %v", err)
	}

	if s.startSpinnerLocked(message) {
		return
	}

	// If restarting fails, disable spinner and fallback to writer output.
	s.spinner = nil
	s.enabled = false
	if s.multiInUse && s.multi != nil {
		s.multi.release()
		s.multiInUse = false
	}
	s.spinnerWriter = nil

	if !s.jsonMode {
		if _, err := fmt.Fprintf(s.writer, "%s...\n", message); err != nil {
			logger.Log.Debugf("Failed to write spinner fallback message: %v", err)
		}
	}
}

// multiSpinnerManager coordinates a shared multi-printer so multiple spinners can
// render concurrently without clobbering each other.
type multiSpinnerManager struct {
	mu         sync.Mutex
	printer    *pterm.MultiPrinter
	started    bool
	refs       int
	totalSpins int      // Total spinners created
	writer     *os.File // Writer for cleanup
}

var (
	multiOnce   sync.Once
	globalMulti *multiSpinnerManager
)

// defaultMultiManager returns the global singleton multi-spinner manager.
func defaultMultiManager() *multiSpinnerManager {
	multiOnce.Do(func() {
		printer := pterm.DefaultMultiPrinter
		printer.SetWriter(os.Stderr)
		globalMulti = &multiSpinnerManager{
			printer: &printer,
			writer:  os.Stderr,
		}
	})

	return globalMulti
}

// acquireWriter starts the multi-printer if needed and returns a writer for a new spinner.
func (m *multiSpinnerManager) acquireWriter() (io.Writer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		if _, err := m.printer.Start(); err != nil {
			return nil, err
		}
		m.started = true
	}

	m.refs++
	m.totalSpins++ // Track total spinners created

	return m.printer.NewWriter(), nil
}

// release decrements the reference count and cleans up when all spinners are done.
func (m *multiSpinnerManager) release() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.refs > 0 {
		m.refs--
	}

	if m.refs == 0 && m.started {
		if _, err := m.printer.Stop(); err != nil {
			logger.Log.Debugf("Failed to stop multi spinner: %v", err)
		}

		// Clear all spinner lines - they leave empty lines with WithRemoveWhenDone(true)
		// Similar to progress bars, clear totalSpins lines
		if m.totalSpins > 0 {
			for i := 0; i < m.totalSpins; i++ {
				if _, err := fmt.Fprintf(m.writer, "\033[1A\033[2K"); err != nil {
					logger.Log.Debugf("Failed to clear spinner line: %v", err)
				}
			}
		}

		printer := pterm.DefaultMultiPrinter
		printer.SetWriter(os.Stderr)
		m.printer = &printer
		m.started = false
		m.totalSpins = 0 // Reset counter for next batch
	}
}
