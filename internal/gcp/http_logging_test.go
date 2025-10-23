package gcp

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
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

func TestLoggingTransportSuccess(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBuffer(nil)),
	}
	stub := &stubRoundTripper{resp: resp}
	transport := loggingTransport{base: stub}

	req, _ := http.NewRequest(http.MethodGet, "https://example.com/test", nil)

	result, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Same(t, resp, result)
	require.Same(t, req, stub.req)
}

func TestLoggingTransportError(t *testing.T) {
	sentinel := errors.New("boom")
	stub := &stubRoundTripper{err: sentinel}
	transport := loggingTransport{base: stub}

	req, _ := http.NewRequest(http.MethodPost, "https://example.com/fail", nil)

	_, err := transport.RoundTrip(req)
	require.ErrorIs(t, err, sentinel)
}

func TestAttachLoggingTransport(t *testing.T) {
	client := &http.Client{}
	attachLoggingTransport(client)

	_, ok := client.Transport.(loggingTransport)
	require.True(t, ok)
}
