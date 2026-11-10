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

	"github.com/charmbracelet/lipgloss"
)

// TestLandingOctopusIsTheLaunchMascot holds the octopus on the landing page
// to the one the TUI draws. The page carries its own copy of the art, as JSON
// that docs/landing.js composes into SVG, because the site is static files
// with no build step to generate it from mascot.go. A copy nothing checks is
// a copy that drifts: redraw the octopus here and the page would keep the old
// one, and nothing would say so. In season the page draws the festive one,
// from a second block held to mascotLaunchSanta the same way.
func TestLandingOctopusIsTheLaunchMascot(t *testing.T) {
	b, err := os.ReadFile("../../docs/index.html")
	if err != nil {
		t.Fatalf("read docs/index.html: %v", err)
	}
	for _, tc := range []struct {
		id   string
		name string
		art  mascotArt
	}{
		{"octopus-art", "mascotLaunch", mascotLaunch},
		{"octopus-art-santa", "mascotLaunchSanta", mascotLaunchSanta},
	} {
		t.Run(tc.id, func(t *testing.T) { checkSiteOctopus(t, string(b), tc.id, tc.name, tc.art) })
	}
}

// checkSiteOctopus compares the JSON block #id of the landing page with a.
func checkSiteOctopus(t *testing.T, page, id, name string, a mascotArt) {
	t.Helper()
	open := `<script type="application/json" id="` + id + `">`
	i := strings.Index(page, open)
	if i < 0 {
		t.Fatalf("docs/index.html has no #%s block", id)
	}
	j := strings.Index(page[i:], "</script>")
	if j < 0 {
		t.Fatalf("the #%s block is not closed", id)
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
		t.Fatalf("decode #%s: %v", id, err)
	}
	// One value and nothing after it: JSON.parse in landing.js rejects
	// trailing data, and would drop the octopus while this still passed.
	if tok, err := dec.Token(); err != io.EOF {
		t.Fatalf("#%s carries more after its one JSON value (%v, %v) — JSON.parse would reject the block", id, tok, err)
	}

	want := map[string][]string{"-1": a.top[-1], "0": a.top[0], "1": a.top[1]}
	if !reflect.DeepEqual(site.Top, want) {
		t.Errorf("periscope rows differ from %s.top:\n got %q\nwant %q", name, site.Top, want)
	}
	if !reflect.DeepEqual(site.Head, a.head) {
		t.Errorf("head rows differ from %s.head:\n got %q\nwant %q", name, site.Head, a.head)
	}
	tent := map[string][]string{"splayed": a.tent[false], "curled": a.tent[true]}
	if !reflect.DeepEqual(site.Tent, tent) {
		t.Errorf("tentacle rows differ from %s.tent:\n got %q\nwant %q", name, site.Tent, tent)
	}
	if site.EyeX != a.eyeX || site.EyeY != a.eyeY {
		t.Errorf("eyes at x %v y %d, %s has x %v y %d", site.EyeX, site.EyeY, name, a.eyeX, a.eyeY)
	}
}

// TestLandingSeasonIsTheAppsSeason holds the stretch the landing page
// dresses up for to the one the christmas theme is in season: the script
// in the page's <head> reads its window from three attributes on <html>,
// and they are seasons' only entry, the days in yearDay.ordinal's own
// encoding. Move the season in seasonal.go and the page would keep the old
// dates, and nothing would say so.
func TestLandingSeasonIsTheAppsSeason(t *testing.T) {
	b, err := os.ReadFile("../../docs/index.html")
	if err != nil {
		t.Fatalf("read docs/index.html: %v", err)
	}
	tag := regexp.MustCompile(`<html [^>]*>`).FindString(string(b))
	if tag == "" {
		t.Fatal("docs/index.html has no <html …> tag with attributes")
	}
	attr := func(name string) string {
		m := regexp.MustCompile(` ` + name + `="([^"]*)"`).FindStringSubmatch(tag)
		if m == nil {
			t.Fatalf("<html> has no %s attribute: %s", name, tag)
		}
		return m[1]
	}
	if len(seasons) != 1 {
		t.Fatalf("the page carries one season, the app has %d: the page's script needs teaching", len(seasons))
	}
	s := seasons[0]
	if got := attr("data-season-name"); got != s.theme {
		t.Errorf("data-season-name %q, the app's season is %q", got, s.theme)
	}
	if got, want := attr("data-season-from"), strconv.Itoa(s.from.ordinal()); got != want {
		t.Errorf("data-season-from %q, want %s (%s)", got, want, s.from)
	}
	if got, want := attr("data-season-to"), strconv.Itoa(s.to.ordinal()); got != want {
		t.Errorf("data-season-to %q, want %s (%s)", got, want, s.to)
	}
	// The served page is out of season: the script decides, per reader.
	if strings.Contains(tag, ` data-season="`) {
		t.Errorf("the served <html> already carries data-season: %s", tag)
	}
	// And the stylesheet and the script hang on the very name the
	// attribute carries.
	for _, want := range []string{
		`html[data-season="` + s.theme + `"] .site-nav::before`,
		`root.setAttribute('data-season', name)`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("docs/index.html lost %q", want)
		}
	}
	js, err := os.ReadFile("../../docs/landing.js")
	if err != nil {
		t.Fatalf("read docs/landing.js: %v", err)
	}
	if !strings.Contains(string(js), `season === '`+s.theme+`'`) {
		t.Errorf("docs/landing.js no longer tests for the %q season", s.theme)
	}
}

