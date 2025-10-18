package gcp

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/kedare/compass/internal/logger"
	"github.com/sirupsen/logrus"
)

type stubRoundTripper struct {
	resp *http.Response
	err  error
	req  *http.Request
}

func (s *stubRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	s.req = req

	return s.resp, s.err
}

func withLoggerOutput(t *testing.T) *bytes.Buffer {
	buf := &bytes.Buffer{}
	originalOut := logger.Log.Out
	originalLevel := logger.Log.GetLevel()
	logger.Log.SetOutput(buf)
	logger.Log.SetLevel(logrus.DebugLevel)
	t.Cleanup(func() {
		logger.Log.SetOutput(originalOut)
		logger.Log.SetLevel(originalLevel)
	})

	return buf
}

func TestLoggingTransportSuccess(t *testing.T) {
	buf := withLoggerOutput(t)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBuffer(nil)),
	}
	stub := &stubRoundTripper{resp: resp}
	transport := loggingTransport{base: stub}

	req, _ := http.NewRequest(http.MethodGet, "https://example.com/test", nil)

	result, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result != resp {
		t.Fatalf("expected response to pass through")
	}

	if stub.req != req {
		t.Fatalf("expected request to pass through")
	}

	if !bytes.Contains(buf.Bytes(), []byte("GCP HTTP GET https://example.com/test -> 200")) {
		t.Fatalf("expected log entry, got %s", buf.String())
	}
}

func TestLoggingTransportError(t *testing.T) {
	buf := withLoggerOutput(t)
	sentinel := errors.New("boom")
	stub := &stubRoundTripper{err: sentinel}
	transport := loggingTransport{base: stub}

	req, _ := http.NewRequest(http.MethodPost, "https://example.com/fail", nil)

	_, err := transport.RoundTrip(req)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}

	if !bytes.Contains(buf.Bytes(), []byte("GCP HTTP POST https://example.com/fail failed")) {
		t.Fatalf("expected failure log, got %s", buf.String())
	}
}

func TestAttachLoggingTransport(t *testing.T) {
	client := &http.Client{}
	attachLoggingTransport(client)

	if _, ok := client.Transport.(loggingTransport); !ok {
		t.Fatalf("expected loggingTransport to be attached")
	}
}
