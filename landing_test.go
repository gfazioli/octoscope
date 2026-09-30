package main

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The landing's motion is hand-written static HTML, CSS and one script
// (docs/landing.js), with no build step to generate or check any of it.
// These tests are the check. Like version_test.go, they read docs/ straight
// off disk: main sits at the repo root, which is `go test`'s working
// directory for this package.

// landingSpring is one spring the landing runs. The promo films write every
// move as the closed-form step response of a damped spring — a frequency f
// in Hz and a damping ratio zeta — and CSS cannot run a spring, but
// `linear()` runs any curve sampled finely enough. So each spring is sampled
// here into the stops the stylesheet carries, with its duration: the time
// its envelope takes to fall under 0.4%. A curve and its duration only make
// sense as a pair, which is why neither is typed by hand.
type landingSpring struct {
	name    string // the custom property; its duration is <name>-duration
	f, zeta float64
	note    string // what it moves, printed above it
}

// landingSprings are the film's own springs, at the film's tempo — the same
// two findergit.app and lancetta.app run, sampled the same way (the sampler
// below reproduces findergit.app's generated block byte for byte, which is
// how it was checked).
var landingSprings = []landingSpring{
	{"--oc-spring", 1.4, 0.5, "a card landing: overshoots and settles"},
	{"--oc-spring-soft", 1.8, 0.75, "a heading rising: barely overshoots"},
}

// springAt is the film's spr: 0 at rest, 1 once settled, past 1 on the
// overshoot. It returns exactly 1 once the envelope is under 2e-5, so an
// end state is exact.
func springAt(t, f, zeta float64) float64 {
	if t <= 0 {
		return 0
	}
	w := 2 * math.Pi * f
	a := zeta * w
	env := math.Exp(-a * t)
	if env < 2e-5 {
		return 1
	}
	wd := w * math.Sqrt(1-zeta*zeta)
	return 1 - env*(math.Cos(wd*t)+(a/wd)*math.Sin(wd*t))
}

// springSettle is the seconds until the envelope is under 0.4%. Every
// landing spring is underdamped (zeta < 1), which is what this closed form
// needs.
func springSettle(f, zeta float64) float64 {
	return math.Log(250) / (zeta * 2 * math.Pi * f)
}

// trimFloat prints v to at most digits decimals with trailing zeros cut:
// 0.5 not 0.5000, 25 not 25.00.
func trimFloat(v float64, digits int) string {
	s := strconv.FormatFloat(v, 'f', digits, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
	}
	if s == "-0" {
		s = "0"
	}
	return s
}

// springsCSS is the block docs/index.html carries between its
// springs:begin and springs:end markers.
func springsCSS() string {
	const points = 48
	var lines []string
	for _, sp := range landingSprings {
		d := springSettle(sp.f, sp.zeta)
		peak := 0.0
		stops := make([]string, 0, points+1)
		for i := 0; i <= points; i++ {
			v := springAt(float64(i)/points*d, sp.f, sp.zeta)
			peak = math.Max(peak, v)
			switch i {
			case 0:
				stops = append(stops, "0")
			case points:
				stops = append(stops, "1")
			default:
				stops = append(stops, trimFloat(v, 4)+" "+trimFloat(float64(i)/points*100, 2)+"%")
			}
		}
		ms := int(math.Round(d * 1000))
		lines = append(lines,
			fmt.Sprintf("            /* f=%s Hz, zeta=%s: overshoot %s%%, %d ms -- %s */",
				trimFloat(sp.f, 2), strconv.FormatFloat(sp.zeta, 'f', -1, 64),
				trimFloat(math.Max(0, (peak-1)*100), 1), ms, sp.note),
			fmt.Sprintf("            %s-duration: %dms;", sp.name, ms),
			fmt.Sprintf("            %s: linear(", sp.name))
		for i, s := range stops {
			sep := ","
			if i == len(stops)-1 {
				sep = ""
			}
			lines = append(lines, "                "+s+sep)
		}
		lines = append(lines, "            );")
	}
	return strings.Join(lines, "\n")
}

