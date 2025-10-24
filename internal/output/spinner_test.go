package output

import (
	"testing"

	"github.com/pterm/pterm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSpinner(t *testing.T) {
	t.Run("creates spinner with message", func(t *testing.T) {
		spinner := NewSpinner("test message")
		require.NotNil(t, spinner)
		assert.Equal(t, "test message", spinner.message)
		assert.NotNil(t, spinner.writer)
		assert.False(t, spinner.active)
		assert.False(t, spinner.stopped)
	})

	t.Run("disables spinner in JSON mode", func(t *testing.T) {
		// Save original JSON mode state
		originalJSONMode := IsJSONMode()
		defer func() {
			if originalJSONMode {
				SetFormat("json")
			} else {
				SetFormat("text")
			}
		}()

		SetFormat("json")
		spinner := NewSpinner("test message")
		require.NotNil(t, spinner)
		assert.True(t, spinner.jsonMode)
		assert.False(t, spinner.enabled) // Should be disabled in JSON mode
	})
}

func TestSpinnerStartAndStop(t *testing.T) {
	t.Run("start marks spinner as active", func(t *testing.T) {
		spinner := NewSpinner("test")
		assert.False(t, spinner.active)
		spinner.Start()
		assert.True(t, spinner.active)
	})

	t.Run("start is idempotent", func(t *testing.T) {
		spinner := NewSpinner("test")
		spinner.Start()
		assert.True(t, spinner.active)
		// Starting again should not panic or change state
		spinner.Start()
		assert.True(t, spinner.active)
	})

	t.Run("stop marks spinner as stopped", func(t *testing.T) {
		spinner := NewSpinner("test")
		spinner.Start()
		spinner.Stop()
		assert.True(t, spinner.stopped)
	})

	t.Run("stop is idempotent", func(t *testing.T) {
		spinner := NewSpinner("test")
		spinner.Start()
		spinner.Stop()
		assert.True(t, spinner.stopped)
		// Stopping again should not panic
		spinner.Stop()
		assert.True(t, spinner.stopped)
	})

	t.Run("cannot start after stop", func(t *testing.T) {
		spinner := NewSpinner("test")
		spinner.Start()
		spinner.Stop()
		// Starting again after stop should not reactivate
		spinner.Start()
		assert.True(t, spinner.stopped)
	})
}

func TestSpinnerUpdate(t *testing.T) {
	t.Run("updates message", func(t *testing.T) {
		spinner := NewSpinner("initial")
		assert.Equal(t, "initial", spinner.message)
		spinner.Update("updated")
		assert.Equal(t, "updated", spinner.message)
	})

	t.Run("update when not active does not panic", func(t *testing.T) {
		spinner := NewSpinner("test")
		spinner.Update("new message")
		assert.Equal(t, "new message", spinner.message)
	})

	t.Run("update when stopped does not panic", func(t *testing.T) {
		spinner := NewSpinner("test")
		spinner.Start()
		spinner.Stop()
		spinner.Update("new message after stop")
		assert.Equal(t, "new message after stop", spinner.message)
	})
}

func TestSpinnerSuccessFailInfo(t *testing.T) {
	t.Run("success stops spinner", func(t *testing.T) {
		spinner := NewSpinner("test")
		spinner.Start()
		spinner.Success("done")
		assert.True(t, spinner.stopped)
	})

	t.Run("fail stops spinner", func(t *testing.T) {
		spinner := NewSpinner("test")
		spinner.Start()
		spinner.Fail("error")
		assert.True(t, spinner.stopped)
	})

	t.Run("info stops spinner", func(t *testing.T) {
		spinner := NewSpinner("test")
		spinner.Start()
		spinner.Info("info message")
		assert.True(t, spinner.stopped)
	})

	t.Run("success is idempotent", func(t *testing.T) {
		spinner := NewSpinner("test")
		spinner.Start()
		spinner.Success("done")
		// Calling again should not panic
		spinner.Success("done again")
		assert.True(t, spinner.stopped)
	})
}

func TestSpinnerJSONMode(t *testing.T) {
	// Save original JSON mode state
	originalJSONMode := IsJSONMode()
	defer func() {
		if originalJSONMode {
			SetFormat("json")
		} else {
			SetFormat("text")
		}
	}()

	t.Run("spinner suppressed in JSON mode", func(t *testing.T) {
		SetFormat("json")
		spinner := NewSpinner("test")
		assert.True(t, spinner.jsonMode)
		// In JSON mode, spinner should be disabled
		assert.False(t, spinner.enabled)
	})

	t.Run("spinner enabled in non-JSON mode", func(t *testing.T) {
		SetFormat("text")
		spinner := NewSpinner("test")
		assert.False(t, spinner.jsonMode)
	})
}

func TestMultiSpinnerManager(t *testing.T) {
	t.Run("defaultMultiManager returns singleton", func(t *testing.T) {
		mgr1 := defaultMultiManager()
		mgr2 := defaultMultiManager()
		assert.Same(t, mgr1, mgr2, "Should return the same singleton instance")
	})

	t.Run("acquireWriter increments refs", func(t *testing.T) {
		printer := pterm.DefaultMultiPrinter
		mgr := &multiSpinnerManager{
			printer: &printer,
			writer:  globalMulti.writer,
		}
		initialRefs := mgr.refs

		_, err := mgr.acquireWriter()
		require.NoError(t, err)
		assert.Equal(t, initialRefs+1, mgr.refs)

		// Clean up
		mgr.release()
	})

	t.Run("release decrements refs", func(t *testing.T) {
		printer := pterm.DefaultMultiPrinter
		mgr := &multiSpinnerManager{
			printer: &printer,
			writer:  globalMulti.writer,
		}

		_, err := mgr.acquireWriter()
		require.NoError(t, err)
		initialRefs := mgr.refs

		mgr.release()
		assert.Equal(t, initialRefs-1, mgr.refs)
	})

	t.Run("release does not go below zero", func(t *testing.T) {
		printer := pterm.DefaultMultiPrinter
		mgr := &multiSpinnerManager{
			printer: &printer,
			writer:  globalMulti.writer,
		}
		mgr.refs = 0

		mgr.release()
		assert.Equal(t, 0, mgr.refs)
	})
}
