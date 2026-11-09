package tui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// tabbedView is a few lists under one head, one shown at a time and T going to the next, as a search's results are.
type tabbedView struct {
	*tview.Box

	app   *App
	title string
	names []string
	tabs  []view
	// current is the tab shown.
	current int
	// head are the lines above the tabs, and headOverlay draws over them, top being the screen row of the first.
	head        func(width int) []string
	headOverlay func(screen tcell.Screen, top, x, y, width, height int)
	// keys handles the keys the owner adds on every tab, returning nil for those it used, and hints names them.
	keys  func(event *tcell.EventKey) *tcell.EventKey
	hints func() []hint
	// self is the view that embeds this one, if one does, which is what goes on screen.
	self view
}

func newTabbedView(app *App, title string) *tabbedView {
	return &tabbedView{Box: tview.NewBox(), app: app, title: title}
}

// addCommits adds a tab of commits.
func (t *tabbedView) addCommits(name string, f *commitFeed) {
	f.header = t.header
	f.headerOverlay = t.overlayHead
	f.keys = t.tabKeys
	f.hints = t.tabHints
	t.add(name, f)
}

// addPeople adds a tab of people.
func (t *tabbedView) addPeople(name string, l *peopleList) {
	l.header = t.header
	l.headerOverlay = t.overlayHead
	l.keys = t.tabKeys
	l.extraHints = t.tabHints
	t.add(name, l)
}

func (t *tabbedView) add(name string, v view) {
	t.names = append(t.names, name)
	t.tabs = append(t.tabs, v)
}

func (t *tabbedView) tab() view { return t.tabs[t.current] }

func (t *tabbedView) header(width int) []string {
	var lines []string
	if t.head != nil {
		lines = append(lines, t.head(width)...)
	}

	return append(lines, profileTabs(t.names, t.current, width), fmt.Sprintf("[%s]%s[-]", theme.hairline, strings.Repeat("─", max(width, 0))))
}

func (t *tabbedView) overlayHead(screen tcell.Screen, top, x, y, width, height int) {
	if t.headOverlay != nil {
		t.headOverlay(screen, top, x, y, width, height)
	}
}

func (t *tabbedView) tabKeys(event *tcell.EventKey) *tcell.EventKey {
	if t.keys != nil {
		if event = t.keys(event); event == nil {
			return nil
		}
	}
	if event.Key() == tcell.KeyRune && event.Rune() == 'T' && len(t.tabs) > 1 {
		t.switchTab((t.current + 1) % len(t.tabs))
		return nil
	}

	return event
}

func (t *tabbedView) tabHints() []hint {
	var hints []hint
	if len(t.tabs) > 1 {
		hints = append(hints, hint{"T", "next tab"})
	}
	if t.hints != nil {
		hints = append(hints, t.hints()...)
	}

	return hints
}

// switchTab shows another tab, loading it the first time.
func (t *tabbedView) switchTab(i int) {
	t.current = i
	t.Invalidate()
	if t.self != nil {
		t.app.show(t.self)
	} else {
		t.app.show(t)
	}
	if !loaded(t.tab()) {
		t.tab().Reload()
	}
}

func (t *tabbedView) Title() string { return t.title }
func (t *tabbedView) Hints() []hint { return t.tab().Hints() }
func (t *tabbedView) Loaded() bool  { return loaded(t.tab()) }

func (t *tabbedView) Loading() bool {
	l, ok := t.tab().(interface{ Loading() bool })
	return ok && l.Loading()
}

func (t *tabbedView) Position() (int, int) {
	if l, ok := t.tab().(interface{ Position() (int, int) }); ok {
		return l.Position()
	}

	return 0, 0
}

func (t *tabbedView) Select(i int) {
	if l, ok := t.tab().(interface{ Select(int) }); ok {
		l.Select(i)
	}
}

// Reload loads the tab shown again, and the others the next time they are shown.
func (t *tabbedView) Reload() {
	for i, v := range t.tabs {
		if i != t.current {
			if p, ok := v.(interface{ forget() }); ok {
				p.forget()
			}
		}
	}
	t.tab().Reload()
}

func (t *tabbedView) Invalidate() {
	for _, v := range t.tabs {
		if h, ok := v.(interface{ Invalidate() }); ok {
			h.Invalidate()
		}
	}
}

func (t *tabbedView) eachCommit(f func(c *api.Commit)) {
	for _, v := range t.tabs {
		if h, ok := v.(commitHolder); ok {
			h.eachCommit(f)
		}
	}
}

func (t *tabbedView) removeCommit(id string) {
	for _, v := range t.tabs {
		if h, ok := v.(commitHolder); ok {
			h.removeCommit(id)
		}
	}
}

func (t *tabbedView) Draw(screen tcell.Screen) {
	x, y, width, height := t.GetRect()
	t.tab().SetRect(x, y, width, height)
	t.tab().Draw(screen)
}

func (t *tabbedView) Focus(delegate func(tview.Primitive)) { delegate(t.tab()) }
func (t *tabbedView) HasFocus() bool                       { return t.tab().HasFocus() }

func (t *tabbedView) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return t.tab().InputHandler()
}

func (t *tabbedView) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
	return t.tab().MouseHandler()
}
