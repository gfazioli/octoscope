package auth

import (
	"errors"
	"testing"
)

// TestTokenResolutionOrder covers the three-way fallback in
// TokenSource(): env var wins, gh CLI is the fallback, errors and
// whitespace yield "" — and each path reports the matching Source so
// auth-error hints can name the right fix.
func TestTokenResolutionOrder(t *testing.T) {
	cases := []struct {
		name    string
		envVal  string
		cliOut  []byte
		cliErr  error
		want    string
		wantSrc Source
	}{
		{
			name:    "env var wins over the CLI",
			envVal:  "env-token",
			cliOut:  []byte("cli-token"),
			cliErr:  nil,
			want:    "env-token",
			wantSrc: SourceEnv,
		},
		{
			name:    "env var is trimmed",
			envVal:  "  env-token\n",
			cliOut:  nil,
			cliErr:  nil,
			want:    "env-token",
			wantSrc: SourceEnv,
		},
		{
			name:    "empty env falls through to the CLI",
			envVal:  "",
			cliOut:  []byte("cli-token\n"),
			cliErr:  nil,
			want:    "cli-token",
			wantSrc: SourceGHCLI,
		},
		{
			name:    "CLI error yields empty string",
			envVal:  "",
			cliOut:  nil,
			cliErr:  errors.New("gh not logged in"),
			want:    "",
			wantSrc: SourceNone,
		},
		{
			name:    "CLI whitespace-only yields empty string",
			envVal:  "",
			cliOut:  []byte("   \n"),
			cliErr:  nil,
			want:    "",
			wantSrc: SourceNone,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("GITHUB_TOKEN", c.envVal)

			orig := ghTokenOutput
			defer func() { ghTokenOutput = orig }()
			cliOut, cliErr := c.cliOut, c.cliErr
			ghTokenOutput = func() ([]byte, error) { return cliOut, cliErr }

			got, gotSrc := TokenSource()
			if got != c.want || gotSrc != c.wantSrc {
				t.Errorf("TokenSource() = (%q, %d), want (%q, %d)", got, gotSrc, c.want, c.wantSrc)
			}
			// Token() must stay a plain delegation to the same resolution.
			if got := Token(); got != c.want {
				t.Errorf("Token() = %q, want %q", got, c.want)
			}
		})
	}
}
