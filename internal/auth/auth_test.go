package auth

import (
	"errors"
	"testing"
)

// TestTokenResolutionOrder covers the three-way fallback in Token():
// env var wins, gh CLI is the fallback, errors and whitespace yield "".
func TestTokenResolutionOrder(t *testing.T) {
	cases := []struct {
		name   string
		envVal string
		cliOut []byte
		cliErr error
		want   string
	}{
		{
			name:   "env var wins over the CLI",
			envVal: "env-token",
			cliOut: []byte("cli-token"),
			cliErr: nil,
			want:   "env-token",
		},
		{
			name:   "env var is trimmed",
			envVal: "  env-token\n",
			cliOut: nil,
			cliErr: nil,
			want:   "env-token",
		},
		{
			name:   "empty env falls through to the CLI",
			envVal: "",
			cliOut: []byte("cli-token\n"),
			cliErr: nil,
			want:   "cli-token",
		},
		{
			name:   "CLI error yields empty string",
			envVal: "",
			cliOut: nil,
			cliErr: errors.New("gh not logged in"),
			want:   "",
		},
		{
			name:   "CLI whitespace-only yields empty string",
			envVal: "",
			cliOut: []byte("   \n"),
			cliErr: nil,
			want:   "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("GITHUB_TOKEN", c.envVal)

			orig := ghTokenOutput
			defer func() { ghTokenOutput = orig }()
			cliOut, cliErr := c.cliOut, c.cliErr
			ghTokenOutput = func() ([]byte, error) { return cliOut, cliErr }

			if got := Token(); got != c.want {
				t.Errorf("got = %q, want %q", got, c.want)
			}
		})
	}
}
