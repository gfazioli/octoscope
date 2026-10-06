package ui

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"strconv"
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
	// One value and nothing after it: JSON.parse in landing.js rejects
	// trailing data, and would drop the octopus while this still passed.
	if tok, err := dec.Token(); err != io.EOF {
		t.Fatalf("#octopus-art carries more after its one JSON value (%v, %v) — JSON.parse would reject the block", tok, err)
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

// TestGuideOctopusIsTheLaunchMascot holds the octopus the guide's notes show
// to the one the TUI draws, looking left with its tentacles splayed. The guide
// pages run no script to draw it, so it is a static file, docs/guide/octopus.svg,
// minified by ImageOptim: a run of pixels is a rect, or the path SVGO turns a
// rect into ("m6 0h2v2h-2z"), filled directly or from the group around it.
// Anything else in it fails here rather than being skipped, so a shape this
// cannot read never passes as a drawing it did not check.
func TestGuideOctopusIsTheLaunchMascot(t *testing.T) {
	b, err := os.ReadFile("../../docs/guide/octopus.svg")
	if err != nil {
		t.Fatalf("read docs/guide/octopus.svg: %v", err)
	}
	want := mascotGrid(mascotLaunch, mascotPose{look: -1}, false)
	w, h := len(want[0]), len(want)
	got := make([][]byte, h)
	for y := range got {
		got[y] = []byte(strings.Repeat(string(pxEmpty), w))
	}
	colour := map[string]byte{"#f00050": pxBody, "#00f0f0": pxLens}
	run := regexp.MustCompile(`^m(\d+) (\d+)h(\d+)v2h-(\d+)z$`)
	paint := func(x, y, width int, fill string) {
		c, ok := colour[strings.ToLower(fill)]
		if !ok {
			t.Fatalf("a run at %d,%d is filled %q: neither the body's nor the lens's colour", x, y, fill)
		}
		if y%2 != 0 || y/2 >= h || x < 0 || x+width > w {
			t.Fatalf("a run at %d,%d, %d wide, is off the %dx%d grid", x, y, width, w, h)
		}
		for i := x; i < x+width; i++ {
			got[y/2][i] = c
		}
	}

	dec := xml.NewDecoder(strings.NewReader(string(b)))
	var fills []string
	runs := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("parse octopus.svg: %v", err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			attr := func(name string) string {
				for _, a := range el.Attr {
					if a.Name.Local == name {
						return a.Value
					}
				}
				return ""
			}
			fill := attr("fill")
			if fill == "" && len(fills) > 0 {
				fill = fills[len(fills)-1]
			}
			fills = append(fills, fill)
			switch el.Name.Local {
			case "svg":
				if vb := attr("viewBox"); vb != fmt.Sprintf("0 0 %d %d", w, 2*h) {
					t.Errorf("viewBox %q, want 0 0 %d %d: a pixel is twice as tall as wide", vb, w, 2*h)
				}
			case "g":
			case "rect":
				x, _ := strconv.Atoi(attr("x"))
				y, _ := strconv.Atoi(attr("y"))
				width, _ := strconv.Atoi(attr("width"))
				if attr("height") != "2" {
					t.Fatalf("a rect at %s,%s is %s tall, not one pixel row", attr("x"), attr("y"), attr("height"))
				}
				paint(x, y, width, fill)
				runs++
			case "path":
				m := run.FindStringSubmatch(attr("d"))
				if m == nil || m[3] != m[4] {
					t.Fatalf("a path this cannot read as a run of pixels: %q", attr("d"))
				}
				x, _ := strconv.Atoi(m[1])
				y, _ := strconv.Atoi(m[2])
				width, _ := strconv.Atoi(m[3])
				paint(x, y, width, fill)
				runs++
			default:
				t.Fatalf("octopus.svg carries a <%s>, which this cannot check", el.Name.Local)
			}
		case xml.EndElement:
			fills = fills[:len(fills)-1]
		}
	}
	if runs == 0 {
		t.Fatal("octopus.svg draws nothing")
	}
	for y := range want {
		if string(got[y]) != string(want[y]) {
			t.Errorf("row %d differs from mascotLaunch looking left:\n got %s\nwant %s", y, got[y], want[y])
		}
	}
}
