package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// The festive header: what a theme with Theme.Festive set adds to the
// top of the screen. A string of lights and falling snow fill the width
// beside the banner, the banner's frame turns candy cane, and the
// mascot wears Santa's trim. None of it has a timer of its own: the
// lights and the snow are a function of the model's clock, which the
// one-second tick that keeps the footer's "Updated Xs ago" current
// already advances, so a festive dashboard at rest costs what a plain
// one does.
//
// The decoration is in colour by nature, so a festive theme is never a
// monochromatic one; TestFestiveThemesAreNotMonochromatic keeps the two
// flags apart.

// mascotLaunchSanta and mascotMiniSanta are the two mascots as a festive
// theme draws them: white fur where the head begins, the tentacles as a
// white beard, and on the launch one the periscope's stem widened into
// the cone of a hat, so the lens is the bell at its tip. Same size, same
// eyes and same lens cells as the plain drawings — every pose and the
// whole choreography carry over untouched.
var mascotLaunchSanta = mascotArt{
	top: map[int][]string{
		-1: {"......oo####..........", "........######........"},
		0:  {"..........oo..........", "........######........"},
		1:  {"..........####oo......", "........######........"},
	},
	head: []string{
		".......wwwwwwww.......",
		".....wwwwwwwwwwww.....",
		"...################...",
		"..##################..",
		"..##################..",
		"..wwwwwwwwwwwwwwwwww..",
	},
	tent: map[bool][]string{
		false: {"...w..w..w..w..w..w...", "..w..w..w....w..w..w.."},
		true:  {"...w..w..w..w..w..w...", "....w..w..ww..w..w...."},
	},
	eyeX: mascotLaunch.eyeX,
	eyeY: mascotLaunch.eyeY,
}

var mascotMiniSanta = mascotArt{
	top: mascotMini.top,
	head: []string{
		"....wwwwwwwwww....",
		"..##############..",
		"..##############..",
		"..wwwwwwwwwwwwww..",
	},
	tent: map[bool][]string{
		false: {"..w.w.w.ww.w.w.w.."},
		true:  {"...w.w.w..w.w.w..."},
	},
	eyeX: mascotMini.eyeX,
	eyeY: mascotMini.eyeY,
}

// activeMascots returns the launch and the mini mascot the active theme
// draws: Santa's in a festive theme, the plain ones otherwise.
func activeMascots() (launch, mini mascotArt) {
	if isFestive() {
		return mascotLaunchSanta, mascotMiniSanta
	}
	return mascotLaunch, mascotMini
}

// festiveGreeting is the launch screen's tagline on the few days that
// have one of their own, and "" on every other day.
func festiveGreeting(now time.Time) string {
	switch month, day := now.Month(), now.Day(); {
	case month == time.December && day >= 24 && day <= 26:
		return "Merry Christmas!"
	case month == time.December && day == 31, month == time.January && day == 1:
		return "Happy New Year!"
	}
	return ""
}

// festiveBanner frames the banner's content like bannerStyle does — a
// rounded box, two cells of padding — with the border striped in the
// accent and snow: a candy cane. Same size as the plain banner, so the
// header's geometry does not depend on the theme.
func festiveBanner(content string) string {
	const pad = "  "
	last := lipgloss.Width(content) + 2*len(pad) + 1 // the right border's column
	// Stripes two cells wide, slanted by taking the row into account.
	stripe := func(x, y int, r string) string {
		if (x+y)/2%2 == 0 {
			return accentStyle.Render(r)
		}
		return festiveSnowStyle.Render(r)
	}
	edge := func(y int, left, right string) string {
		var b strings.Builder
		b.WriteString(stripe(0, y, left))
		for x := 1; x < last; x++ {
			b.WriteString(stripe(x, y, "─"))
		}
		b.WriteString(stripe(last, y, right))
		return b.String()
	}
	return edge(0, "╭", "╮") + "\n" +
		stripe(0, 1, "│") + pad + content + pad + stripe(last, 1, "│") + "\n" +
		edge(2, "╰", "╯")
}

