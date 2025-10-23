package gcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/api/networkmanagement/v1"
)

func withOperationBackoff(t *testing.T, timeout time.Duration, cfg backoffConfig) {
	originalTimeout := operationTimeout
	originalFactory := newOperationBackoff
	operationTimeout = timeout
	newOperationBackoff = func() backoffConfig { return cfg }

	t.Cleanup(func() {
		operationTimeout = originalTimeout
		newOperationBackoff = originalFactory
	})
}

func TestWaitForOperationWithBackoffSuccess(t *testing.T) {
	withOperationBackoff(t, 200*time.Millisecond, backoffConfig{initial: 5 * time.Millisecond, multiplier: 1.0, max: 5 * time.Millisecond})

	attempts := 0

	err := waitForOperationWithBackoff(t.Context(), func(context.Context) (*networkmanagement.Operation, error) {
		attempts++
		if attempts >= 2 {
			return &networkmanagement.Operation{Done: true}, nil
		}

		return &networkmanagement.Operation{Done: false}, nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, attempts)
}

func TestWaitForOperationWithBackoffContextCancel(t *testing.T) {
	withOperationBackoff(t, 200*time.Millisecond, backoffConfig{initial: 5 * time.Millisecond, multiplier: 1.0, max: 5 * time.Millisecond})

	ctx, cancel := context.WithCancel(t.Context())
	attempts := 0
	err := waitForOperationWithBackoff(ctx, func(context.Context) (*networkmanagement.Operation, error) {
		attempts++

		cancel()

		return &networkmanagement.Operation{Done: false}, nil
	})

	require.ErrorIs(t, err, context.Canceled)
	require.NotZero(t, attempts)
}

func TestWaitForOperationWithBackoffError(t *testing.T) {
	withOperationBackoff(t, 200*time.Millisecond, backoffConfig{initial: 5 * time.Millisecond, multiplier: 1.0, max: 5 * time.Millisecond})

	sentinel := errors.New("boom")
	err := waitForOperationWithBackoff(t.Context(), func(context.Context) (*networkmanagement.Operation, error) {
		return nil, sentinel
	})

	require.ErrorIs(t, err, sentinel)
}

func TestWaitForOperationWithBackoffTimeout(t *testing.T) {
	withOperationBackoff(t, 40*time.Millisecond, backoffConfig{initial: 10 * time.Millisecond, multiplier: 1.0, max: 10 * time.Millisecond})

	start := time.Now()
	err := waitForOperationWithBackoff(t.Context(), func(context.Context) (*networkmanagement.Operation, error) {
		return &networkmanagement.Operation{Done: false}, nil
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "timed out")

	require.GreaterOrEqual(t, time.Since(start), 20*time.Millisecond)
}
