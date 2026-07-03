package github

import (
	"context"
	"errors"
	"reflect"
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
		{"expired or revoked token (401 body)", errors.New(`non-200 OK status code: 401 Unauthorized body: "{\"message\":\"Bad credentials\"}"`), ReasonAuth},
		{"classic PAT missing a scope", errors.New("Your token has not been granted the required scopes to execute this query. The 'starredRepositories' field requires one of the following scopes: ['read:user'], but your token has only been granted the: ['repo'] scopes."), ReasonAuthScope},
		{"fine-grained PAT missing a permission", errors.New("Resource not accessible by personal access token"), ReasonAuthScope},
		{"REST admin-rights 403", errors.New("Must have admin rights to Repository."), ReasonAuthScope},
		{"graphql not-found (deleted or renamed)", errors.New("Could not resolve to a Repository with the name 'owner/gone'."), ReasonNotFound},
		{"rest 404", errors.New(`non-200 OK status code: 404 Not Found body: "{\"message\":\"Not Found\"}"`), ReasonNotFound},
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

// TestMissingScopes pins the scope-name extraction from GitHub's
// insufficient-scopes wording: required scopes are captured, the
// "granted the:" list is not, and names are sanitized, deduped and
// capped since they ride inside a GitHub-controlled error body.
func TestMissingScopes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want []string
	}{
		{"single scope", errors.New("The 'starredRepositories' field requires one of the following scopes: ['read:user'], but your token has only been granted the: ['repo'] scopes."), []string{"read:user"}},
		{"multiple scopes, deduped", errors.New("requires one of the following scopes: ['read:user', 'user:email', 'read:user']"), []string{"read:user", "user:email"}},
		{"granted list is not mistaken for required", errors.New("requires one of the following scopes: ['read:org'], but your token has only been granted the: ['repo', 'gist'] scopes."), []string{"read:org"}},
		{"no scope list in the message", errors.New("401 Bad credentials"), nil},
		{"nil error", nil, nil},
		{"hostile scope name is sanitized", errors.New("following scopes: ['read:user\x1b[2J\x1b[31m']"), []string{"read:user"}},
		{"capped at six", errors.New("following scopes: ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h']"), []string{"a", "b", "c", "d", "e", "f"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MissingScopes(c.err); !reflect.DeepEqual(got, c.want) {
				t.Errorf("MissingScopes(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}
