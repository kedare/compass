package gcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
	err := waitForOperationWithBackoff(context.Background(), func(context.Context) (*networkmanagement.Operation, error) {
		attempts++
		if attempts >= 2 {
			return &networkmanagement.Operation{Done: true}, nil
		}

		return &networkmanagement.Operation{Done: false}, nil
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if attempts != 2 {
		t.Fatalf("expected two polls, got %d", attempts)
	}
}

func TestWaitForOperationWithBackoffContextCancel(t *testing.T) {
	withOperationBackoff(t, 200*time.Millisecond, backoffConfig{initial: 5 * time.Millisecond, multiplier: 1.0, max: 5 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := waitForOperationWithBackoff(ctx, func(context.Context) (*networkmanagement.Operation, error) {
		attempts++
		cancel()
		return &networkmanagement.Operation{Done: false}, nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	if attempts == 0 {
		t.Fatal("expected at least one poll attempt")
	}
}

func TestWaitForOperationWithBackoffError(t *testing.T) {
	withOperationBackoff(t, 200*time.Millisecond, backoffConfig{initial: 5 * time.Millisecond, multiplier: 1.0, max: 5 * time.Millisecond})

	sentinel := errors.New("boom")
	err := waitForOperationWithBackoff(context.Background(), func(context.Context) (*networkmanagement.Operation, error) {
		return nil, sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestWaitForOperationWithBackoffTimeout(t *testing.T) {
	withOperationBackoff(t, 40*time.Millisecond, backoffConfig{initial: 10 * time.Millisecond, multiplier: 1.0, max: 10 * time.Millisecond})

	start := time.Now()
	err := waitForOperationWithBackoff(context.Background(), func(context.Context) (*networkmanagement.Operation, error) {
		return &networkmanagement.Operation{Done: false}, nil
	})

	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}

	if time.Since(start) < 20*time.Millisecond {
		t.Fatalf("timeout returned too quickly, duration %s", time.Since(start))
	}
}
