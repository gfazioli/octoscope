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
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/html"
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

// pageOf parses a page the way a browser does: golang.org/x/net/html
// follows the HTML spec's tree construction, so tag and attribute names are
// lowercased, entities decoded, comments and script text are not elements,
// and a custom element such as <main-card> is not a <main>. Matching the
// source by hand got each of those wrong in review, one pass at a time.
// Scripting is off, as for a reader without JavaScript, so what a
// <noscript> holds is parsed into elements and checked like the rest.
func pageOf(t *testing.T, page string) *html.Node {
	t.Helper()
	doc, err := html.ParseWithOptions(strings.NewReader(page), html.ParseOptionEnableScripting(false))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return doc
}

// elements returns the HTML elements under n named name, or all of them for
// "", in document order. SVG and MathML elements are walked through but not
// returned, since HTML can sit inside them (an <svg> <foreignObject>). What
// sits inside a <template> is left out: a browser keeps it inert, so it is
// not part of the page.
func elements(n *html.Node, name string) []*html.Node {
	var found []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if c.Namespace == "" && (name == "" || c.Data == name) {
				found = append(found, c)
			}
			if c.Namespace != "" || c.Data != "template" {
				walk(c)
			}
		}
	}
	walk(n)
	return found
}

// attr returns an element's attribute, "" when it has none.
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == key {
			return a.Val
		}
	}
	return ""
}

// asciiLower lowercases ASCII letters only, as HTML does when it compares
// names and enumerated values: "LAZY" is lazy, "deſcription" is not a
// description.
func asciiLower(s string) string {
	return strings.Map(func(r rune) rune {
		if 'A' <= r && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}, s)
}

// tokens splits a space-separated attribute such as class or rel on ASCII
// whitespace only, as HTML does: a no-break space belongs to its token.
func tokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\f' || r == '\r'
	})
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
	for _, el := range elements(pageOf(t, page), "") {
		for _, a := range el.Attr {
			if a.Key == "data-armed" || a.Key == "data-revealed" {
				t.Errorf("the served markup carries a reveal state: <%s %s=%q>", el.Data, a.Key, a.Val)
			}
		}
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
	doc := pageOf(t, page)
	// The scripts that can set a variant: landing.js, and the page's own.
	scripts := readDocsFile(t, "docs/landing.js")
	for _, s := range elements(doc, "script") {
		if s.FirstChild != nil {
			scripts += "\n" + s.FirstChild.Data
		}
	}
	css := styleBlock(t, page)
	used := map[string]bool{}
	// Read off the elements, not the source: the stylesheet's own
	// [data-reveal='x'] would otherwise count as a use of every pose it has.
	for _, el := range elements(doc, "") {
		for _, a := range el.Attr {
			if a.Key == "data-reveal" {
				used[a.Val] = true
			}
		}
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

// landingImages returns every <img> of the landing.
func landingImages(t *testing.T) []*html.Node {
	t.Helper()
	imgs := elements(pageOf(t, readLanding(t)), "img")
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
	for _, img := range landingImages(t) {
		src := attr(img, "src")
		w, errW := strconv.Atoi(attr(img, "width"))
		h, errH := strconv.Atoi(attr(img, "height"))
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
	var shots []*html.Node
	for _, div := range elements(pageOf(t, readLanding(t)), "div") {
		if slices.Contains(tokens(attr(div, "class")), "theme-carousel-track") {
			shots = append(shots, elements(div, "img")...)
		}
	}
	if len(shots) < 2 {
		t.Fatalf("found %d carousel shots — the check would measure nothing", len(shots))
	}
	// An enumerated value: HTML reads "LAZY" as lazy.
	lazy := func(img *html.Node) bool { return asciiLower(attr(img, "loading")) == "lazy" }
	if lazy(shots[0]) {
		t.Errorf("the first shot is lazy, so the carousel would open on an empty frame: %s", attr(shots[0], "src"))
	}
	for _, shot := range shots[1:] {
		if !lazy(shot) {
			t.Errorf("a shot after the first loads with the page: %s", attr(shot, "src"))
		}
	}
}

// TestLandingEndsOnTheSupportCard holds the end of the page to the shape the
// maintainer's other product sites have, asked for on 2026-10-06: the
// sponsorship is a card in the footer, after its link row, and what
// octoscope cannot show comes before it rather than after. The octopus
// stands on that card (landing.js finds it by its id), and the nav's
// Sponsor link and the footer's both lead to it.
func TestLandingEndsOnTheSupportCard(t *testing.T) {
	doc := pageOf(t, readLanding(t))
	all := elements(doc, "")
	index := map[*html.Node]int{}
	for i, el := range all {
		index[el] = i
	}
	inside := func(el *html.Node, name string) *html.Node {
		for p := el.Parent; p != nil; p = p.Parent {
			if p.Type == html.ElementNode && p.Data == name {
				return p
			}
		}
		return nil
	}
	text := func(n *html.Node) string {
		var b strings.Builder
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.TextNode {
				b.WriteString(n.Data)
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(n)
		return strings.Join(strings.Fields(b.String()), " ")
	}

	var card, row, cantShow *html.Node
	for _, el := range all {
		if attr(el, "id") == "sponsor" {
			if card != nil {
				t.Fatal("two elements have id=\"sponsor\"")
			}
			card = el
		}
		if slices.Contains(tokens(attr(el, "class")), "footer-row") {
			row = el
		}
		if el.Data == "h2" && text(el) == "What octoscope can't show" {
			cantShow = el
		}
	}
	if card == nil || row == nil || cantShow == nil {
		t.Fatalf("missing a landmark: support card %v, footer row %v, can't-show heading %v", card != nil, row != nil, cantShow != nil)
	}
	footer := inside(card, "footer")
	if footer == nil {
		t.Fatal("the support card is not in the footer")
	}
	if !slices.Contains(tokens(attr(card, "class")), "support-card") {
		t.Errorf("#sponsor is not the support card: class=%q", attr(card, "class"))
	}
	if inside(row, "footer") != footer || index[row] > index[card] {
		t.Error("the support card does not follow the footer's link row")
	}
	if index[cantShow] > index[footer] {
		t.Error("\"What octoscope can't show\" comes after the support card")
	}
	links := 0
	for _, a := range elements(doc, "a") {
		if attr(a, "href") == "#sponsor" {
			links++
		}
	}
	if links < 2 {
		t.Errorf("found %d links to #sponsor, want the nav's and the footer's", links)
	}
}
