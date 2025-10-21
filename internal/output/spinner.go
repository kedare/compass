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
// degrades to log-style messages otherwise.
type Spinner struct {
	mu         sync.Mutex
	message    string
	writer     *os.File
	enabled    bool
	active     bool
	stopped    bool
	spinner    *pterm.SpinnerPrinter
	multi      *multiSpinnerManager
	multiInUse bool
}

// NewSpinner creates a spinner that renders to stderr. Call Start before recording progress.
func NewSpinner(message string) *Spinner {
	writer := os.Stderr
	enabled := term.IsTerminal(int(writer.Fd()))

	var multi *multiSpinnerManager
	if enabled {
		multi = defaultMultiManager()
	}

	return &Spinner{
		message: message,
		writer:  writer,
		enabled: enabled,
		multi:   multi,
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
			sp, startErr := pterm.DefaultSpinner.
				WithShowTimer(false).
				WithRemoveWhenDone(true).
				WithWriter(writer).
				Start(s.message)
			if startErr != nil {
				logger.Log.Debugf("Failed to start spinner: %v", startErr)
				s.enabled = false
				s.multi.release()
			} else {
				s.spinner = sp
				s.multiInUse = true
			}
		}
	}

	if !s.enabled || s.spinner == nil {
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

	if s.spinner != nil {
		s.spinner.UpdateText(message)

		return
	}

	if _, err := fmt.Fprintf(s.writer, "%s...\n", message); err != nil {
		logger.Log.Debugf("Failed to write spinner update: %v", err)
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

func (s *Spinner) stop(fn func(*pterm.SpinnerPrinter), fallbackPrefix, fallbackMessage string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return
	}
	s.stopped = true

	if s.spinner != nil {
		fn(s.spinner)
	} else if fallbackMessage != "" {
		if _, err := fmt.Fprintf(s.writer, "%s %s\n", fallbackPrefix, fallbackMessage); err != nil {
			logger.Log.Debugf("Failed to write spinner message: %v", err)
		}
	}

	if s.multiInUse && s.multi != nil {
		s.multi.release()
		s.multiInUse = false
	}
}

// multiSpinnerManager coordinates a shared multi-printer so multiple spinners can
// render concurrently without clobbering each other.
type multiSpinnerManager struct {
	mu      sync.Mutex
	printer *pterm.MultiPrinter
	started bool
	refs    int
}

var (
	multiOnce   sync.Once
	globalMulti *multiSpinnerManager
)

func defaultMultiManager() *multiSpinnerManager {
	multiOnce.Do(func() {
		printer := pterm.DefaultMultiPrinter
		printer.SetWriter(os.Stderr)
		globalMulti = &multiSpinnerManager{
			printer: &printer,
		}
	})

	return globalMulti
}

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

	return m.printer.NewWriter(), nil
}

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
		printer := pterm.DefaultMultiPrinter
		printer.SetWriter(os.Stderr)
		m.printer = &printer
		m.started = false
	}
}
