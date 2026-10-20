package tui

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	_ "golang.org/x/image/webp"

	"github.com/eggplants/dev-null-tui/internal/api"
	"github.com/eggplants/dev-null-tui/internal/browser"
)

// imageViewer shows a commit's images one at a time, in pixels when the terminal can draw them and in block
// characters when it can't.
type imageViewer struct {
	*tview.Box

	app    *App
	images []api.Image
	index  int

	decoded map[int]image.Image
	failed  map[int]error

	// graphic is the shown image encoded for the terminal, at the size key says.
	graphic *graphic
	key     [3]int
	// blocks draws the image in block characters, for terminals without a graphics protocol.
	blocks      *tview.Image
	blocksImage image.Image
}

func newImageViewer(app *App, images []api.Image) *imageViewer {
	return &imageViewer{
		Box:     tview.NewBox(),
		app:     app,
		images:  images,
		decoded: map[int]image.Image{},
		failed:  map[int]error{},
		blocks:  tview.NewImage(),
	}
}

// Reload fetches the shown image unless it is already in.
func (v *imageViewer) Reload() {
	index := v.index
	if v.decoded[index] != nil {
		return
	}
	delete(v.failed, index)

	var img image.Image
	v.app.async(func(ctx context.Context) (err error) {
		img, err = fetchImage(ctx, v.app.client.HTTPClient, v.images[index].URL)
		return err
	}, func(err error) {
		if err != nil {
			v.failed[index] = err
			return
		}
		v.decoded[index] = img
	})
}

// fetchImage downloads and decodes an image; commit images are WebP, but any format Go reads will do.
func fetchImage(ctx context.Context, client *http.Client, url string) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching the image: %s", res.Status)
	}

	img, _, err := image.Decode(io.LimitReader(res.Body, 20<<20))
	if err != nil {
		return nil, fmt.Errorf("decoding the image: %w", err)
	}

	return img, nil
}

func (v *imageViewer) Draw(screen tcell.Screen) {
	v.Box.DrawForSubclass(screen, v)
	x, y, width, height := v.GetInnerRect()
	if width <= 0 || height <= 2 {
		return
	}

	img := v.images[v.index]
	title := fmt.Sprintf("[::b]Image %d/%d[::-] [%s]%d×%d · n/p switch · o open in browser · esc back[-]",
		v.index+1, len(v.images), colorMuted, img.Width, img.Height)
	tview.Print(screen, title, x, y, width, tview.AlignLeft, tcell.ColorDefault)
	y, height = y+2, height-2

	decoded := v.decoded[v.index]
	switch {
	case v.failed[v.index] != nil:
		tview.Print(screen, fmt.Sprintf("[red]%s[-]", tview.Escape(v.failed[v.index].Error())), x, y, width, tview.AlignLeft, tcell.ColorDefault)
		return
	case decoded == nil:
		tview.Print(screen, fmt.Sprintf("[%s]Loading…[-]", colorMuted), x, y, width, tview.AlignLeft, tcell.ColorDefault)
		return
	}

	// An image drawn in pixels would cover a modal on top of it, so it falls back to blocks while one is open.
	if gs, ok := screen.(*graphicsScreen); ok && gs.protocol != graphicsNone && !v.app.modalOpen() {
		v.drawPixels(gs, decoded, x, y, width, height)
		return
	}

	if v.blocksImage != decoded {
		v.blocks.SetImage(decoded)
		v.blocksImage = decoded
	}
	v.blocks.SetRect(x, y, width, height)
	v.blocks.Draw(screen)
}

// drawPixels fits the image into the rectangle, at most twice its size, and centers it.
func (v *imageViewer) drawPixels(gs *graphicsScreen, img image.Image, x, y, width, height int) {
	cellWidth, cellHeight := gs.cellSize()
	bounds := img.Bounds()
	scale := min(float64(width*cellWidth)/float64(bounds.Dx()), float64(height*cellHeight)/float64(bounds.Dy()), 2)
	pixelWidth, pixelHeight := max(int(float64(bounds.Dx())*scale), 1), max(int(float64(bounds.Dy())*scale), 1)
	if gs.protocol == graphicsSixel {
		// Sixel draws six rows of pixels at a time; a partial band would spill into the cell below.
		pixelHeight = max(pixelHeight-pixelHeight%6, 6)
	}

	cols, rows := (pixelWidth+cellWidth-1)/cellWidth, (pixelHeight+cellHeight-1)/cellHeight
	if key := [3]int{v.index, pixelWidth, pixelHeight}; v.graphic == nil || key != v.key {
		v.graphic, v.key = gs.encode(img, pixelWidth, pixelHeight), key
	}
	gs.place(v.graphic, x+(width-cols)/2, y+(height-rows)/2, cols, rows)
}

func (v *imageViewer) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return v.WrapInputHandler(func(event *tcell.EventKey, _ func(p tview.Primitive)) {
		switch {
		case event.Key() == tcell.KeyRight || event.Rune() == 'n':
			v.show(v.index + 1)
		case event.Key() == tcell.KeyLeft || event.Rune() == 'p':
			v.show(v.index - 1)
		case event.Rune() == 'o':
			if err := browser.Open(v.images[v.index].URL); err != nil {
				v.app.flashError(err)
			}
		}
	})
}

func (v *imageViewer) show(index int) {
	if index >= 0 && index < len(v.images) && index != v.index {
		v.index = index
		v.Reload()
	}
}
