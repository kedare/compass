package gcp

import (
	"context"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/kedare/compass/internal/logger"
	"golang.org/x/oauth2/google"
)

const (
	maxRetries     = 3
	initialBackoff = 1 * time.Second
	maxBackoff     = 10 * time.Second
)

type loggingTransport struct {
	base http.RoundTripper
}

// isRetryableStatusCode checks if an HTTP status code is retryable.
func isRetryableStatusCode(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || // 429
		statusCode == http.StatusServiceUnavailable || // 503
		statusCode == http.StatusGatewayTimeout || // 504
		statusCode == http.StatusInternalServerError // 500
}

// isReadOnlyRequest checks if a request is a safe read-only operation.
func isReadOnlyRequest(req *http.Request) bool {
	return req.Method == http.MethodGet || req.Method == http.MethodHead
}

func (t loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	// Check if context is already canceled before starting
	if req.Context() != nil {
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		default:
		}
	}

	var resp *http.Response
	var err error

	// Only retry read-only requests
	shouldRetry := isReadOnlyRequest(req)
	attempts := 1

	if shouldRetry {
		attempts = maxRetries
	}

	for attempt := 0; attempt < attempts; attempt++ {
		// If this is a retry, apply exponential backoff with context-aware sleep
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * initialBackoff
			if backoff > maxBackoff {
				backoff = maxBackoff
			}

			logger.Log.Debugf("GCP HTTP retrying %s %s after %s (attempt %d/%d)",
				req.Method, req.URL.String(), backoff, attempt+1, attempts)

			// Use context-aware sleep that can be interrupted
			select {
			case <-time.After(backoff):
				// Backoff complete, continue with retry
			case <-req.Context().Done():
				// Context canceled during backoff
				return nil, req.Context().Err()
			}
		}

		start := time.Now()
		resp, err = base.RoundTrip(req)
		elapsed := time.Since(start)

		// Check if context was canceled during the request
		if req.Context() != nil {
			select {
			case <-req.Context().Done():
				// Context was canceled, close response if exists and return immediately
				if resp != nil && resp.Body != nil {
					resp.Body.Close()
				}
				return nil, req.Context().Err()
			default:
			}
		}

		// If request failed with network error and it's retryable, continue to next attempt
		if err != nil {
			if shouldRetry && attempt < attempts-1 {
				logger.Log.Debugf("GCP HTTP %s %s failed after %s: %v (will retry)",
					req.Method, req.URL.String(), elapsed, err)

				continue
			}

			logger.Log.Debugf("GCP HTTP %s %s failed after %s: %v",
				req.Method, req.URL.String(), elapsed, err)

			return nil, err
		}

		status := 0
		if resp != nil {
			status = resp.StatusCode
		}

		// If we got a retryable status code and can retry, do so
		if shouldRetry && attempt < attempts-1 && isRetryableStatusCode(status) {
			logger.Log.Debugf("GCP HTTP %s %s -> %d (%s) (will retry)",
				req.Method, req.URL.String(), status, elapsed)

			// Drain and close the response body before retrying
			if resp != nil && resp.Body != nil {
				if _, copyErr := io.Copy(io.Discard, resp.Body); copyErr != nil {
					logger.Log.Debugf("Failed to drain response body: %v", copyErr)
				}
				if closeErr := resp.Body.Close(); closeErr != nil {
					logger.Log.Debugf("Failed to close response body: %v", closeErr)
				}
			}

			continue
		}

		logger.Log.Debugf("GCP HTTP %s %s -> %d (%s)", req.Method, req.URL.String(), status, elapsed)

		break
	}

	return resp, err
}

func attachLoggingTransport(client *http.Client) {
	if client == nil {
		return
	}

	client.Transport = loggingTransport{base: client.Transport}
}

func newHTTPClientWithLogging(ctx context.Context, scopes ...string) (*http.Client, error) {
	client, err := google.DefaultClient(ctx, scopes...)
	if err != nil {
		return nil, err
	}

	attachLoggingTransport(client)

	return client, nil
}
