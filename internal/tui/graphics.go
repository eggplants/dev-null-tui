package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-sixel"
	"golang.org/x/image/draw"
)

// graphicsProtocol is how the terminal draws pixels, if it can.
type graphicsProtocol int

const (
	graphicsNone graphicsProtocol = iota
	graphicsKitty
	graphicsSixel
)

func (p graphicsProtocol) String() string {
	return [...]string{"none", "kitty", "sixel"}[p]
}

// detectGraphics asks the terminal which graphics protocol it speaks, before tcell takes it over. DN_GRAPHICS set to
// kitty, sixel or none skips asking. It also returns the size of a cell in pixels, if the terminal said.
func detectGraphics() (graphicsProtocol, int, int) {
	switch os.Getenv("DN_GRAPHICS") {
	case "kitty":
		return graphicsKitty, 0, 0
	case "sixel":
		return graphicsSixel, 0, 0
	case "none":
		return graphicsNone, 0, 0
	}

	// A kitty graphics query, the text area's size in pixels, and then the primary device attributes, which every
	// terminal answers: once that answer is in, so are the others the terminal knows.
	answer := queryTerminal("\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\" + "\x1b[14t" + "\x1b[c")

	protocol := graphicsNone
	switch {
	case bytes.Contains(answer, []byte("\x1b_Gi=31;OK")):
		protocol = graphicsKitty
	case hasSixel(answer):
		protocol = graphicsSixel
	}

	var height, width int
	if i := bytes.Index(answer, []byte("\x1b[4;")); i >= 0 {
		fmt.Sscanf(string(answer[i:]), "\x1b[4;%d;%dt", &height, &width)
	}

	return protocol, width, height
}

// hasSixel tells whether the primary device attributes, ESC [ ? 62 ; 4 ; ... c, list 4: sixel graphics.
func hasSixel(answer []byte) bool {
	i := bytes.Index(answer, []byte("\x1b[?"))
	if i < 0 {
		return false
	}
	da, _, ok := strings.Cut(string(answer[i+3:]), "c")
	if !ok {
		return false
	}

	for _, attr := range strings.Split(da, ";")[1:] {
		if attr == "4" {
			return true
		}
	}

	return false
}

// graphic is an image scaled and encoded for the terminal.
type graphic struct {
	id   uint32
	data []byte
}

// placement is a graphic drawn over a rectangle of cells.
type placement struct {
	graphic             *graphic
	x, y, width, height int
}

// graphicsScreen draws images over tcell's cells, which tcell can't do. Primitives place their images while they
// are drawn; after tcell has drawn the cells, Show writes the images straight to the terminal and locks their cells
// so tcell doesn't draw over them until they are taken away.
type graphicsScreen struct {
	tcell.Screen

	protocol graphicsProtocol
	// cellWidth and cellHeight are the size of a cell in pixels, for when the terminal doesn't say on resize.
	cellWidth, cellHeight int

	placed, shown []placement
	lastID        uint32
	width, height int
}

func newGraphicsScreen(screen tcell.Screen, protocol graphicsProtocol, cellWidth, cellHeight int) *graphicsScreen {
	return &graphicsScreen{Screen: screen, protocol: protocol, cellWidth: cellWidth, cellHeight: cellHeight}
}

// cellSize returns the size of a cell in pixels, guessing one when the terminal doesn't say.
func (s *graphicsScreen) cellSize() (int, int) {
	if tty, ok := s.Tty(); ok {
		if ws, err := tty.WindowSize(); err == nil {
			if w, h := ws.CellDimensions(); w > 0 && h > 0 {
				return w, h
			}
		}
	}
	if s.cellWidth > 0 && s.cellHeight > 0 {
		if w, h := s.Size(); w > 0 && h > 0 {
			// The answer to ESC [ 14 t is the whole text area.
			return s.cellWidth / w, s.cellHeight / h
		}
	}

	return 10, 20
}

// encode scales an image to width × height pixels for the terminal.
func (s *graphicsScreen) encode(img image.Image, width, height int) *graphic {
	scaled := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), img, img.Bounds(), draw.Src, nil)

	s.lastID++
	g := &graphic{id: s.lastID}

	var b bytes.Buffer
	switch s.protocol {
	case graphicsKitty:
		var raw bytes.Buffer
		if err := png.Encode(&raw, scaled); err != nil {
			return nil
		}
		// The image goes in chunks of 4096 base64 bytes; q=2 keeps the terminal from answering into tcell's input
		// and C=1 from moving the cursor.
		data := base64.StdEncoding.EncodeToString(raw.Bytes())
		for i := 0; i < len(data); i += 4096 {
			chunk := data[i:min(i+4096, len(data))]
			more := 0
			if i+4096 < len(data) {
				more = 1
			}
			if i == 0 {
				fmt.Fprintf(&b, "\x1b_Ga=T,f=100,q=2,C=1,i=%d,m=%d;%s\x1b\\", g.id, more, chunk)
			} else {
				fmt.Fprintf(&b, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
			}
		}
	case graphicsSixel:
		if err := sixel.NewEncoder(&b).Encode(scaled); err != nil {
			return nil
		}
	}
	g.data = b.Bytes()

	return g
}

// place draws a graphic over a rectangle of cells in this frame. Call it while drawing.
func (s *graphicsScreen) place(g *graphic, x, y, width, height int) {
	if g != nil {
		s.placed = append(s.placed, placement{g, x, y, width, height})
	}
}

// Clear starts a frame, which tview does before drawing.
func (s *graphicsScreen) Clear() {
	s.placed = s.placed[:0]
	s.Screen.Clear()
}

func (s *graphicsScreen) Show() { s.show(s.Screen.Show) }
func (s *graphicsScreen) Sync() { s.show(s.Screen.Sync) }

func (s *graphicsScreen) show(flush func()) {
	width, height := s.Size()
	if width == s.width && height == s.height && samePlacements(s.placed, s.shown) {
		flush()
		return
	}
	s.width, s.height = width, height

	var out bytes.Buffer
	for _, p := range s.shown {
		s.LockRegion(p.x, p.y, p.width, p.height, false)
		if s.protocol == graphicsKitty {
			fmt.Fprintf(&out, "\x1b_Ga=d,d=I,q=2,i=%d\x1b\\", p.graphic.id)
		}
	}
	// The cells under the images are drawn blank first, so nothing shows through transparent pixels.
	flush()

	for _, p := range s.placed {
		s.LockRegion(p.x, p.y, p.width, p.height, true)
		// Save the cursor, move to the cell, draw, and restore the cursor where tcell left it.
		fmt.Fprintf(&out, "\x1b7\x1b[%d;%dH", p.y+1, p.x+1)
		out.Write(p.graphic.data)
		out.WriteString("\x1b8")
	}
	s.shown = append(s.shown[:0], s.placed...)

	if tty, ok := s.Tty(); ok && out.Len() > 0 {
		_, _ = tty.Write(out.Bytes())
	}
}

func samePlacements(a, b []placement) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