func readLanding(t *testing.T) string {
	t.Helper()
	return readDocsFile(t, "docs/index.html")
}

// readDocsFile reads a file under docs/ with its line endings normalised.
// A Windows checkout converts them to CRLF (git's core.autocrlf), and the
// springs block is compared byte for byte: without this the Windows CI job
// found no springs:begin marker at all.
func readDocsFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// TestLandingSpringsAreGenerated fails when the stylesheet's springs are not
// exactly what springsCSS generates — a stop edited by hand, a spring added
// to one side only — and prints the block to paste.
func TestLandingSpringsAreGenerated(t *testing.T) {
	page := readLanding(t)
	const begin, end = "/* springs:begin */\n", "            /* springs:end */"
	i := strings.Index(page, begin)
	if i < 0 {
		t.Fatalf("docs/index.html has no springs:begin marker in :root")
	}
	i += len(begin)
	j := strings.Index(page[i:], end)
	if j < 0 {
		t.Fatalf("docs/index.html has no springs:end marker after springs:begin")
	}
	got := page[i : i+j]
	if want := springsCSS() + "\n"; got != want {
		t.Errorf("the springs in docs/index.html are not the generated ones; paste this between the markers:\n%s", want)
	}
}

// styleBlock returns the page's inline stylesheet.
func styleBlock(t *testing.T, page string) string {
	t.Helper()
	i := strings.Index(page, "<style>")
	j := strings.Index(page, "</style>")
	if i < 0 || j < i {
		t.Fatal("docs/index.html has no <style> block")
	}
	return page[i:j]
}

// markupOf is the page without its stylesheet, its scripts and its
// comments: what the parser turns into elements.
func markupOf(page string) string {
	return regexp.MustCompile(`(?s)<style>.*?</style>|<script.*?</script>|<!--.*?-->`).ReplaceAllString(page, "")
}

// cssRule is one rule of a stylesheet that carries declarations: its
// selector list, split, and its declaration block — at whatever depth of
// @media and @supports it sits.
type cssRule struct {
	selectors []string
	decls     string
}

// cssRules walks a stylesheet's blocks by brace depth. It relies on the
// page's CSS having no brace inside a string or a comment's neighbour; a
// stray one would misalign every rule after it and fail these tests
// loudly rather than let them pass.
func cssRules(css string) []cssRule {
	css = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, "")
	var rules []cssRule
	var preludes []string
	start := 0
	for i := 0; i < len(css); i++ {
		switch css[i] {
		case '{':
			prelude := css[start:i]
			// A statement at-rule (`@import …;`) can sit before a rule.
			if k := strings.LastIndex(prelude, ";"); k >= 0 {
				prelude = prelude[k+1:]
			}
			preludes = append(preludes, strings.TrimSpace(prelude))
			start = i + 1
		case '}':
			if n := len(preludes); n > 0 {
				if p := preludes[n-1]; !strings.HasPrefix(p, "@") {
					rules = append(rules, cssRule{selectors: splitSelectors(p), decls: css[start:i]})
				}
				preludes = preludes[:n-1]
			}
			start = i + 1
		}
	}
	return rules
}

