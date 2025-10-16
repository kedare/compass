package gcp

import (
	"context"
	"net/http"
	"time"

	"cx/internal/logger"
	"golang.org/x/oauth2/google"
)

type loggingTransport struct {
	base http.RoundTripper
}

func (t loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	start := time.Now()
	resp, err := base.RoundTrip(req)
	elapsed := time.Since(start)

	if err != nil {
		logger.Log.Debugf("GCP HTTP %s %s failed after %s: %v", req.Method, req.URL.String(), elapsed, err)

		return nil, err
	}

	status := 0
	if resp != nil {
		status = resp.StatusCode
	}

	logger.Log.Debugf("GCP HTTP %s %s -> %d (%s)", req.Method, req.URL.String(), status, elapsed)

	return resp, nil
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
