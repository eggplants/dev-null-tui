package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// listView is a scrollable list of multi-line items, laid out by a render function for the width it has.
// Header lines sit above the items and footer lines below them; neither can be selected.
type listView[T any] struct {
	*tview.Box

	items  []T
	render func(item T, selected bool, width int) []string
	header func(width int) []string
	// divider, if set, gives the lines that go above an item, after the one before it (nil for the first).
	divider  func(prev, item T, width int) []string
	footer   []string
	selected int
	offset   int

	// onLast runs when the selection moves onto the last item, so the owner can load the next page.
	onLast func()
	// onActivate runs when the selected item is clicked again, as Enter would open it.
	onActivate func()
	// overlay, if set, draws over an item once its lines are drawn, as its images are. top is the screen row of the
	// item's first line, which may be above or below the list's rectangle (x, y, width, height); it must not draw
	// outside that.
	overlay func(screen tcell.Screen, item T, top, x, y, width, height int)
	// headerOverlay draws over the header the same way, top being the screen row of its first line.
	headerOverlay func(screen tcell.Screen, top, x, y, width, height int)

	lines []string
	// starts is the first line of each item, its divider included, and heads the first line of the item itself.
	starts []int
	heads  []int
	width  int
	dirty  bool
}

func newListView[T any](render func(item T, selected bool, width int) []string) *listView[T] {
	return &listView[T]{Box: tview.NewBox(), render: render, dirty: true}
}

func (l *listView[T]) SetItems(items []T) {
	l.items = items
	l.selected = min(l.selected, max(len(items)-1, 0))
	l.dirty = true
}

func (l *listView[T]) Append(items []T) {
	l.items = append(l.items, items...)
	l.dirty = true
}

func (l *listView[T]) SetFooter(lines ...string) {
	l.footer = lines
	l.dirty = true
}

func (l *listView[T]) Invalidate() {
	l.dirty = true
}

// Position is where the selection is, for the status bar.
func (l *listView[T]) Position() (int, int) {
	return l.selected, len(l.items)
}

// Items are the items in the list, in order.
func (l *listView[T]) Items() []T {
	return l.items
}

// Selected returns the selected item, or false when the list is empty.
func (l *listView[T]) Selected() (T, bool) {
	if l.selected < 0 || l.selected >= len(l.items) {
		var zero T
		return zero, false
	}

	return l.items[l.selected], true
}

func (l *listView[T]) Replace(index int, item T) {
	if index >= 0 && index < len(l.items) {
		l.items[index] = item
		l.dirty = true
	}
}

func (l *listView[T]) Select(index int) {
	if len(l.items) == 0 {
		l.selected = 0
		return
	}

	index = max(0, min(index, len(l.items)-1))
	if index != l.selected {
		l.selected = index
		l.dirty = true
	}
	if index == len(l.items)-1 && l.onLast != nil {
		l.onLast()
	}
}

func (l *listView[T]) layout(width int) {
	l.lines = l.lines[:0]
	if l.header != nil {
		l.lines = append(l.lines, l.header(width)...)
	}

	l.starts, l.heads = l.starts[:0], l.heads[:0]
	var prev T
	for i, item := range l.items {
		l.starts = append(l.starts, len(l.lines))
		if l.divider != nil {
			l.lines = append(l.lines, l.divider(prev, item, width)...)
		}
		l.heads = append(l.heads, len(l.lines))
		l.lines = append(l.lines, l.render(item, i == l.selected, width)...)
		prev = item
	}
	l.starts = append(l.starts, len(l.lines))
	l.lines = append(l.lines, l.footer...)

	l.width = width
	l.dirty = false
}

// heads0 is where the items start, after the header.
func (l *listView[T]) heads0() int {
	if len(l.starts) == 0 {
		return len(l.lines)
	}

	return l.starts[0]
}

// scrollToSelection keeps the selected item in view, showing as much of it as fits from its top.
func (l *listView[T]) scrollToSelection(height int) {
	if len(l.items) == 0 {
		l.offset = 0
		return
	}

	start, end := l.starts[l.selected], l.starts[l.selected+1]
	if l.selected == 0 {
		start = 0
	}
	if l.selected == len(l.items)-1 {
		end = len(l.lines)
	}

	switch {
	case start < l.offset:
		l.offset = start
	case end > l.offset+height:
		l.offset = min(start, end-height)
	}
}

