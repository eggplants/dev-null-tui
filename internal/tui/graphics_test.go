package tui

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/eggplants/dev-null-tui/internal/api"
)

func TestHasSixel(t *testing.T) {
	for answer, want := range map[string]bool{
		"\x1b[?62;4;6;22c":            true,
		"\x1b[?65;1;9c":               false,
		"\x1b[4;600;800t\x1b[?62;4c":  true,
		"\x1b_Gi=31;OK\x1b\\\x1b[?1c": false,
		"":                            false,
	} {
		if got := hasSixel([]byte(answer)); got != want {
			t.Errorf("hasSixel(%q) = %v", answer, got)
		}
	}
}

// fakeTty records what is written to the terminal behind a simulation screen.
type fakeTty struct {
	tcell.Tty
	mu  sync.Mutex
	out bytes.Buffer
}

func (f *fakeTty) Write(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.out.Write(b)
}

func (f *fakeTty) WindowSize() (tcell.WindowSize, error) {
	return tcell.WindowSize{Width: 20, Height: 10, PixelWidth: 200, PixelHeight: 200}, nil
}

func (f *fakeTty) take() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	defer f.out.Reset()

	return f.out.String()
}

type ttyScreen struct {
	tcell.SimulationScreen
	tty *fakeTty
}

func (s ttyScreen) Tty() (tcell.Tty, bool) { return s.tty, true }

func newTestGraphicsScreen(t *testing.T, protocol graphicsProtocol) (*graphicsScreen, tcell.SimulationScreen, *fakeTty) {
	t.Helper()

	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(20, 10)
	tty := &fakeTty{}

	return newGraphicsScreen(ttyScreen{sim, tty}, protocol, 0, 0), sim, tty
}

func shownRune(sim tcell.SimulationScreen, x, y int) rune {
	cells, width, _ := sim.GetContents()
	if runes := cells[y*width+x].Runes; len(runes) > 0 {
		return runes[0]
	}

	return ' '
}

func TestGraphicsScreenDrawsImagesOverLockedCells(t *testing.T) {
	gs, sim, tty := newTestGraphicsScreen(t, graphicsKitty)
	if w, h := gs.cellSize(); w != 10 || h != 20 {
		t.Fatalf("cellSize() = %d, %d", w, h)
	}

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	g := gs.encode(img, 20, 40)
	if !strings.HasPrefix(string(g.data), "\x1b_Ga=T,f=100,q=2,C=1,i=1,m=0;") {
		t.Fatalf("encoded as %q", g.data[:40])
	}

	frame := func(text rune, placed bool) {
		gs.Clear()
		gs.SetContent(3, 2, text, nil, tcell.StyleDefault)
		if placed {
			gs.place(g, 2, 1, 2, 2)
		}
		gs.Show()
	}

	frame(' ', true)
	if out := tty.take(); !strings.HasPrefix(out, "\x1b7\x1b[2;3H\x1b_Ga=T") || !strings.HasSuffix(out, "\x1b8") {
		t.Errorf("the first frame wrote %q", out)
	}

	// The same image in the same place isn't sent again, and tcell leaves its cells alone.
	frame('x', true)
	if out := tty.take(); out != "" {
		t.Errorf("an unchanged frame wrote %q", out)
	}
	if r := shownRune(sim, 3, 2); r != ' ' {
		t.Errorf("tcell drew %q under the image", r)
	}

	// Once the image is gone the terminal is told to drop it and the cells are drawn again.
	frame('x', false)
	if out := tty.take(); out != "\x1b_Ga=d,d=I,q=2,i=1\x1b\\" {
		t.Errorf("taking the image away wrote %q", out)
	}
	if r := shownRune(sim, 3, 2); r != 'x' {
		t.Errorf("the cell under the image shows %q", r)
	}
}

func TestGraphicsScreenEncodesSixel(t *testing.T) {
	gs, _, _ := newTestGraphicsScreen(t, graphicsSixel)

	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	g := gs.encode(img, 12, 6)
	if s := string(g.data); !strings.HasPrefix(s, "\x1bP0;0;8q\"1;1;12;6") || !strings.HasSuffix(s, "\x1b\\") {
		t.Errorf("encoded as %q", s)
	}
}

// imageAPI serves a log of one commit carrying two images, and the images.
func imageAPI(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/img.png":
		_ = png.Encode(w, image.NewGray(image.Rect(0, 0, 8, 4)))
		return
	case "/missing.png":
		http.NotFound(w, r)
		return
	}

	alice := map[string]any{"id": "u1", "username": "alice", "name": "Alice"}
	img := map[string]any{"id": "i1", "url": "http://" + r.Host + "/img.png", "width": 8, "height": 4}
	missing := map[string]any{"id": "i2", "url": "http://" + r.Host + "/missing.png", "width": 8, "height": 4}

	var out any
	switch r.URL.Path {
	case "/api/v1/me":
		out = map[string]any{"data": alice}
	case "/api/v1/log/following":
		out = map[string]any{"data": []any{map[string]any{
			"id": "c1", "short_hash": "abc1234", "body": "look", "status": "approved", "author": alice,
			"images": []any{img, missing},
		}}}
	default:
		out = map[string]any{"data": []any{}}
	}
	_ = json.NewEncoder(w).Encode(out)
}

func TestViewsACommitsImages(t *testing.T) {
	h := start(t, http.HandlerFunc(imageAPI))

	h.waitFor("2 images · 8×4 · i to view")
	h.typeText("i")
	h.waitFor("Image 1/2")
	// The simulation screen has no graphics protocol, so the image is drawn in blocks.
	h.waitForGone("Reading from /dev/null")

	h.typeText("n")
	h.waitFor("Image 2/2")
	h.waitFor("fetching the image: 404 Not Found")

	h.key(tcell.KeyEscape)
	h.waitFor("2 images · 8×4 · i to view")
}

func TestShowsImagesInTheList(t *testing.T) {
	h := start(t, http.HandlerFunc(imageAPI))
	h.waitFor("2 images · 8×4 · i to view")

	// The simulation screen has no graphics protocol, so the first image is drawn in half blocks, and the one that
	// isn't there is marked.
	h.waitUntil(func(screen string) bool {
		rows := strings.Split(screen, "\n")
		return len(rows) > 3 && strings.Contains(strings.Join(rows[3:], "\n"), "▀▀▀") && strings.Contains(screen, "✕")
	}, "draw the images")
}

func TestStripBoxesFitTheirImages(t *testing.T) {
	wide := api.Image{Width: 1600, Height: 900}
	tall := api.Image{Width: 600, Height: 1200}

	// At 1×2 a cell, 8 rows of a 16:9 image are 28 cells wide, and a tall one 8.
	boxes := stripBoxes([]api.Image{wide, tall}, 80, 8, 1, 2)
	if len(boxes) != 2 || boxes[0] != (imageBox{x: 0, cols: 28, rows: 8}) || boxes[1] != (imageBox{x: 29, cols: 8, rows: 8}) {
		t.Errorf("boxes = %+v", boxes)
	}

	// Three wide images in 40 cells are cut to 12 cells each, and shorter to keep their shape.
	for _, b := range stripBoxes([]api.Image{wide, wide, wide, wide}, 40, 8, 1, 2) {
		if b.cols != 12 || b.rows != 3 {
			t.Errorf("box %+v", b)
		}
	}
}
