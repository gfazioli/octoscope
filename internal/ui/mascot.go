package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// The launch mascot: a small octopus with a periscope — octo, scope —
// drawn in block characters and animated while the first dashboard
// fetch is in flight. It heads every screen shown before the first
// dashboard paint (loading, the sponsor splash, help, the rate-limit
// panel, a first-fetch error). The dashboard keeps its one-line
// banner, so the landing's screenshot geometry is untouched.
//
// The art is pixels, not characters. Each terminal cell holds a 2×2
// grid of them, drawn with the quadrant blocks (▘▝▖▗▚▞▛▜▙▟) plus
// ▀ ▄ ▌ ▐ █. A cell is about twice as tall as it is wide, and so is each
// pixel: that is why the rows below look squashed in source and come
// out round on screen, and why this works where the multi-line ASCII
// octagon renderBanner mentions did not — a character fills a whole
// cell, a pixel a quarter of one.
//
// Legend: '.' empty, '#' body (the theme's Accent), 'o' lens and eyes
// (the theme's Value). A cell can show two colours only as a
// foreground and a background, so no cell may mix '#', 'o' and '.';
// TestMascotEveryPoseIsWellFormed holds every pose to that.

const (
	mascotPxW = 22 // pixels across: 11 cells
	mascotPxH = 10 // pixels down: 5 rows

	pxEmpty = '.'
	pxBody  = '#'
	pxLens  = 'o'
)

// mascotPeriscope is the top two pixel rows, one pair per look
// direction: the stem sits on the centre cell and the lens turns with
// the eyes.
var mascotPeriscope = map[int][2]string{
	-1: {"......oo####..........", "..........##.........."},
	0:  {"..........oo..........", "..........##.........."},
	1:  {"..........####oo......", "..........##.........."},
}

// mascotHead is the six rows every pose shares. The eyes are drawn on
// top of it, on the cells at x 6-7 and 14-15 of rows 4-5.
var mascotHead = [6]string{
	".......########.......",
	".....############.....",
	"...################...",
	"..##################..",
	"..##################..",
	"...################...",
}

// mascotTentacles is the bottom two rows: six tentacles whose tips
// splay out (false) or curl in (true), alternating on every step.
var mascotTentacles = map[bool][2]string{
	false: {"...#..#..#..#..#..#...", "..#..#..#....#..#..#.."},
	true:  {"...#..#..#..#..#..#...", "....#..#..##..#..#...."},
}

// mascotEyeX is the left pixel column of each eye's cell.
var mascotEyeX = [2]int{6, 14}

// mascotPose is one frame of the mascot.
type mascotPose struct {
	look  int  // -1 left, 0 ahead, 1 right: the eyes and the periscope agree
	blink bool // lids half down
	curl  bool // tentacle tips curled in rather than splayed out
}

// mascotGrid composes a pose into pixels. silhouette draws the
// one-colour version: eyes as holes, lens as body. That is what a
// monochromatic theme gets (NO_COLOR included, since it forces
// `monochrome`): its Accent and Value are two shades of one hue — 252
// and 255 in `monochrome` — so a Value-coloured eye on an Accent body
// would all but vanish, while a hole reads in any palette.
func mascotGrid(p mascotPose, silhouette bool) [mascotPxH][mascotPxW]byte {
	var g [mascotPxH][mascotPxW]byte
	peri, tent := mascotPeriscope[p.look], mascotTentacles[p.curl]
	rows := make([]string, 0, mascotPxH)
	rows = append(rows, peri[:]...)
	rows = append(rows, mascotHead[:]...)
	rows = append(rows, tent[:]...)
	for y, r := range rows {
		copy(g[y][:], r)
	}

	for _, x := range mascotEyeX {
		switch {
		case silhouette && p.blink:
			g[5][x], g[5][x+1] = pxEmpty, pxEmpty
		case silhouette:
			// A hole one cell wide, shifted a pixel toward the look.
			for y := 4; y <= 5; y++ {
				g[y][x+p.look], g[y][x+p.look+1] = pxEmpty, pxEmpty
			}
		case p.blink:
			// Body-coloured lid over the lower half of the eye.
			g[5][x], g[5][x+1] = pxLens, pxLens
		default:
			// The eye is the whole cell; looking sideways leaves only
			// the half on that side lit.
			for y := 4; y <= 5; y++ {
				g[y][x], g[y][x+1] = pxLens, pxLens
				switch p.look {
				case -1:
					g[y][x+1] = pxEmpty
				case 1:
					g[y][x] = pxEmpty
				}
			}
		}
	}

	if silhouette {
		for y := range g {
			for x := range g[y] {
				if g[y][x] == pxLens {
					g[y][x] = pxBody
				}
			}
		}
	}
	return g
}

