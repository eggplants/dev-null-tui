package tui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"net/url"
	"strings"

	"github.com/gdamore/tcell/v2"
	"golang.org/x/image/draw"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// avatarSize is how many pixels across GitHub is asked to send an avatar, enough for the profile's.
const avatarSize = 96

// avatarURL asks GitHub for an avatar no bigger than lists draw it.
func avatarURL(link string) string {
	u, err := url.Parse(link)
	if err != nil || !strings.HasSuffix(u.Host, "githubusercontent.com") {
		return link
	}
	q := u.Query()
	q.Set("s", fmt.Sprint(avatarSize))
	u.RawQuery = q.Encode()

	return u.String()
}

// drawAvatar draws a user's avatar over cols × rows cells at x, y: their picture in a ring of their lane when the
// terminal draws pixels and the whole of it is on screen with nothing over it, and else their initials on their
// lane, which is also what shows while the picture comes.
func (a *App) drawAvatar(screen tcell.Screen, u *api.User, x, y, cols, rows, clipY, clipHeight int) {
	if u == nil {
		return
	}

	gs, ok := screen.(*graphicsScreen)
	if ok && gs.protocol != graphicsNone && imageRows > 0 && u.AvatarURL != "" && !a.modalOpen() && y >= clipY && y+rows <= clipY+clipHeight {
		if th := a.thumbs.get(avatarURL(u.AvatarURL)); th.img != nil {
			cw, ch := gs.cellSize()
			ring := laneColor(u)
			key := fmt.Sprintf("%dx%d %s %s", cols, rows, ring, theme.shell)
			g, ok := th.avatars[key]
			if !ok {
				// Sixel has no transparency, so the corners are painted the background.
				g = gs.encode(roundAvatar(th.img, cols*cw, rows*ch, ring, gs.protocol == graphicsSixel), cols*cw, rows*ch)
				th.avatars[key] = g
			}
			gs.place(g, x, y, cols, rows)
			return
		}
	}

	drawInitials(screen, u, x, y, cols, rows, clipY, clipHeight)
}

// drawInitials draws a user's initials on their lane, as many as fit, in the middle row.
func drawInitials(screen tcell.Screen, u *api.User, x, y, cols, rows, clipY, clipHeight int) {
	style := tcell.StyleDefault.Background(tcell.GetColor(laneColor(u))).Foreground(tcell.GetColor(theme.popover)).Bold(true)
	letters := []rune(initials(u))
	letters = letters[:min(len(letters), cols)]
	start := (cols - len(letters)) / 2

	for row := range rows {
		if y+row < clipY || y+row >= clipY+clipHeight {
			continue
		}
		for col := range cols {
			r := ' '
			if row == (rows-1)/2 && col >= start && col-start < len(letters) {
				r = letters[col-start]
			}
			screen.SetContent(x+col, y+row, r, nil, style)
		}
	}
}

// roundAvatar crops a picture to a circle in a ring of its owner's lane, centered in width × height pixels. Outside
// the circle is clear, or the background when opaque.
func roundAvatar(img image.Image, width, height int, ring string, opaque bool) *image.NRGBA {
	size := min(width, height)
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	corner := image.Pt(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2)
	square := image.Rectangle{Min: corner, Max: corner.Add(image.Pt(side, side))}

	scaled := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), img, square, draw.Src, nil)

	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	if opaque {
		draw.Draw(out, out.Bounds(), image.NewUniform(hexColorRGBA(theme.shell)), image.Point{}, draw.Src)
	}

	ringColor := hexColorRGBA(ring)
	left, top := (width-size)/2, (height-size)/2
	radius := float64(size) / 2
	thickness := math.Max(1.5, float64(size)/16)
	for py := range size {
		for px := range size {
			d := math.Hypot(float64(px)+0.5-radius, float64(py)+0.5-radius)
			var c color.NRGBA
			switch {
			case d <= radius-thickness-0.5:
				c = scaled.NRGBAAt(px, py)
			case d <= radius-thickness+0.5:
				c = blend(scaled.NRGBAAt(px, py), ringColor, d-(radius-thickness-0.5))
			case d <= radius-0.5:
				c = ringColor
			case d <= radius+0.5:
				// The ring's outer edge fades out.
				c = ringColor
				c.A = uint8(float64(c.A) * (radius + 0.5 - d))
			default:
				continue
			}
			if opaque {
				c = blend(out.NRGBAAt(left+px, top+py), color.NRGBA{c.R, c.G, c.B, 255}, float64(c.A)/255)
			}
			out.SetNRGBA(left+px, top+py, c)
		}
	}

	return out
}

// blend mixes b into a by t, from 0 to 1.
func blend(a, b color.NRGBA, t float64) color.NRGBA {
	mixed := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t + 0.5) }

	return color.NRGBA{mixed(a.R, b.R), mixed(a.G, b.G), mixed(a.B, b.B), mixed(a.A, b.A)}
}

func hexColorRGBA(hex string) color.NRGBA {
	var r, g, b uint8
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return color.NRGBA{A: 255}
	}

	return color.NRGBA{r, g, b, 255}
}
