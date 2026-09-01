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
// same path *without rewriting the file*.
//
// Comparing the two returned paths does not test that. The path is a
// fixed name under os.TempDir(), so a resolveIcon with the Once removed
// returns the same string on every call and writes the PNG every time —
// measured: that mutation survived the earlier version of this test.
// The contract only becomes observable on disk, so the check is to
// overwrite the file and require the second call to leave it alone.
func TestResolveIconIsIdempotent(t *testing.T) {
	first := resolveIcon()
	// Checked here and not left to the sibling test above: resolveIcon
	// returns "" when the write failed, and two empty strings are equal
	// — so without this line the whole test passes on the failure it is
	// meant to be indifferent to.
	if first == "" {
		t.Fatal("resolveIcon returned an empty path")
	}

	// The path is process-independent (a fixed name in the temp dir), so
	// put the real icon back rather than leaving a marker for the next
	// run — or for a Send in another test binary.
	t.Cleanup(func() {
		if err := os.WriteFile(first, iconBytes, 0o644); err != nil {
			t.Errorf("restoring the icon at %s: %v", first, err)
		}
	})

	marker := []byte("deliberately not a PNG")
	if err := os.WriteFile(first, marker, 0o644); err != nil {
		t.Fatalf("overwriting the icon at %s: %v", first, err)
	}

	second := resolveIcon()
	if second != first {
		t.Errorf("resolveIcon returned %q then %q", first, second)
	}

	after, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("reading %s back: %v", first, err)
	}
	if !bytes.Equal(after, marker) {
		t.Errorf("the second call rewrote the file (%d bytes on disk), want the %d-byte marker untouched",
			len(after), len(marker))
	}
}