// TestLandingGarlandIsTheApps holds what the landing page hangs in season
// to what the app draws beside its banner. The page draws it itself, in its
// stylesheet and in docs/landing.js, on the same grid of cells: a wire with
// a bulb in every festiveBulbEvery-th cell, the bulbs in festiveBulbStyles'
// colours and order, one in festiveChase out and the dark spot moving a
// bulb a second; and for snow the app's own flakes as text, less the ones
// outside the Latin range the site's own fonts carry (fonts/fonts.css),
// which a system without a fallback would draw as boxes across the page.
func TestLandingGarlandIsTheApps(t *testing.T) {
	b, err := os.ReadFile("../../docs/index.html")
	if err != nil {
		t.Fatalf("read docs/index.html: %v", err)
	}
	page := string(b)
	theme := seasons[0].theme
	// The rule that draws: the selector also heads the rule the two layers
	// share and the one that stops the chase under Reduce Motion.
	rule := func(selector string) string {
		t.Helper()
		rest := page
		for {
			var ok bool
			if _, rest, ok = strings.Cut(rest, selector+" {"); !ok {
				t.Fatalf("docs/index.html has no rule %q with a background-image", selector)
			}
			if body, _, _ := strings.Cut(rest, "}"); strings.Contains(body, "background-image") {
				return body
			}
		}
	}
	lights := rule(`html[data-season="` + theme + `"] .site-nav::before`)
	chase := rule(`html[data-season="` + theme + `"] .site-nav::after`)

	// The colours: the bulbs' in order, then the wire's.
	if err := applyTheme(theme, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applyTheme(defaultTheme, "") })
	var want []string
	for _, s := range append(append([]lipgloss.Style{}, festiveBulbStyles...), festiveWireStyle) {
		want = append(want, strings.ToUpper(string(s.GetForeground().(lipgloss.Color))))
	}
	// A gradient names its colour at every stop: a repeat is one colour.
	var got []string
	for _, c := range regexp.MustCompile(`#[0-9A-Fa-f]{6}`).FindAllString(lights, -1) {
		if c = strings.ToUpper(c); len(got) == 0 || got[len(got)-1] != c {
			got = append(got, c)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the lights' colours are %v, the app's bulbs and wire are %v", got, want)
	}

	// The grid: a cell is 10px on the page.
	const cell = 10
	var centres []string
	for _, m := range regexp.MustCompile(`circle at ([0-9.]+)px`).FindAllStringSubmatch(lights, -1) {
		centres = append(centres, m[1])
	}
	var wantCentres []string
	for i := range festiveBulbStyles {
		// The middle of the bulb's cell: festiveLights puts a bulb where
		// x%festiveBulbEvery is festiveBulbEvery/2.
		wantCentres = append(wantCentres, strconv.Itoa((i*festiveBulbEvery+festiveBulbEvery/2)*cell+cell/2))
	}
	if !reflect.DeepEqual(centres, wantCentres) {
		t.Errorf("the bulbs sit at %v px, want %v: one every %d cells of %dpx", centres, wantCentres, festiveBulbEvery, cell)
	}
	tile := festiveChase * festiveBulbEvery * cell
	for _, want := range []string{
		fmt.Sprintf("left: -%dpx;", tile),
		fmt.Sprintf("background-size: %dpx ", tile),
		fmt.Sprintf("animation: season-chase %ds steps(%d) infinite;", festiveChase, festiveChase),
	} {
		if !strings.Contains(chase, want) {
			t.Errorf("the dark spot's rule lost %q:\n%s", want, chase)
		}
	}
	if want := fmt.Sprintf("@keyframes season-chase { to { transform: translateX(%dpx); } }", tile); !strings.Contains(page, want) {
		t.Errorf("docs/index.html lost %q", want)
	}

	// The snow.
	js, err := os.ReadFile("../../docs/landing.js")
	if err != nil {
		t.Fatalf("read docs/landing.js: %v", err)
	}
	list := regexp.MustCompile(`var FLAKES = \[(.*)\];`).FindStringSubmatch(string(js))
	if list == nil {
		t.Fatal("docs/landing.js has no FLAKES list")
	}
	var flakes, wantFlakes []string
	for _, m := range regexp.MustCompile(`\['([^']*)', ([01])\]`).FindAllStringSubmatch(list[1], -1) {
		flakes = append(flakes, m[1]+" faint="+m[2])
	}
	for _, f := range festiveFlakes {
		if r := []rune(f.glyph); len(r) != 1 || r[0] > 0xFF {
			continue
		}
		faint := "0"
		if f.frost {
			faint = "1"
		}
		wantFlakes = append(wantFlakes, f.glyph+" faint="+faint)
	}
	if !reflect.DeepEqual(flakes, wantFlakes) {
		t.Errorf("the page's flakes are %q, the app's Latin ones are %q", flakes, wantFlakes)
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
