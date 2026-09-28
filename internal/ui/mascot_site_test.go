package ui

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestLandingOctopusIsTheLaunchMascot holds the octopus on the landing page
// to the one the TUI draws. The page carries its own copy of the art, as JSON
// that docs/landing.js composes into SVG, because the site is static files
// with no build step to generate it from mascot.go. A copy nothing checks is
// a copy that drifts: redraw the octopus here and the page would keep the old
// one, and nothing would say so.
func TestLandingOctopusIsTheLaunchMascot(t *testing.T) {
	b, err := os.ReadFile("../../docs/index.html")
	if err != nil {
		t.Fatalf("read docs/index.html: %v", err)
	}
	page := string(b)
	const open = `<script type="application/json" id="octopus-art">`
	i := strings.Index(page, open)
	if i < 0 {
		t.Fatal("docs/index.html has no #octopus-art block")
	}
	j := strings.Index(page[i:], "</script>")
	if j < 0 {
		t.Fatal("the #octopus-art block is not closed")
	}

	var site struct {
		Top  map[string][]string `json:"top"`
		Head []string            `json:"head"`
		Tent map[string][]string `json:"tent"`
		EyeX [2]int              `json:"eyeX"`
		EyeY int                 `json:"eyeY"`
	}
	dec := json.NewDecoder(strings.NewReader(page[i+len(open) : i+j]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&site); err != nil {
		t.Fatalf("decode #octopus-art: %v", err)
	}

	a := mascotLaunch
	want := map[string][]string{"-1": a.top[-1], "0": a.top[0], "1": a.top[1]}
	if !reflect.DeepEqual(site.Top, want) {
		t.Errorf("periscope rows differ from mascotLaunch.top:\n got %q\nwant %q", site.Top, want)
	}
	if !reflect.DeepEqual(site.Head, a.head) {
		t.Errorf("head rows differ from mascotLaunch.head:\n got %q\nwant %q", site.Head, a.head)
	}
	tent := map[string][]string{"splayed": a.tent[false], "curled": a.tent[true]}
	if !reflect.DeepEqual(site.Tent, tent) {
		t.Errorf("tentacle rows differ from mascotLaunch.tent:\n got %q\nwant %q", site.Tent, tent)
	}
	if site.EyeX != a.eyeX || site.EyeY != a.eyeY {
		t.Errorf("eyes at x %v y %d, mascotLaunch has x %v y %d", site.EyeX, site.EyeY, a.eyeX, a.eyeY)
	}
}
