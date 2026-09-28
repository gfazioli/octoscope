package main

import (
	"fmt"
	"math"
	"os"
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
	b, err := os.ReadFile("docs/index.html")
	if err != nil {
		t.Fatalf("read docs/index.html: %v", err)
	}
	return string(b)
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

// TestLandingHidesNothingBeforeTheScript holds the reveals to their one
// safety property: nothing is hidden until docs/landing.js has run and found
// it off screen. A scope is at REST in the served HTML, ARMED (data-armed)
// only once the script has measured it entirely below or above the
// viewport, and REVEALED on its way into view. So the markup must carry no
// data-armed or data-revealed of its own, and every rule that hides an item
// — the ones keyed on :not([data-revealed]) — must also require data-armed:
// a rule that hid on the absence of data-revealed alone would hide the page
// from every reader whose script never ran.
func TestLandingHidesNothingBeforeTheScript(t *testing.T) {
	page := readLanding(t)
	markup := regexp.MustCompile(`(?s)<style>.*?</style>|<script.*?</script>`).ReplaceAllString(page, "")
	if m := regexp.MustCompile(`<[^>]*\sdata-(armed|revealed)\b[^>]*>`).FindString(markup); m != "" {
		t.Errorf("the served markup carries a reveal state: %s", m)
	}

	css := styleBlock(t, page)
	hiding := 0
	for _, rule := range regexp.MustCompile(`(?s)([^{}]+)\{`).FindAllStringSubmatch(css, -1) {
		for _, sel := range strings.Split(rule[1], ",") {
			if !strings.Contains(sel, ":not([data-revealed])") {
				continue
			}
			hiding++
			if !strings.Contains(sel, "[data-armed]") {
				t.Errorf("selector hides without requiring data-armed: %q", strings.TrimSpace(sel))
			}
		}
	}
	if hiding == 0 {
		t.Error("found no hiding selectors at all — the check measured nothing")
	}
}

// TestLandingRevealVariantsHavePoses catches a data-reveal value with no
// starting pose, in the markup or set by the script: a typo there does not
// fail, the item just arrives with a fade and no motion.
func TestLandingRevealVariantsHavePoses(t *testing.T) {
	page := readLanding(t)
	script, err := os.ReadFile("docs/landing.js")
	if err != nil {
		t.Fatalf("read docs/landing.js: %v", err)
	}
	css := styleBlock(t, page)
	used := map[string]bool{}
	for _, m := range regexp.MustCompile(`data-reveal="([^"]+)"`).FindAllStringSubmatch(page, -1) {
		used[m[1]] = true
	}
	for _, m := range regexp.MustCompile(`setAttribute\('data-reveal', '([^']+)'\)`).FindAllStringSubmatch(string(script), -1) {
		used[m[1]] = true
	}
	if len(used) == 0 {
		t.Fatal("no data-reveal variant found in the markup or the script — the check measured nothing")
	}
	// Both forms: the item parked by a scope above it, and the item that is
	// its own scope.
	for v := range used {
		for _, sel := range []string{
			"[data-armed]:not([data-revealed]) [data-reveal='" + v + "']",
			"[data-reveal='" + v + "'][data-armed]:not([data-revealed])",
		} {
			if !strings.Contains(css, sel) {
				t.Errorf("data-reveal=%q has no armed pose in the stylesheet: missing %s", v, sel)
			}
		}
	}
}
