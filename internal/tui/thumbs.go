package tui

import (
	"context"
	"image"
	"math"
	"time"

	"github.com/gdamore/tcell/v2"
	"golang.org/x/image/draw"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// maxThumbs is how many images the cache keeps before it starts over, so a long scroll doesn't hold every one.
const maxThumbs = 200

// thumbWidth is the widest a cached image is kept, plenty for a third of a terminal.
const thumbWidth = 640

// thumb is an image shown in a list, fetched once and kept small, with what it was drawn as at each size.
type thumb struct {
	img     image.Image
	err     error
	loading bool

	blocks   map[[2]int][][][2]tcell.Color
	graphics map[[2]int]*graphic
}

// thumbs fetches the images lists show, a few at a time, and keeps them. It's only used on the UI goroutine.
type thumbs struct {
	app     *App
	entries map[string]*thumb
	slots   chan struct{}
}

func newThumbs(app *App) *thumbs {
	return &thumbs{app: app, entries: map[string]*thumb{}, slots: make(chan struct{}, 4)}
}

// get returns the image at url, starting to fetch it the first time it's asked for.
func (t *thumbs) get(url string) *thumb {
	if th, ok := t.entries[url]; ok {
		return th
	}
	if len(t.entries) >= maxThumbs {
		t.entries = map[string]*thumb{}
	}

	th := &thumb{loading: true, blocks: map[[2]int][][][2]tcell.Color{}, graphics: map[[2]int]*graphic{}}
	t.entries[url] = th
	go func() {
		t.slots <- struct{}{}
		defer func() { <-t.slots }()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		img, err := fetchImage(ctx, imageClient, url)
		if err == nil {
			img = shrink(img, thumbWidth)
		}
		t.app.tv.QueueUpdateDraw(func() { th.img, th.err, th.loading = img, err, false })
	}()

	return th
}

// shrink scales img down to at most width pixels wide.
func shrink(img image.Image, width int) image.Image {
	b := img.Bounds()
	if b.Dx() <= width {
		return img
	}

	height := max(1, b.Dy()*width/b.Dx())
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.ApproxBiLinear.Scale(out, out.Bounds(), img, b, draw.Src, nil)

	return out
}

// cells is the image as half blocks, cols × rows, the top and bottom color of each.
func (th *thumb) cells(cols, rows int) [][][2]tcell.Color {
	key := [2]int{cols, rows}
	if c, ok := th.blocks[key]; ok {
		return c
	}

	scaled := image.NewNRGBA(image.Rect(0, 0, cols, rows*2))
	draw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), th.img, th.img.Bounds(), draw.Src, nil)
	cells := make([][][2]tcell.Color, rows)
	for y := range rows {
		cells[y] = make([][2]tcell.Color, cols)
		for x := range cols {
			cells[y][x] = [2]tcell.Color{pixel(scaled, x, y*2), pixel(scaled, x, y*2+1)}
		}
	}
	th.blocks[key] = cells

	return cells
}

// pixel is the color at x, y over the theme's background, since a terminal cell has no alpha.
func pixel(img *image.NRGBA, x, y int) tcell.Color {
	c := img.NRGBAAt(x, y)
	bg := tcell.GetColor(theme.shell)
	br, bgreen, bb := bg.RGB()
	a := int32(c.A)
	blend := func(v uint8, under int32) int32 { return (int32(v)*a + under*(255-a)) / 255 }

	return tcell.NewRGBColor(blend(c.R, br), blend(c.G, bgreen), blend(c.B, bb))
}

// imageBox is where one of a commit's images goes in its strip, in cells.
type imageBox struct {
	x, cols, rows int
}

// stripBoxes lays up to three images out side by side in width cells and rows rows, each as large as its shape
// lets it be, a cell being cellWidth × cellHeight pixels.
func stripBoxes(images []api.Image, width, rows, cellWidth, cellHeight int) []imageBox {
	n := min(len(images), 3)
	if n == 0 || width < n*3 {
		return nil
	}

	const gap = 1
	most := (width - gap*(n-1)) / n
	boxes := make([]imageBox, n)
	x := 0
	for i, img := range images[:n] {
		aspect := 16.0 / 9
		if img.Width > 0 && img.Height > 0 {
			aspect = float64(img.Width) / float64(img.Height)
		}

		cols := int(math.Round(float64(rows*cellHeight) * aspect / float64(cellWidth)))
		r := rows
		if cols > most {
			cols = most
			r = max(1, int(math.Round(float64(cols*cellWidth)/aspect/float64(cellHeight))))
		}
		boxes[i] = imageBox{x: x, cols: max(cols, 1), rows: r}
		x += boxes[i].cols + gap
	}

	return boxes
}

// drawImages draws a commit's images in its strip, whose top row is at top: in pixels when the terminal can and the
// whole strip is on screen with nothing over it, and in half blocks when not. Images still coming are boxes.
func (a *App) drawImages(screen tcell.Screen, images []api.Image, left, top, width, rows, clipY, clipHeight int) {
	gs, pixels := screen.(*graphicsScreen)
	pixels = pixels && gs.protocol != graphicsNone && !a.modalOpen() && top >= clipY && top+rows <= clipY+clipHeight

	// The boxes are laid out as the strip was, whether the images are drawn in pixels or blocks, so they don't jump
	// as they scroll off.
	visible := func(y int) bool { return y >= clipY && y < clipY+clipHeight }
	for i, box := range stripBoxes(images, width, rows, cellWidth, cellHeight) {
		x := left + box.x
		th := a.thumbs.get(images[i].URL)

		switch {
		case th.img == nil:
			mark := '▣'
			if th.err != nil {
				mark = '✕'
			}
			style := tcell.StyleDefault.Background(tcell.GetColor(colorWash)).Foreground(tcell.GetColor(colorFaint))
			for y := range box.rows {
				if !visible(top + y) {
					continue
				}
				for col := range box.cols {
					r := ' '
					if y == box.rows/2 && col == box.cols/2 {
						r = mark
					}
					screen.SetContent(x+col, top+y, r, nil, style)
				}
			}
		case pixels:
			cw, ch := gs.cellSize()
			pixelHeight := box.rows * ch
			if gs.protocol == graphicsSixel {
				// Sixel draws six rows of pixels at a time; a partial band would spill into the cell below.
				pixelHeight -= pixelHeight % 6
			}
			key := [2]int{box.cols, box.rows}
			g, ok := th.graphics[key]
			if !ok {
				g = gs.encode(th.img, box.cols*cw, pixelHeight)
				th.graphics[key] = g
			}
			gs.place(g, x, top, box.cols, box.rows)
		default:
			for y, row := range th.cells(box.cols, box.rows) {
				if !visible(top + y) {
					continue
				}
				for col, c := range row {
					screen.SetContent(x+col, top+y, '▀', nil, tcell.StyleDefault.Foreground(c[0]).Background(c[1]))
				}
			}
		}
	}
}
