package tui

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// palette is a set of the web's colors, from its DESIGN.md and app.css.
type palette struct {
	dark bool

	shell       string
	popover     string
	ink         string
	muted       string
	faint       string
	hairline    string
	rail        string
	wash        string
	primary     string
	primarySoft string
	destructive string
	brand       string
	lanes       [6]string
}

var (
	darkPalette = palette{
		dark:        true,
		shell:       "#111112",
		popover:     "#1f1f22",
		ink:         "#ececec",
		muted:       "#a1a1a6",
		faint:       "#6e6e73",
		hairline:    "#2a2a2d",
		rail:        "#36363a",
		wash:        "#232326",
		primary:     "#7aa2f7",
		primarySoft: "#18233a",
		destructive: "#ff6b6b",
		brand:       "#ff2d20",
		lanes:       [6]string{"#ff8a4c", "#51cf66", "#da77f2", "#3bc9db", "#f06595", "#fcc419"},
	}
	lightPalette = palette{
		shell:       "#f6f7f8",
		popover:     "#ffffff",
		ink:         "#121417",
		muted:       "#5b616b",
		faint:       "#8b919a",
		hairline:    "#e1e4e8",
		rail:        "#d3d7dd",
		wash:        "#eef0f2",
		primary:     "#3b6fe0",
		primarySoft: "#e8effc",
		destructive: "#c92a2a",
		brand:       "#ff2d20",
		lanes:       [6]string{"#d9480f", "#2f9e44", "#9c36b5", "#0c8599", "#c2255c", "#b7791f"},
	}
)

// theme is the palette the TUI draws with. Set it with applyTheme.
var theme = darkPalette

// The colors as tview's style tags take them, kept apart from theme so a tag reads like what it means.
var (
	colorInk     string
	colorMuted   string
	colorFaint   string
	colorRail    string
	colorWash    string
	colorPrimary string
	colorDanger  string
	colorWarning string
	colorSuccess string
)

func init() { applyTheme(darkPalette) }

// applyTheme makes p the palette, including for tview's own primitives: borders, modals and inputs.
func applyTheme(p palette) {
	theme = p

	colorInk, colorMuted, colorFaint = p.ink, p.muted, p.faint
	colorRail, colorWash = p.rail, p.wash
	colorPrimary, colorDanger = p.primary, p.destructive
	colorWarning, colorSuccess = p.lanes[5], p.lanes[1]

	tview.Styles = tview.Theme{
		PrimitiveBackgroundColor:    tcell.GetColor(p.shell),
		ContrastBackgroundColor:     tcell.GetColor(p.wash),
		MoreContrastBackgroundColor: tcell.GetColor(p.primary),
		BorderColor:                 tcell.GetColor(p.rail),
		TitleColor:                  tcell.GetColor(p.ink),
		GraphicsColor:               tcell.GetColor(p.rail),
		PrimaryTextColor:            tcell.GetColor(p.ink),
		SecondaryTextColor:          tcell.GetColor(p.primary),
		TertiaryTextColor:           tcell.GetColor(p.lanes[1]),
		InverseTextColor:            tcell.GetColor(p.popover),
		ContrastSecondaryTextColor:  tcell.GetColor(p.muted),
	}
	tview.Borders.Horizontal, tview.Borders.Vertical = '─', '│'
	tview.Borders.TopLeft, tview.Borders.TopRight = '╭', '╮'
	tview.Borders.BottomLeft, tview.Borders.BottomRight = '╰', '╯'
	tview.Borders.HorizontalFocus, tview.Borders.VerticalFocus = '─', '│'
	tview.Borders.TopLeftFocus, tview.Borders.TopRightFocus = '╭', '╮'
	tview.Borders.BottomLeftFocus, tview.Borders.BottomRightFocus = '╰', '╯'
}

// pickTheme chooses the palette: name, when it's dark or light, or else the one that suits the terminal's
// background, which answer (the terminal's answers to probeQuery) may tell. Without an answer it stays dark.
func pickTheme(name string, answer []byte) palette {
	switch name {
	case "dark":
		return darkPalette
	case "light":
		return lightPalette
	}

	if r, g, b, ok := parseBackground(answer); ok && luminance(r, g, b) > 0.5 {
		return lightPalette
	}

	return darkPalette
}

// parseBackground reads the answer to OSC 11, ESC ] 11 ; rgb:RRRR/GGGG/BBBB, which has 1 to 4 hex digits a channel.
func parseBackground(answer []byte) (r, g, b float64, ok bool) {
	i := bytes.Index(answer, []byte("\x1b]11;rgb:"))
	if i < 0 {
		return 0, 0, 0, false
	}
	rest := answer[i+len("\x1b]11;rgb:"):]
	if end := bytes.IndexAny(rest, "\x07\x1b"); end >= 0 {
		rest = rest[:end]
	}

	parts := bytes.Split(rest, []byte("/"))
	if len(parts) != 3 {
		return 0, 0, 0, false
	}

	var channels [3]float64
	for j, part := range parts {
		if len(part) == 0 || len(part) > 4 {
			return 0, 0, 0, false
		}
		v, err := strconv.ParseUint(string(part), 16, 16)
		if err != nil {
			return 0, 0, 0, false
		}
		channels[j] = float64(v) / float64(uint64(1)<<(4*len(part))-1)
	}

	return channels[0], channels[1], channels[2], true
}

// luminance is a color's relative luminance, for channels from 0 to 1, close enough to tell light from dark.
func luminance(r, g, b float64) float64 {
	return 0.2126*r + 0.7152*g + 0.0722*b
}

// hexLuminance is the luminance of "#rrggbb".
func hexLuminance(hex string) float64 {
	var r, g, b uint8
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return 0
	}

	return luminance(float64(r)/255, float64(g)/255, float64(b)/255)
}

// mix blends a color into another, as CSS's color-mix does, weight being how much of a there is.
func mix(a, b string, weight float64) string {
	var ar, ag, ab, br, bg, bb uint8
	if _, err := fmt.Sscanf(a, "#%02x%02x%02x", &ar, &ag, &ab); err != nil {
		return a
	}
	if _, err := fmt.Sscanf(b, "#%02x%02x%02x", &br, &bg, &bb); err != nil {
		return a
	}

	channel := func(x, y uint8) uint8 { return uint8(float64(x)*weight + float64(y)*(1-weight) + 0.5) }

	return fmt.Sprintf("#%02x%02x%02x", channel(ar, br), channel(ag, bg), channel(ab, bb))
}

// readable tells whether a custom lane color stands out from the theme's background, which a color picked on the
// other theme may not.
func readable(hex string) bool {
	l := hexLuminance(hex)
	if theme.dark {
		return l >= 0.12
	}

	return l <= 0.7
}
