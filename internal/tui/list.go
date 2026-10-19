package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// listView is a scrollable list of multi-line items, laid out by a render function for the width it has.
// Header lines sit above the items and footer lines below them; neither can be selected.
type listView[T any] struct {
	*tview.Box

	items    []T
	render   func(item T, selected bool, width int) []string
	header   func(width int) []string
	footer   []string
	selected int
	offset   int

	// onLast runs when the selection moves onto the last item, so the owner can load the next page.
	onLast func()

	lines  []string
	starts []int
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

	l.starts = l.starts[:0]
	for i, item := range l.items {
		l.starts = append(l.starts, len(l.lines))
		l.lines = append(l.lines, l.render(item, i == l.selected, width)...)
	}
	l.starts = append(l.starts, len(l.lines))
	l.lines = append(l.lines, l.footer...)

	l.width = width
	l.dirty = false
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
	if width <= 0 || height <= 0 {
		return
	}

	if l.dirty || width != l.width {
		l.layout(width)
		l.scrollToSelection(height)
	}

	for row := 0; row < height && l.offset+row < len(l.lines); row++ {
		tview.Print(screen, l.lines[l.offset+row], x, y+row, width, tview.AlignLeft, tcell.ColorDefault)
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
			l.Select(l.indexAt(l.offset + mouseY - y))
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