// splitSelectors splits a selector list at its top-level commas only: a
// comma inside :not(…), :is(…) or an attribute value is part of one
// selector.
func splitSelectors(list string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(list); i++ {
		switch list[i] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(list[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(list[start:]))
}

// hides reports whether a declaration block, on its own, makes its
// element invisible: no opacity, no visibility, or no box.
func hides(decls string) bool {
	for _, d := range strings.Split(decls, ";") {
		d = strings.Join(strings.Fields(d), "")
		d = strings.TrimSuffix(d, "!important")
		if regexp.MustCompile(`^opacity:0*(\.0*)?%?$`).MatchString(d) ||
			d == "visibility:hidden" || d == "display:none" {
			return true
		}
	}
	return false
}

// TestLandingHidesNothingBeforeTheScript holds the reveals to their one
// safety property: nothing is hidden until docs/landing.js has run and found
// it off screen. A scope is at REST in the served HTML, ARMED (data-armed)
// only once the script has measured it entirely below or above the
// viewport, and REVEALED on its way into view. So the markup must carry no
// data-armed or data-revealed of its own, and every rule that hides a
// reveal element must require an armed, unrevealed scope: a rule that hid
// on data-reveal alone, or on the absence of data-revealed alone, would
// hide the page from every reader whose script never ran.
//
// Keyed on what a rule DOES rather than how its selector reads: every rule
// whose declarations hide, at any depth of @media or @supports, and whose
// selector names the reveal attributes, is checked — pseudo-elements
// aside, which carry decoration rather than content.
func TestLandingHidesNothingBeforeTheScript(t *testing.T) {
	page := readLanding(t)
	markup := markupOf(page)
	if loc := regexp.MustCompile(`\sdata-(armed|revealed)\b`).FindStringIndex(markup); loc != nil {
		from := max(0, loc[0]-80)
		t.Errorf("the served markup carries a reveal state: …%s…", markup[from:loc[1]])
	}

	reveal := regexp.MustCompile(`\[data-(reveal|scope|armed|revealed)\b`)
	// What a selector requires, rather than what it merely mentions: a
	// :not(…) group names what must be ABSENT, so `:not([data-armed])`
	// is the opposite of the requirement and has to be left out of it.
	negated := regexp.MustCompile(`:not\([^()]*\)`)
	hiding := 0
	for _, r := range cssRules(styleBlock(t, page)) {
		if !hides(r.decls) {
			continue
		}
		for _, sel := range r.selectors {
			if strings.Contains(sel, "::") || !reveal.MatchString(sel) {
				continue
			}
			hiding++
			required := negated.ReplaceAllString(sel, "")
			if !strings.Contains(required, "[data-armed]") || !strings.Contains(sel, ":not([data-revealed])") {
				t.Errorf("selector hides a reveal element without requiring an armed, unrevealed scope: %q", sel)
			}
		}
	}
	if hiding == 0 {
		t.Error("found no rule hiding a reveal element at all — the check measured nothing")
	}
}

// TestLandingRevealVariantsHavePoses catches a data-reveal value with no
// starting pose, in the markup or set by the script: a typo there does not
// fail, the item just arrives with a fade and no motion.
func TestLandingRevealVariantsHavePoses(t *testing.T) {
	page := readLanding(t)
	// The scripts that can set a variant: landing.js, and the page's own.
	scripts := readDocsFile(t, "docs/landing.js")
	for _, m := range regexp.MustCompile(`(?s)<script[^>]*>(.*?)</script>`).FindAllStringSubmatch(page, -1) {
		scripts += "\n" + m[1]
	}
	css := styleBlock(t, page)
	used := map[string]bool{}
	// Any quoting HTML allows, in the markup only — the stylesheet's own
	// [data-reveal='x'] would otherwise count as a use of every pose it has.
	for _, m := range regexp.MustCompile(`\sdata-reveal\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'<>=]+))`).FindAllStringSubmatch(markupOf(page), -1) {
		used[m[1]+m[2]+m[3]] = true
	}
	for _, m := range regexp.MustCompile(`setAttribute\(\s*["']data-reveal["']\s*,\s*["']([^"']+)["']\s*\)|dataset\.reveal\s*=\s*["']([^"']+)["']`).FindAllStringSubmatch(scripts, -1) {
		used[m[1]+m[2]] = true
	}
	if len(used) == 0 {
		t.Fatal("no data-reveal variant found in the markup or the script — the check measured nothing")
	}
	// Both forms: the item parked by a scope above it, and the item that is
	// its own scope — each in a rule that actually declares something, since
	// a selector over an empty block is no pose at all.
	rules := cssRules(css)
	posed := func(sel string) bool {
		for _, r := range rules {
			if !strings.Contains(r.decls, ":") {
				continue
			}
			for _, s := range r.selectors {
				if s == sel {
					return true
				}
			}
		}
		return false
	}
	for v := range used {
		for _, sel := range []string{
			"[data-armed]:not([data-revealed]) [data-reveal='" + v + "']",
			"[data-reveal='" + v + "'][data-armed]:not([data-revealed])",
		} {
			if !posed(sel) {
				t.Errorf("data-reveal=%q has no armed pose in the stylesheet: no rule declares anything for %s", v, sel)
			}
		}
	}
}

// imgAttrs returns the attributes of every <img> in the landing's markup.
func imgAttrs(t *testing.T) []map[string]string {
	t.Helper()
	attr := regexp.MustCompile(`([a-z-]+)\s*=\s*"([^"]*)"`)
	var imgs []map[string]string
	for _, tag := range regexp.MustCompile(`<img\b[^>]*>`).FindAllString(markupOf(readLanding(t)), -1) {
		a := map[string]string{}
		for _, m := range attr.FindAllStringSubmatch(tag, -1) {
			a[m[1]] = m[2]
		}
		imgs = append(imgs, a)
	}
	if len(imgs) == 0 {
		t.Fatal("found no <img> in docs/index.html — the check would measure nothing")
	}
	return imgs
}

// TestLandingImagesAreSized fails on an image the page cannot size before
// its bytes arrive. The logo carried no width or height: under a slow
// connection its first bytes landed after the first paint, and the hero
// below it jumped (CLS 0.178 in five of five Lighthouse runs on a slow
// phone, 0.036 with the size in the markup). A local image's attributes
// must be its real size, or they reserve the wrong box and the jump
// returns smaller.
func TestLandingImagesAreSized(t *testing.T) {
	for _, img := range imgAttrs(t) {
		src := img["src"]
		w, errW := strconv.Atoi(img["width"])
		h, errH := strconv.Atoi(img["height"])
		if errW != nil || errH != nil || w <= 0 || h <= 0 {
			t.Errorf("<img src=%q> has no numeric width and height", src)
			continue
		}
		if strings.Contains(src, "://") {
			continue
		}
		f, err := os.Open(filepath.Join("docs", filepath.FromSlash(src)))
		if err != nil {
			t.Errorf("<img src=%q>: %v", src, err)
			continue
		}
		cfg, _, err := image.DecodeConfig(f)
		f.Close()
		if err != nil {
			t.Errorf("<img src=%q>: %v", src, err)
			continue
		}
		if cfg.Width != w || cfg.Height != h {
			t.Errorf("<img src=%q> says %d×%d, the file is %d×%d", src, w, h, cfg.Width, cfg.Height)
		}
	}
}

// TestLandingCarouselShotsAreLazy fails when a carousel shot after the first
// is not lazy. The ten stills weigh about 5 MB and all of them loaded with
// the page; lazy, the page brings the first and the one or two the browser
// finds near it, and landing.js loads the rest a dwell ahead of their turn.
// The first must not be lazy, or the carousel opens on an empty frame.
func TestLandingCarouselShotsAreLazy(t *testing.T) {
	page := readLanding(t)
	i := strings.Index(page, `<div class="theme-carousel-track">`)
	j := strings.Index(page, `<div class="carousel-foot">`)
	if i < 0 || j < i {
		t.Fatal("docs/index.html has no carousel track before the carousel foot")
	}
	shots := regexp.MustCompile(`<img\b[^>]*>`).FindAllString(page[i:j], -1)
	if len(shots) < 2 {
		t.Fatalf("found %d carousel shots — the check would measure nothing", len(shots))
	}
	if strings.Contains(shots[0], `loading="lazy"`) {
		t.Errorf("the first shot is lazy, so the carousel would open on an empty frame: %s", shots[0])
	}
	for _, shot := range shots[1:] {
		if !strings.Contains(shot, `loading="lazy"`) {
			t.Errorf("a shot after the first loads with the page: %s", shot)
		}
	}
}