// mascotRole is which palette slot a cell's foreground or background
// takes.
type mascotRole uint8

const (
	roleNone mascotRole = iota
	roleBody
	roleLens
)

// mascotCell is one terminal cell of the mascot.
type mascotCell struct {
	r      rune
	fg, bg mascotRole
	// mixed is set when the cell's pixels mixed both colours with an
	// empty pixel, which one cell cannot show; the lens pixels were
	// drawn as body instead. No pose does this — the test keeps it so.
	mixed bool
}

// quadrantRunes maps a 2×2 pixel mask (top-left 8, top-right 4,
// bottom-left 2, bottom-right 1) to the block character drawing it.
var quadrantRunes = [16]rune{
	' ', '▗', '▖', '▄', '▝', '▐', '▞', '▟',
	'▘', '▚', '▌', '▙', '▀', '▜', '▛', '█',
}

// mascotCells turns a pose into rows of terminal cells.
func mascotCells(p mascotPose, silhouette bool) [][]mascotCell {
	g := mascotGrid(p, silhouette)
	weights := [4]int{8, 4, 2, 1}
	out := make([][]mascotCell, 0, mascotPxH/2)
	for y := 0; y < mascotPxH; y += 2 {
		row := make([]mascotCell, 0, mascotPxW/2)
		for x := 0; x < mascotPxW; x += 2 {
			px := [4]byte{g[y][x], g[y][x+1], g[y+1][x], g[y+1][x+1]}
			var body, lens, empty int
			for _, v := range px {
				switch v {
				case pxBody:
					body++
				case pxLens:
					lens++
				default:
					empty++
				}
			}
			c := mascotCell{}
			mask := func(want byte) int {
				n := 0
				for i, v := range px {
					if v == want {
						n += weights[i]
					}
				}
				return n
			}
			switch {
			case body == 0 && lens == 0:
				c.r = ' '
			case lens == 0:
				c.r, c.fg = quadrantRunes[mask(pxBody)], roleBody
			case body == 0:
				c.r, c.fg = quadrantRunes[mask(pxLens)], roleLens
			case empty == 0 && lens > body:
				c.r, c.fg, c.bg = quadrantRunes[mask(pxLens)], roleLens, roleBody
			case empty == 0:
				// Ties go to the body as foreground: the blink's lid and
				// the periscope's stem both read as body over lens.
				c.r, c.fg, c.bg = quadrantRunes[mask(pxBody)], roleBody, roleLens
			default:
				n := 0
				for i, v := range px {
					if v != pxEmpty {
						n += weights[i]
					}
				}
				c.r, c.fg, c.mixed = quadrantRunes[n], roleBody, true
			}
			row = append(row, c)
		}
		out = append(out, row)
	}
	return out
}

// mascotStyle is the style a cell's roles draw with. The styles live in
// styles.go with the rest of the palette.
func mascotStyle(fg, bg mascotRole) lipgloss.Style {
	switch {
	case fg == roleBody && bg == roleLens:
		return mascotBodyOnLensStyle
	case fg == roleLens && bg == roleBody:
		return mascotLensOnBodyStyle
	case fg == roleLens:
		return mascotLensStyle
	default:
		return mascotBodyStyle
	}
}

