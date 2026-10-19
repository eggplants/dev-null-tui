// Package tui is the terminal client for /dev/null, built on tview.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
	"github.com/eggplants/dev-null-tui/internal/commit"
)

// view is a screen of the TUI: a tab's root or something opened from it, like a thread or a profile.
type view interface {
	tview.Primitive
	Reload()
}

type tab struct {
	name  string
	stack []view
}

type App struct {
	tv     *tview.Application
	client *api.Client
	me     *api.User

	root   *tview.Pages
	body   *tview.Flex
	header *tview.TextView
	status *tview.TextView

	tabs    []*tab
	current int
	unread  int

	statusTimer *time.Timer

	// commitWithImages sends a commit with images, signed with an SSH key on the user's GitHub account.
	commitWithImages func(ctx context.Context, opts commit.Options) (string, error)
}

func New(client *api.Client) *App {
	a := &App{
		tv:     tview.NewApplication(),
		client: client,
		root:   tview.NewPages(),
		body:   tview.NewFlex(),
		header: tview.NewTextView().SetDynamicColors(true).SetRegions(true),
		status: tview.NewTextView().SetDynamicColors(true),

		commitWithImages: commit.Run,
	}

	a.header.SetHighlightedFunc(func(added, _, _ []string) {
		if len(added) > 0 {
			var i int
			if _, err := fmt.Sscanf(added[0], "tab%d", &i); err == nil && i != a.current {
				a.switchTab(i)
			}
		}
	})

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.header, 1, 0, false).
		AddItem(a.body, 0, 1, true).
		AddItem(a.status, 1, 0, false)
	a.root.AddPage("main", layout, true, true)

	a.tv.SetRoot(a.root, true).EnableMouse(true).SetInputCapture(a.globalKeys)

	return a
}

// SetScreen draws the TUI on screen instead of the terminal, as tests do.
func (a *App) SetScreen(screen tcell.Screen) *App {
	a.tv.SetScreen(screen)

	return a
}

// Run signs in with /api/v1/me and runs the TUI until the user quits.
func (a *App) Run(ctx context.Context) error {
	me, err := a.client.Me(ctx)
	if err != nil {
		return err
	}
	a.me, a.unread = &me.Data, me.UnreadNotificationsCount

	a.tabs = []*tab{
		{name: "Following", stack: []view{newLogFeed(a, api.LogFollowing)}},
		{name: "Latest", stack: []view{newLogFeed(a, api.LogLatest)}},
		{name: "Trending", stack: []view{newLogFeed(a, api.LogTrending)}},
		{name: "Notifications", stack: []view{newNotifications(a)}},
		{name: "Saved", stack: []view{newSavedFeed(a)}},
		{name: "Profile", stack: []view{newProfile(a, a.me.Username)}},
	}
	a.switchTab(0)
	a.hint()

	go a.pollUnread(ctx)

	return a.tv.Run()
}

func (a *App) top() view {
	t := a.tabs[a.current]

	return t.stack[len(t.stack)-1]
}

func (a *App) show(v view) {
	a.body.Clear().AddItem(v, 0, 1, true)
	a.tv.SetFocus(v)
	a.drawHeader()
}

func (a *App) switchTab(i int) {
	first := len(a.tabs[i].stack) == 1 && !loaded(a.tabs[i].stack[0])
	a.current = i
	a.show(a.top())
	if first {
		a.top().Reload()
	}
}

// push opens a view on top of the current tab, as Enter on a commit opens its thread.
func (a *App) push(v view) {
	t := a.tabs[a.current]
	t.stack = append(t.stack, v)
	a.show(v)
	v.Reload()
}

func (a *App) pop() bool {
	t := a.tabs[a.current]
	if len(t.stack) == 1 {
		return false
	}

	t.stack = t.stack[:len(t.stack)-1]
	a.show(a.top())

	return true
}

func (a *App) drawHeader() {
	var b strings.Builder
	for i, t := range a.tabs {
		name := t.name
		if t.name == "Notifications" && a.unread > 0 {
			name += fmt.Sprintf(" [%s](%d)[-]", colorAccent, a.unread)
		}
		fmt.Fprintf(&b, `["tab%d"] %d %s [""]`, i, i+1, name)
	}
	a.header.SetText(b.String()).Highlight(fmt.Sprintf("tab%d", a.current))
}

// hint shows the key hints in the status bar.
func (a *App) hint() {
	a.status.SetText(fmt.Sprintf("[%s]j/k move  ⏎ open  c commit  r reply  f star  s save  u profile  ? help  q quit[-]", colorMuted))
}