func (l *listView[T]) Draw(screen tcell.Screen) {
	l.Box.DrawForSubclass(screen, l)
	x, y, width, height := l.GetInnerRect()
	if width <= 1 || height <= 0 {
		return
	}

	// The last column is kept for the scrollbar.
	if l.dirty || width-1 != l.width {
		l.layout(width - 1)
		l.scrollToSelection(height)
	}

	// The selected item sits on the wash, all but its last line, which separates it from the next.
	washStart, washEnd := -1, -1
	if len(l.items) > 0 && l.selected < len(l.starts)-1 {
		washStart, washEnd = l.heads[l.selected], l.starts[l.selected+1]-1
	}
	wash := tcell.StyleDefault.Background(tcell.GetColor(colorWash))
	ink := tcell.GetColor(colorInk)

	for row := 0; row < height && l.offset+row < len(l.lines); row++ {
		line := l.offset + row
		if line >= washStart && line < washEnd {
			for col := range width - 1 {
				screen.SetContent(x+col, y+row, ' ', nil, wash)
			}
		}
		tview.Print(screen, l.lines[line], x, y+row, width-1, tview.AlignLeft, ink)
	}

	if l.headerOverlay != nil && l.offset < l.heads0() {
		l.headerOverlay(screen, y-l.offset, x, y, width-1, height)
	}
	if l.overlay != nil {
		for i, item := range l.items {
			if l.starts[i+1] > l.offset && l.heads[i] < l.offset+height {
				l.overlay(screen, item, y+l.heads[i]-l.offset, x, y, width-1, height)
			}
		}
	}

	// A list longer than the screen has a scrollbar on its right edge.
	if total := len(l.lines); total > height && height > 0 {
		thumb := max(1, height*height/total)
		top := l.offset * (height - thumb) / max(1, total-height)
		for row := range height {
			style := tcell.StyleDefault.Foreground(tcell.GetColor(colorRail)).Background(tcell.GetColor(theme.shell))
			r := '│'
			if row >= top && row < top+thumb {
				r = '┃'
				style = style.Foreground(tcell.GetColor(colorFaint))
			}
			screen.SetContent(x+width-1, y+row, r, nil, style)
		}
	}
}

func (l *listView[T]) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return l.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		_, _, _, height := l.GetInnerRect()

		switch event.Key() {
		case tcell.KeyDown:
			l.next(height)
		case tcell.KeyUp:
			l.previous()
		case tcell.KeyHome:
			l.Select(0)
		case tcell.KeyEnd:
			l.Select(len(l.items) - 1)
		case tcell.KeyPgDn, tcell.KeyCtrlD:
			l.Select(l.indexAt(l.offset + height))
		case tcell.KeyPgUp, tcell.KeyCtrlU:
			l.Select(l.indexAt(l.offset - height))
		case tcell.KeyRune:
			switch event.Rune() {
			case 'j':
				l.next(height)
			case 'k':
				l.previous()
			case 'g':
				l.Select(0)
			case 'G':
				l.Select(len(l.items) - 1)
			}
		}
	})
}

func (l *listView[T]) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	return l.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
		if !l.InRect(event.Position()) {
			return false, nil
		}

		switch action {
		case tview.MouseScrollDown:
			l.Select(l.selected + 1)
		case tview.MouseScrollUp:
			l.Select(l.selected - 1)
		case tview.MouseLeftClick:
			_, y, _, _ := l.GetInnerRect()
			_, mouseY := event.Position()
			index := l.indexAt(l.offset + mouseY - y)
			if index == l.selected && len(l.items) > 0 && l.onActivate != nil {
				l.onActivate()
			} else {
				l.Select(index)
			}
			setFocus(l)
		default:
			return false, nil
		}

		return true, nil
	})
}

// next scrolls through an item taller than the screen before it moves on to the next one.
func (l *listView[T]) next(height int) {
	if !l.dirty && l.selected+1 < len(l.starts) && l.starts[l.selected+1] > l.offset+height {
		l.offset++
		return
	}

	l.Select(l.selected + 1)
}

func (l *listView[T]) previous() {
	if !l.dirty && l.selected < len(l.starts) && (l.starts[l.selected] < l.offset || l.selected == 0 && l.offset > 0) {
		l.offset--
		return
	}

	l.Select(l.selected - 1)
}

// indexAt returns the item shown on a line of the layout.
func (l *listView[T]) indexAt(line int) int {
	for i := min(len(l.items), len(l.starts)-1) - 1; i >= 0; i-- {
		if l.starts[i] <= line {
			return i
		}
	}

	return 0
}