const (
	// festiveGarlandMinWidth is the narrowest gap worth decorating:
	// two bulbs' worth.
	festiveGarlandMinWidth = 8
	// festiveBulbEvery is the distance, in cells, from one bulb on the
	// string to the next.
	festiveBulbEvery = 4
	// festiveChase is how many bulbs apart the dark ones are: one in
	// every festiveChase is out, and the dark spot moves a bulb a second.
	festiveChase = 4
	// festiveSnowPeriod is how many seconds pass between two flakes in
	// the same column. Longer than the snow is tall, or a column would
	// never be empty.
	festiveSnowPeriod = 5
	// festiveSnowLanes is how sparse the snow is: one column in this
	// many carries flakes at all.
	festiveSnowLanes = 2
)

// festiveFlakes are the snowflakes, by how a column's hash picks them:
// mostly specks, with the occasional crystal.
var festiveFlakes = []struct {
	glyph string
	frost bool // drawn fainter: further away
}{
	{"·", false}, {"*", false}, {"❅", false}, {"·", true}, {"❆", false}, {"*", true},
}

// festiveGarland draws the decoration beside the banner: w cells wide
// and rows tall, a string of lights on the first row and snow falling
// through the rest. tick is the clock in seconds; the same tick always
// draws the same picture.
func festiveGarland(w, rows int, tick int64) []string {
	out := make([]string, rows)
	for y := range out {
		if y == 0 {
			out[y] = festiveLights(w, tick)
		} else {
			out[y] = festiveSnow(w, y-1, tick)
		}
	}
	return out
}

// festiveLights is the string of lights: a wire with a bulb every
// festiveBulbEvery cells, the bulbs cycling through festiveBulbStyles.
// One bulb in every festiveChase is out — a ring instead of a disc —
// and the dark spot runs along the string, one bulb per tick.
func festiveLights(w int, tick int64) string {
	var b strings.Builder
	wire := 0
	flush := func() {
		if wire > 0 {
			b.WriteString(festiveWireStyle.Render(strings.Repeat("─", wire)))
			wire = 0
		}
	}
	for x := 0; x < w; x++ {
		if x%festiveBulbEvery != festiveBulbEvery/2 {
			wire++
			continue
		}
		flush()
		bulb := x / festiveBulbEvery
		glyph := "●"
		if (int64(bulb)-tick)%festiveChase == 0 {
			glyph = "○"
		}
		b.WriteString(festiveBulbStyles[bulb%len(festiveBulbStyles)].Render(glyph))
	}
	flush()
	return b.String()
}

// festiveSnow is one row of the snow, row 0 being the top one. Each
// column is a lane with a hash of its own: whether it snows there at
// all, when, and which flake. A flake enters at the top and is one row
// lower on every tick, so the rows read together as snow falling.
func festiveSnow(w, row int, tick int64) string {
	var b strings.Builder
	gap := 0
	for x := 0; x < w; x++ {
		lane := int64(uint32(x+1) * 2654435761 >> 16) // Knuth's multiplicative hash
		phase := (tick + lane/festiveSnowLanes) % festiveSnowPeriod
		if phase < 0 {
			phase += festiveSnowPeriod // a clock before 1970 still snows
		}
		if lane%festiveSnowLanes != 0 || phase != int64(row) {
			gap++
			continue
		}
		b.WriteString(strings.Repeat(" ", gap))
		gap = 0
		flake := festiveFlakes[lane/(festiveSnowLanes*festiveSnowPeriod)%int64(len(festiveFlakes))]
		if flake.frost {
			b.WriteString(festiveFrostStyle.Render(flake.glyph))
		} else {
			b.WriteString(festiveSnowStyle.Render(flake.glyph))
		}
	}
	b.WriteString(strings.Repeat(" ", gap))
	return b.String()
}