// flash shows a message in the status bar for a few seconds. Call it from the UI goroutine.
func (a *App) flash(format string, args ...any) {
	a.status.SetText(fmt.Sprintf(format, args...))
	if a.statusTimer != nil {
		a.statusTimer.Stop()
	}
	a.statusTimer = time.AfterFunc(4*time.Second, func() {
		a.tv.QueueUpdateDraw(a.hint)
	})
}

func (a *App) flashError(err error) {
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case "device_not_approved":
			a.flash("[red]This key isn't approved any more. Quit and run `dn login`.[-]")
			return
		case "rate_limited":
			a.flash("[red]Slow down: try again in %s.[-]", apiErr.RetryAfter)
			return
		}
	}

	a.flash("[red]%s[-]", tview.Escape(err.Error()))
}

// async runs work off the UI goroutine and then done on it with work's error, which it flashes unless done
// handles it.
func (a *App) async(work func(ctx context.Context) error, done func(err error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := work(ctx)
		a.tv.QueueUpdateDraw(func() {
			if done != nil {
				done(err)
			} else if err != nil {
				a.flashError(err)
			}
		})
	}()
}

func (a *App) pollUnread(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			me, err := a.client.Me(ctx)
			if err != nil {
				continue
			}
			a.tv.QueueUpdateDraw(func() {
				a.unread = me.UnreadNotificationsCount
				a.drawHeader()
			})
		}
	}
}

func (a *App) modalOpen() bool {
	name, _ := a.root.GetFrontPage()

	return name != "main"
}

func (a *App) openModal(name string, p tview.Primitive) {
	a.root.AddPage(name, p, true, true)
	a.tv.SetFocus(p)
}

// closeModal removes a modal and focuses what is under it: another modal, like the composer under the image
// picker, or the current view.
func (a *App) closeModal(name string) {
	a.root.RemovePage(name)

	if front, p := a.root.GetFrontPage(); front != "main" && p != nil {
		a.tv.SetFocus(p)
		return
	}
	a.tv.SetFocus(a.top())
}

// confirm asks a yes-or-no question and runs yes if the user agrees.
func (a *App) confirm(question string, yes func()) {
	modal := tview.NewModal().SetText(question).AddButtons([]string{"Cancel", "Yes"}).
		SetDoneFunc(func(_ int, label string) {
			a.closeModal("confirm")
			if label == "Yes" {
				yes()
			}
		})
	a.openModal("confirm", modal)
}

func (a *App) globalKeys(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyCtrlC {
		a.tv.Stop()
		return nil
	}
	if a.modalOpen() {
		return event
	}

	switch event.Key() {
	case tcell.KeyTab:
		a.switchTab((a.current + 1) % len(a.tabs))
		return nil
	case tcell.KeyBacktab:
		a.switchTab((a.current + len(a.tabs) - 1) % len(a.tabs))
		return nil
	case tcell.KeyEscape, tcell.KeyBackspace, tcell.KeyBackspace2:
		a.pop()
		return nil
	case tcell.KeyRune:
	default:
		return event
	}

	switch r := event.Rune(); {
	case r >= '1' && r <= '9' && int(r-'1') < len(a.tabs):
		a.switchTab(int(r - '1'))
	case r == 'h':
		a.pop()
	case r == 'q':
		if !a.pop() {
			a.tv.Stop()
		}
	case r == 'R':
		a.top().Reload()
		if a.tabs[a.current].name == "Notifications" {
			a.unread = 0
			a.drawHeader()
		}
	case r == 'c':
		a.compose(composeOptions{})
	case r == '?':
		a.help()
	default:
		return event
	}

	return nil
}

func (a *App) help() {
	text := tview.NewTextView().SetDynamicColors(true).SetText(strings.TrimSpace(`
[::b]Move[::-]
  j k ↓ ↑     next / previous      g G       first / last
  ^d ^u       page down / up       1-6 tab   switch tab
  ⏎ l         open the thread      esc h ⌫   back
  q           back, or quit        ^c        quit

[::b]Commits[::-]
  c           new commit           R         refresh
  r           reply                Q         quote
  f           star / unstar        s         save / unsave
  P           give a pastel        u         author's profile
  o           open in browser      D         delete yours

[::b]Profile[::-]
  F           follow / unfollow    T         commits / replies

[::b]Composer[::-]
  ^s          commit               esc       cancel
  ^o          attach an image      ^r        remove the last one
`))
	text.SetBorder(true).SetTitle(" Keys ").SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		a.closeModal("help")
		return nil
	})

	a.openModal("help", centered(text, 66, 22))
}

func centered(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 0, true).
			AddItem(nil, 0, 1, false), width, 0, true).
		AddItem(nil, 0, 1, false)
}

func loaded(v view) bool {
	l, ok := v.(interface{ Loaded() bool })

	return ok && l.Loaded()
}
