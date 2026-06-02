package github

import (
	"context"
	"errors"
	"testing"
)

// TestClassifyErr pins the reason mapping, especially that transient
// HTTP/2 stream errors (RST_STREAM / GOAWAY from a congested gateway)
// are classified ReasonServer so the UI retries them like a 502.
func TestClassifyErr(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		err  error
		want FetchErrorReason
	}{
		{"http2 stream cancel from peer", errors.New("stream error: stream ID 25; CANCEL; received from peer"), ReasonServer},
		{"http2 internal error", errors.New("http2: server sent GOAWAY and closed the connection"), ReasonServer},
		{"502 bad gateway", errors.New(`non-200 OK status code: 502 Bad Gateway body: "<html>..."`), ReasonServer},
		{"503", errors.New("503 Service Unavailable"), ReasonServer},
		{"primary rate limit", errors.New("API rate limit exceeded"), ReasonRateLimitPrimary},
		{"secondary rate limit", errors.New("you have exceeded a secondary rate limit"), ReasonRateLimitSecondary},
		{"bad credentials", errors.New("401 Bad credentials"), ReasonAuth},
		{"network unreachable", errors.New("dial tcp: network is unreachable"), ReasonNetwork},
		{"tls handshake", errors.New("tls: handshake failure"), ReasonNetwork},
		{"unknown", errors.New("something weird happened"), ReasonUnknown},
		{"nil", nil, ReasonUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyErr(ctx, c.err); got != c.want {
				t.Errorf("classifyErr(%q) = %d, want %d", c.err, got, c.want)
			}
		})
	}
}