// renderMascot draws a pose as mascotPxH/2 lines of equal width,
// grouping runs of same-styled cells so each run is one styled span.
func renderMascot(p mascotPose, silhouette bool) []string {
	rows := mascotCells(p, silhouette)
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		var b strings.Builder
		for i := 0; i < len(row); {
			j := i
			var run strings.Builder
			for j < len(row) && row[j].fg == row[i].fg && row[j].bg == row[i].bg {
				run.WriteRune(row[j].r)
				j++
			}
			if row[i].fg == roleNone {
				b.WriteString(run.String())
			} else {
				b.WriteString(mascotStyle(row[i].fg, row[i].bg).Render(run.String()))
			}
			i = j
		}
		lines = append(lines, b.String())
	}
	return lines
}

// mascotStep is how long one pose holds. The mascot has no timer of its
// own: it advances on the loading spinner's accepted ticks (see the
// spinner.TickMsg case in Update), so it animates exactly while the
// spinner does and stops with it.
const mascotStep = 250 * time.Millisecond

// mascotChoreography is one loop, one entry per step: look ahead, look
// right, back, look left, back, blink. Three seconds at mascotStep.
var mascotChoreography = []struct {
	look  int
	blink bool
}{
	{0, false}, {0, false},
	{1, false}, {1, false}, {1, false},
	{0, false},
	{-1, false}, {-1, false}, {-1, false},
	{0, false},
	{0, true},
	{0, false},
}

// mascotPoseAt returns the pose for a step. The tentacles alternate on
// every step, independently of where the eyes are.
func mascotPoseAt(step int) mascotPose {
	if step < 0 {
		step = 0
	}
	c := mascotChoreography[step%len(mascotChoreography)]
	return mascotPose{look: c.look, blink: c.blink, curl: step%2 == 1}
}

// launchStep converts the count of accepted spinner ticks into a
// choreography step, from the spinner's own frame rate.
func (m Model) launchStep() int {
	per := 1
	if fps := m.spinner.Spinner.FPS; fps > 0 {
		per = max(1, int(mascotStep/fps))
	}
	return m.launchTicks / per
}

// launchStatus is the spinner line shown beside the mascot while the
// first fetch runs, and nothing otherwise.
func (m Model) launchStatus() string {
	if !m.loading || m.stats != nil {
		return ""
	}
	return m.spinner.View() + "  " + mutedStyle.Render("Loading…")
}

// launchTagline is the second line beside the mascot.
const launchTagline = "a terminal dashboard for GitHub"

// launchHeaderGap is the space between the mascot and its text.
const launchHeaderGap = "   "

// renderLaunchHeader heads every screen shown before the first
// dashboard paint: the mascot, and beside it the name and version, the
// tagline and status — the spinner line while the first fetch runs,
// or nothing. Where the terminal is too narrow for the pair it falls
// back to the dashboard's own one-line banner with status underneath,
// which is what these screens showed before the mascot existed.
func (m Model) renderLaunchHeader(available int, status string) string {
	name := boldStyle.Foreground(colAccent).Render("octoscope")
	if m.version != "" {
		name += mutedStyle.Render("  " + m.version)
	}
	text := []string{"", name, mutedStyle.Render(launchTagline)}
	if status != "" {
		text = append(text, status)
	}

	// Animated only while the first fetch runs; otherwise (a first-fetch
	// error, a splash left open after one) it rests on the first pose
	// rather than freezing mid-blink.
	step := 0
	if m.loading {
		step = m.launchStep()
	}
	art := renderMascot(mascotPoseAt(step), IsMonochromatic())
	need := mascotPxW/2 + len(launchHeaderGap)
	widest := 0
	for _, l := range text {
		if w := lipgloss.Width(l); w > widest {
			widest = w
		}
	}
	if need+widest > available {
		out := renderBanner(m.version)
		if status != "" {
			out += "\n\n" + status
		}
		return out
	}

	gap := make([]string, len(art))
	for i := range gap {
		gap[i] = launchHeaderGap
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		strings.Join(art, "\n"),
		strings.Join(gap, "\n"),
		strings.Join(text, "\n"),
	)
}
