package notify

import (
	"bytes"
	"os"
	"testing"
)

// TestResolveIconWritesTheEmbeddedPNG covers the one part of this
// package that is not somebody else's code: Send falls through to beeep
// everywhere except macOS, so the octoscope-specific behaviour on
// Windows and Linux is entirely "write the embedded icon somewhere the
// backend can read it".
//
// Worth a test on every platform because the path comes from
// os.TempDir(), which is %TEMP% on Windows and /tmp elsewhere — the
// kind of difference that only shows up on the platform nobody runs.
func TestResolveIconWritesTheEmbeddedPNG(t *testing.T) {
	path := resolveIcon()
	if path == "" {
		t.Fatal("resolveIcon returned an empty path")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the icon back from %s: %v", path, err)
	}
	if !bytes.Equal(got, iconBytes) {
		t.Errorf("icon on disk is %d bytes, embedded is %d", len(got), len(iconBytes))
	}
	if !bytes.HasPrefix(got, []byte("\x89PNG\r\n\x1a\n")) {
		t.Error("the written file is not a PNG")
	}
}

// TestResolveIconIsIdempotent pins the sync.Once contract: Send calls
// resolveIcon on every notification, so a second call must return the
// same path without rewriting the file.
func TestResolveIconIsIdempotent(t *testing.T) {
	first := resolveIcon()
	if second := resolveIcon(); second != first {
		t.Errorf("resolveIcon returned %q then %q", first, second)
	}
}
