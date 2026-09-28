package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// The mascot: a small octopus with a periscope — octo, scope — drawn in
// block characters, in two sizes. The launch mascot heads every screen
// shown before the first dashboard paint (loading, the sponsor splash,
// help, the rate-limit panel, a first-fetch error) and is animated
// while the first fetch is in flight. The mini mascot sits beside the
// dashboard's banner, the height of its box, rests facing ahead and
// looks around while a refresh is in flight.
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
// TestMascotEveryPoseIsWellFormed holds every pose of both sizes to that.

const (
	pxEmpty = '.'
	pxBody  = '#'
	pxLens  = 'o'
)

// mascotArt is one drawing of the mascot: the periscope rows by look
// direction, the head every pose shares, the tentacle rows by curl, and
// where the eyes are drawn on the head.
type mascotArt struct {
	top  map[int][]string  // -1 left, 0 ahead, 1 right: the lens turns with the eyes
	head []string          // the eyes are drawn over these rows
	tent map[bool][]string // tips splayed out (false) or curled in (true)
	eyeX [2]int            // left pixel column of each eye's cell
	eyeY int               // top pixel row of the eyes, counted from the top of the art
}

func (a mascotArt) width() int  { return len(a.head[0]) }
func (a mascotArt) height() int { return len(a.top[0]) + len(a.head) + len(a.tent[false]) }

// mascotLaunch heads the screens before the first dashboard paint:
// 22 × 10 pixels, 11 cells by 5 rows. The periscope's stem sits on the
// centre cell; six tentacles.
var mascotLaunch = mascotArt{
	top: map[int][]string{
		-1: {"......oo####..........", "..........##.........."},
		0:  {"..........oo..........", "..........##.........."},
		1:  {"..........####oo......", "..........##.........."},
	},
	head: []string{
		".......########.......",
		".....############.....",
		"...################...",
		"..##################..",
		"..##################..",
		"...################...",
	},
	tent: map[bool][]string{
		false: {"...#..#..#..#..#..#...", "..#..#..#....#..#..#.."},
		true:  {"...#..#..#..#..#..#...", "....#..#..##..#..#...."},
	},
	eyeX: [2]int{6, 14},
	eyeY: 4,
}

// mascotMini sits beside the dashboard's banner: 18 × 6 pixels, 9 cells
// by 3 rows, the height of the banner's box. There is no room for a
// stem, so the periscope lies along the top of its head.
var mascotMini = mascotArt{
	top: map[int][]string{
		-1: {"....oo####........"},
		0:  {"........oo........"},
		1:  {"........####oo...."},
	},
	head: []string{
		"....##########....",
		"..##############..",
		"..##############..",
		"..##############..",
	},
	tent: map[bool][]string{
		false: {"..#.#.#.##.#.#.#.."},
		true:  {"...#.#.#..#.#.#..."},
	},
	eyeX: [2]int{4, 12},
	eyeY: 2,
}

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
func mascotGrid(a mascotArt, p mascotPose, silhouette bool) [][]byte {
	rows := make([]string, 0, a.height())
	rows = append(rows, a.top[p.look]...)
	rows = append(rows, a.head...)
	rows = append(rows, a.tent[p.curl]...)
	g := make([][]byte, len(rows))
	for y, r := range rows {
		g[y] = []byte(r)
	}

	top, low := a.eyeY, a.eyeY+1
	for _, x := range a.eyeX {
		switch {
		case silhouette && p.blink:
			g[low][x], g[low][x+1] = pxEmpty, pxEmpty
		case silhouette:
			// A hole one cell wide, shifted a pixel toward the look.
			for _, y := range []int{top, low} {
				g[y][x+p.look], g[y][x+p.look+1] = pxEmpty, pxEmpty
			}
		case p.blink:
			// Body-coloured lid over the lower half of the eye.
			g[low][x], g[low][x+1] = pxLens, pxLens
		default:
			// The eye is the whole cell; looking sideways leaves only
			// the half on that side lit.
			for _, y := range []int{top, low} {
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
func mascotCells(a mascotArt, p mascotPose, silhouette bool) [][]mascotCell {
	g := mascotGrid(a, p, silhouette)
	weights := [4]int{8, 4, 2, 1}
	out := make([][]mascotCell, 0, len(g)/2)
	for y := 0; y+1 < len(g); y += 2 {
		row := make([]mascotCell, 0, a.width()/2)
		for x := 0; x+1 < a.width(); x += 2 {
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

// renderMascot draws a pose as lines of equal width, one per cell row,
// grouping runs of same-styled cells so each run is one styled span.
func renderMascot(a mascotArt, p mascotPose, silhouette bool) []string {
	rows := mascotCells(a, p, silhouette)
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
// spinner does — the first fetch and every refresh after it — and stops
// with it.
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

// mascotPoseNow is the pose both mascots draw: the choreography while a
// fetch is in flight, rest — facing ahead, eyes open — otherwise, rather
// than freezing mid-blink when the spinner stops.
func (m Model) mascotPoseNow() mascotPose {
	if !m.loading {
		return mascotPose{}
	}
	per := 1
	if fps := m.spinner.Spinner.FPS; fps > 0 {
		per = max(1, int(mascotStep/fps))
	}
	return mascotPoseAt(m.mascotTicks / per)
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

	art := renderMascot(mascotLaunch, m.mascotPoseNow(), IsMonochromatic())
	need := mascotLaunch.width()/2 + len(launchHeaderGap)
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

	return joinBeside(art, launchHeaderGap, text)
}

// headerMascotGap is the space between the mini mascot and the banner.
const headerMascotGap = "  "

// renderHeader is the dashboard's top line: the mini mascot and the
// banner beside it, the two the height of the banner's box, so nothing
// below moves. Where the pair does not fit, the banner stands alone.
func (m Model) renderHeader(available int) string {
	banner := renderBanner(m.version)
	if mascotMini.width()/2+len(headerMascotGap)+lipgloss.Width(banner) > available {
		return banner
	}
	art := renderMascot(mascotMini, m.mascotPoseNow(), IsMonochromatic())
	return joinBeside(art, headerMascotGap, strings.Split(banner, "\n"))
}

// joinBeside sets art and text side by side, top-aligned, with gap
// between them on every row the art covers.
func joinBeside(art []string, gap string, text []string) string {
	g := make([]string, len(art))
	for i := range g {
		g[i] = gap
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		strings.Join(art, "\n"),
		strings.Join(g, "\n"),
		strings.Join(text, "\n"),
	)
}
