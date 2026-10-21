// Package tui is the terminal client for /dev/null, built on tview.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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
	// Title names the view in the breadcrumbs.
	Title() string
	// Hints are the keys worth knowing on the view right now, the most useful first.
	Hints() []hint
}

// hint is a key and what it does, for the status bar.
type hint struct{ key, label string }

type tab struct {
	name  string
	short string
	stack []view
}

// Options are the settings the TUI takes from the config file and the environment.
type Options struct {
	// PollInterval is how often the logs are asked for new commits and /me for unread notifications.
	PollInterval time.Duration
	// Mouse turns the mouse on.
	Mouse bool
	// Theme is "dark", "light" or "auto", which picks the one that suits the terminal's background.
	Theme string
	// Graphics is "kitty", "sixel", "none" or "auto", which asks the terminal.
	Graphics string
	// SaveSetting keeps a setting changed in the settings for the next run, by its settings.json name.
	SaveSetting func(name, value string) error
	// Locked are the settings an environment variable sets, by their name, which win over settings.json.
	Locked map[string]string
	// DraftPath is where a new commit's draft is kept between runs.
	DraftPath string
	// System is what the app does outside its screen and the API; nil is the machine's own.
	System System
	// InlineImages shows commits' images in the lists, not only behind i.
	InlineImages bool
}

func DefaultOptions() Options {
	return Options{PollInterval: 30 * time.Second, Mouse: true, Theme: "auto", Graphics: "auto", InlineImages: true}
}

type App struct {
	tv     *tview.Application
	screen tcell.Screen // set by SetScreen, or else made by Run
	client *api.Client
	me     *api.CurrentUserData
	opts   Options
	// autoTheme is the palette that suits the terminal's background.
	autoTheme palette
	// sys opens links, copies, edits and keeps the draft.
	sys System
	// thumbs are the images the lists show.
	thumbs *thumbs

	root   *tview.Pages
	layout *tview.Flex
	body   *column
	header *tview.Box
	strip  *tview.Box
	crumbs *tview.Box
	status *tview.Box

	tabs    []*tab
	current int
	unread  int
	// tabSpans are where each tab's label was drawn in the header, for clicks.
	tabSpans [][2]int

	// toast is the message the status bar shows instead of the hints, until toastID moves on.
	toast   string
	toastID int
	// busy counts the actions in flight that the user waits on, which spin the spinner.
	busy     int
	spinning bool
	// pending is the key typed before the next one, like the g of "g n".
	pending rune

	// pendingReview are the user's commits waiting for review, checked when the logs are polled.
	pendingReview map[string]*api.Commit
	// quietUntil holds polling back after the server said to slow down.
	quietUntil time.Time
	// pollEvery hands the polling a new interval.
	pollEvery chan time.Duration
	// protocol is how the terminal draws images, for the settings to say.
	protocol graphicsProtocol

	// commitWithImages sends a commit with images, signed with an SSH key on the user's GitHub account.
	commitWithImages func(ctx context.Context, opts commit.Options) (string, error)
}

func New(client *api.Client) *App {
	return NewWithOptions(client, DefaultOptions())
}

func NewWithOptions(client *api.Client, opts Options) *App {
	a := &App{
		tv:     tview.NewApplication(),
		client: client,
		opts:   opts,
		root:   tview.NewPages(),
		header: tview.NewBox(),
		strip:  tview.NewBox(),
		crumbs: tview.NewBox(),
		status: tview.NewBox(),

		pendingReview:    map[string]*api.Commit{},
		pollEvery:        make(chan time.Duration, 1),
		commitWithImages: commit.Run,
	}
	a.body = newColumn(maxColumnWidth)
	a.body.aside = a.drawAside

	a.header.SetDrawFunc(a.drawHeaderBox)
	a.header.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action != tview.MouseLeftClick || !a.header.InRect(event.Position()) {
			return action, event
		}
		x, _ := event.Position()
		for i, span := range a.tabSpans {
			if x >= span[0] && x < span[1] && i < len(a.tabs) {
				a.switchTab(i)
				return action, nil
			}
		}
		return action, event
	})
	a.strip.SetDrawFunc(a.drawStrip)
	a.crumbs.SetDrawFunc(a.drawCrumbs)
	a.status.SetDrawFunc(a.drawStatus)

	a.layout = tview.NewFlex().SetDirection(tview.FlexRow)
	layout := a.layout.
		AddItem(a.header, 1, 0, false).
		AddItem(a.strip, 1, 0, false).
		AddItem(a.crumbs, 1, 0, false).
		AddItem(a.body, 0, 1, true).
		AddItem(a.status, 1, 0, false)
	a.root.AddPage("main", layout, true, true)

	a.autoTheme = darkPalette
	a.thumbs = newThumbs(a)
	imageRows = 0
	if opts.InlineImages {
		imageRows = 8
	}
	a.sys = opts.System
	if a.sys == nil {
		a.sys = &osSystem{draftPath: opts.DraftPath, tty: a.tty}
	}
	a.tv.SetRoot(&sizeGuard{Pages: a.root}, true).EnableMouse(opts.Mouse).SetInputCapture(a.globalKeys)

	return a
}

// tty is the terminal the app draws on, if it draws on one.
func (a *App) tty() (io.Writer, bool) {
	if a.screen == nil {
		return nil, false
	}
	tty, ok := a.screen.Tty()

	return tty, ok
}

// SetScreen draws the TUI on screen instead of the terminal, as tests do.
func (a *App) SetScreen(screen tcell.Screen) *App {
	a.screen = screen
	a.tv.SetScreen(screen)

	return a
}

// Run signs in with /api/v1/me and runs the TUI until the user quits.
func (a *App) Run(ctx context.Context) error {
	var protocol graphicsProtocol
	// areaWidth and areaHeight are the terminal's text area in pixels, if it said.
	var areaWidth, areaHeight int
	if a.screen == nil {
		// The terminal is asked about itself before tcell takes it over and would read the answers as keys.
		answer := probeTerminal(a.opts)
		a.autoTheme = pickTheme("auto", answer)
		applyTheme(pickTheme(a.opts.Theme, answer))
		protocol, areaWidth, areaHeight = detectGraphics(a.opts.Graphics, answer)
	}
	a.paint()

	me, err := a.client.Me(ctx)
	if err != nil {
		return err
	}
	a.me, a.unread = &me.Data, me.UnreadNotificationsCount
	showPasteis = a.me.PasteisMode

	a.tabs = []*tab{
		{name: "Following", short: "Fol", stack: []view{newLogFeed(a, api.LogFollowing)}},
		{name: "Latest", short: "New", stack: []view{newLogFeed(a, api.LogLatest)}},
		{name: "Trending", short: "Hot", stack: []view{newLogFeed(a, api.LogTrending)}},
		{name: "Notifications", short: "Ntf", stack: []view{newNotifications(a)}},
		{name: "Starred", short: "Star", stack: []view{newStarredFeed(a)}},
		{name: "Profile", short: "Me", stack: []view{newProfile(a, a.me.Username)}},
	}
	a.switchTab(0)

	go a.poll(ctx)

	if a.screen == nil {
		screen, err := tcell.NewScreen()
		if err != nil {
			return err
		}
		a.protocol = protocol
		gs := newGraphicsScreen(screen, protocol, areaWidth, areaHeight)
		a.SetScreen(gs)
		// tview only turns the mouse and pasting on for a screen it makes itself.
		a.tv.EnableMouse(a.opts.Mouse).EnablePaste(true)
		if protocol != graphicsNone {
			// The screen only knows its cells' size once it's started, which tview does in Run.
			a.tv.SetAfterDrawFunc(func(tcell.Screen) {
				if w, h := gs.cellSize(); w > 0 && h > 0 && (w != cellWidth || h != cellHeight) {
					cellWidth, cellHeight = w, h
					a.views(func(v view) {
						if i, ok := v.(interface{ Invalidate() }); ok {
							i.Invalidate()
						}
					})
					go a.tv.QueueUpdateDraw(func() {})
				}
			})
		}
	}

	return a.tv.Run()
}

func (a *App) top() view {
	t := a.tabs[a.current]

	return t.stack[len(t.stack)-1]
}

func (a *App) show(v view) {
	a.body.SetInner(v)
	a.tv.SetFocus(v)
	a.drawHeader()
}

func (a *App) switchTab(i int) {
	first := len(a.tabs[i].stack) == 1 && !loaded(a.tabs[i].stack[0])
	a.current = i
	a.show(a.top())
	switch {
	case first:
		a.top().Reload()
	case len(a.tabs[i].stack) == 1:
		// A log with new commits on top takes them in when it's switched to at its top, as it would have if shown.
		if f, ok := a.top().(*commitFeed); ok && f.newCount > 0 && f.selected == 0 {
			f.Reload()
		}
	}
}

// tabIndex finds a tab by name.
func (a *App) tabIndex(name string) int {
	for i, t := range a.tabs {
		if t.name == name {
			return i
		}
	}

	return -1
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

// paint gives the app's own primitives the theme's background, as those made after applyTheme take it themselves.
func (a *App) paint() {
	shell := tcell.GetColor(theme.shell)
	for _, b := range []interface{ SetBackgroundColor(tcell.Color) *tview.Box }{a.root, a.layout, a.header, a.strip, a.crumbs, a.status, a.body} {
		b.SetBackgroundColor(shell)
	}
	a.views(func(v view) {
		if b, ok := v.(interface{ SetBackgroundColor(tcell.Color) *tview.Box }); ok {
			b.SetBackgroundColor(shell)
		}
		if p, ok := v.(*profileView); ok {
			for _, t := range p.tabs {
				if b, ok := t.(interface{ SetBackgroundColor(tcell.Color) *tview.Box }); ok {
					b.SetBackgroundColor(shell)
				}
			}
		}
	})
}

// views calls f with every view in every tab, roots first.
func (a *App) views(f func(v view)) {
	for _, t := range a.tabs {
		for _, v := range t.stack {
			f(v)
		}
	}
}

const logo = "/dev/null"

// drawHeader asks for the header to be drawn again, as it is with every frame.
func (a *App) drawHeader() {}

// columnRect is where the column of the current view is across the screen's width, the aside included when there's
// room for it, which the header, the crumbs and the status bar line up with.
func columnRect(x, width int) (int, int) {
	w := min(width, maxColumnWidth)
	if width >= maxColumnWidth+asideGap+asideWidth {
		w = maxColumnWidth + asideGap + asideWidth
	}

	return x + (width-w)/2, w
}

// drawAside draws the panel right of the column: who's signed in, and the keys for what's on screen.
func (a *App) drawAside(screen tcell.Screen, x, y, width, height int) {
	if a.me == nil || len(a.tabs) == 0 {
		return
	}

	rule := fmt.Sprintf("[%s]%s[-]", theme.hairline, strings.Repeat("─", width))
	name := a.me.Name
	if name == "" {
		name = a.me.Username
	}
	lines := []string{
		"",
		fmt.Sprintf("%s [%s::b]%s[-::-]", avatarChip(&a.me.User), colorInk, tview.Escape(name)),
		fmt.Sprintf("     [%s]●[-] [%s]@%s[-]", laneColor(&a.me.User), colorMuted, tview.Escape(a.me.Username)),
	}
	if showPasteis {
		lines = append(lines, fmt.Sprintf("     [%s]◍[-] [%s]%d pastéis left today[-]", theme.lanes[5], colorMuted, a.me.PasteisRemaining))
	}
	if a.unread > 0 {
		lines = append(lines, fmt.Sprintf("     [%s]●[-] [%s]%d unread notification%s[-]", colorPrimary, colorMuted, a.unread, plural(a.unread)))
	}
	lines = append(lines, "", rule, "", fmt.Sprintf("[%s::b]Keys[-::-]", colorInk))
	for _, h := range append(a.top().Hints(), hint{"c", "new commit"}, hint{"/", "go to"}, hint{"g", "go places"}, hint{"?", "all keys"}) {
		lines = append(lines, fmt.Sprintf(" [%s::b]%-6s[-::-] [%s]%s[-]", colorInk, tview.Escape(h.key), colorMuted, h.label))
	}
	lines = append(lines, "", rule, fmt.Sprintf("[%s]%s[-]", colorFaint, tview.Escape(strings.TrimPrefix(strings.TrimPrefix(a.client.BaseURL, "https://"), "http://"))))

	for i, l := range lines {
		if i >= height {
			break
		}
		tview.Print(screen, l, x, y+i, width, tview.AlignLeft, tcell.GetColor(colorInk))
	}
}

// drawHeaderBox draws the logo, the tabs with their unread and new counts, and the user's name.
func (a *App) drawHeaderBox(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
	if len(a.tabs) == 0 {
		return x, y, width, height
	}
	x, width = columnRect(x, width)

	me := ""
	if a.me != nil {
		me = fmt.Sprintf("[%s]@%s[-] ", colorMuted, tview.Escape(a.me.Username))
	}
	logo := fmt.Sprintf(" [%s::b]/[%s::b]dev[%s::b]/[%s::b]null[-::-] ", theme.brand, colorInk, theme.brand, colorInk)

	// The tabs lose their numbers, then their full names, when they don't fit; the user's name goes first.
	tiers := []struct{ numbers, short bool }{{true, false}, {false, false}, {true, true}, {false, true}}
	for n, tier := range tiers {
		labels := make([]string, len(a.tabs))
		total := tview.TaggedStringWidth(logo)
		for i, t := range a.tabs {
			name := t.name
			if tier.short {
				name = t.short
			}
			label := tview.Escape(name)
			if tier.numbers {
				label = fmt.Sprintf("[%s]%d[-] %s", colorFaint, i+1, label)
			}
			if i == a.current {
				label = fmt.Sprintf("[%s:%s:b] %s [-:-:-]", colorInk, theme.primarySoft, label)
			} else {
				label = fmt.Sprintf("[%s] %s [-]", colorMuted, label)
			}
			if t.name == "Notifications" && a.unread > 0 {
				label += fmt.Sprintf("[%s:%s:b]%d[-:-:-]", theme.popover, colorPrimary, a.unread)
			}
			if f, ok := t.stack[0].(*commitFeed); ok && f.newCount > 0 {
				label += fmt.Sprintf("[%s]↑%d[-]", colorPrimary, f.newCount)
			}
			labels[i] = label
			total += tview.TaggedStringWidth(label) + 1
		}
		if total > width && n < len(tiers)-1 {
			continue
		}

		tview.Print(screen, logo, x, y, width, tview.AlignLeft, tcell.GetColor(colorInk))
		at := x + tview.TaggedStringWidth(logo)
		a.tabSpans = a.tabSpans[:0]
		for _, label := range labels {
			w := tview.TaggedStringWidth(label)
			tview.Print(screen, label, at, y, x+width-at, tview.AlignLeft, tcell.GetColor(colorInk))
			a.tabSpans = append(a.tabSpans, [2]int{at, at + w})
			at += w + 1
		}
		if at+tview.TaggedStringWidth(me) <= x+width {
			tview.Print(screen, me, x, y, width, tview.AlignRight, tcell.GetColor(colorInk))
		}
		break
	}

	return x, y, width, height
}

// drawStrip draws the six lanes across the screen, like the web's banner.
func (a *App) drawStrip(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
	for i := range width {
		color := tcell.GetColor(theme.lanes[i*len(theme.lanes)/max(width, 1)])
		screen.SetContent(x+i, y, '▀', nil, tcell.StyleDefault.Foreground(color).Background(tcell.GetColor(theme.shell)))
	}

	return x, y, width, height
}

// drawCrumbs says where the current view is: the tab and what was opened from it.
func (a *App) drawCrumbs(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
	if len(a.tabs) == 0 {
		return x, y, width, height
	}

	x, width = columnRect(x, width)
	t := a.tabs[a.current]
	parts := make([]string, len(t.stack))
	for i, v := range t.stack {
		style := colorMuted
		if i == len(t.stack)-1 {
			style = colorInk + "::b"
		}
		parts[i] = fmt.Sprintf("[%s]%s[-::-]", style, tview.Escape(v.Title()))
	}
	left := " " + strings.Join(parts, fmt.Sprintf(" [%s]›[-] ", colorFaint))
	// Long trails lose their oldest crumbs first.
	for len(parts) > 2 && tview.TaggedStringWidth(left) > width-12 {
		parts = parts[1:]
		left = " …" + fmt.Sprintf(" [%s]›[-] ", colorFaint) + strings.Join(parts, fmt.Sprintf(" [%s]›[-] ", colorFaint))
	}

	right := ""
	if len(t.stack) > 1 {
		right = fmt.Sprintf("[%s]esc[-] [%s]back[-] ", colorInk, colorMuted)
	}
	tview.Print(screen, spread(left, right, width), x, y, width, tview.AlignLeft, tcell.GetColor(colorInk))

	return x, y, width, height
}

var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

func spinnerFrame() string {
	return string(spinnerFrames[time.Now().UnixMilli()/100%int64(len(spinnerFrames))])
}

// drawStatus draws the toast, or the spinner and the hints for the current view, with where the selection is.
func (a *App) drawStatus(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
	if len(a.tabs) == 0 {
		return x, y, width, height
	}

	x, width = columnRect(x, width)
	right := ""
	if p, ok := a.top().(interface{ Position() (int, int) }); ok {
		if i, n := p.Position(); n > 0 {
			right = fmt.Sprintf("[%s]%d/%d[-] ", colorFaint, i+1, n)
		}
	}

	left := " "
	switch {
	case a.toast != "":
		left += a.toast
	case a.pending == 'g':
		left += hintsText(gHints, width-tview.TaggedStringWidth(right)-2)
	default:
		if a.busy > 0 {
			left += fmt.Sprintf("[%s]%s[-] ", colorPrimary, spinnerFrame())
		}
		hints := append(a.top().Hints(), hint{"?", "help"})
		left += hintsText(hints, width-tview.TaggedStringWidth(left)-tview.TaggedStringWidth(right)-1)
	}
	tview.Print(screen, spread(left, right, width), x, y, width, tview.AlignLeft, tcell.GetColor(colorInk))

	return x, y, width, height
}

// gHints are the places g goes, shown while it waits for the next key.
var gHints = []hint{{"g", "top"}, {"l", "your log"}, {"a", "latest"}, {"e", "trending"}, {"n", "notifications"}, {"b", "starred"}, {"p", "profile"}, {"s", "settings"}}

// hintsText lays hints out in width, dropping those that don't fit but always keeping the last one, which is help.
func hintsText(hints []hint, width int) string {
	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = fmt.Sprintf("[%s::b]%s[-::-] [%s]%s[-]", colorInk, tview.Escape(h.key), colorMuted, h.label)
	}

	for n := len(parts); n > 0; n-- {
		shown := append(append([]string{}, parts[:n-1]...), parts[len(parts)-1])
		if n == len(parts) {
			shown = parts
		}
		if text := strings.Join(shown, "  "); tview.TaggedStringWidth(text) <= width {
			return text
		}
	}

	return ""
}

type toastKind int

const (
	toastInfo toastKind = iota
	toastOK
	toastError
)

// flash shows a message in the status bar for a few seconds. Call it from the UI goroutine.
func (a *App) flash(format string, args ...any) {
	a.notify(toastInfo, format, args...)
}

// succeed flashes that something worked.
func (a *App) succeed(format string, args ...any) {
	a.notify(toastOK, format, args...)
}

// fail flashes what went wrong, for longer.
func (a *App) fail(format string, args ...any) {
	a.notify(toastError, format, args...)
}

func (a *App) notify(kind toastKind, format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	duration := 4 * time.Second
	switch kind {
	case toastOK:
		text = fmt.Sprintf("[%s]✓[-] %s", colorSuccess, text)
	case toastError:
		text = fmt.Sprintf("[%s]✕ %s[-]", colorDanger, text)
		duration = 8 * time.Second
	}

	a.toastID++
	a.toast = text
	id := a.toastID
	time.AfterFunc(duration, func() {
		a.tv.QueueUpdateDraw(func() {
			// A newer toast keeps its own time.
			if a.toastID == id {
				a.toast = ""
			}
		})
	})
}

func (a *App) flashError(err error) {
	a.fail("%s", tview.Escape(errorMessage(err)))
}

// errorMessage says what went wrong in words a person can act on.
func errorMessage(err error) string {
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case "device_not_approved":
			return "This key isn't approved any more. Quit and run `dn login`."
		case "invalid_signature":
			return "The server didn't take this key's signature. Is your clock right? If it is, run `dn login`."
		case "invalid_grant":
			return "That sign-in code was used or has expired. Run `dn login` again."
		case "rate_limited":
			if apiErr.RetryAfter > 0 {
				return fmt.Sprintf("Slow down: try again in %s.", apiErr.RetryAfter)
			}
			return "Slow down: try again in a minute."
		case "user_blocked":
			return "You can't do that: one of you blocked the other."
		case "user_unavailable":
			return "This account isn't available."
		case "own_commit":
			return "That's your own commit."
		case "commit_unavailable":
			return "This commit isn't available any more."
		case "pasteis_exhausted":
			return "You're out of pastéis for today. They come back at midnight UTC."
		case "github_sync_failed":
			return "GitHub didn't answer. Try again in a bit."
		case "timed_out":
			return "Your account is timed out for now: " + apiErr.Message
		case "banned":
			return "This account is banned from /dev/null."
		case "commit_rules_required":
			return "Accept the commit rules on the web before you commit."
		}

		switch {
		case apiErr.Status == 404:
			return "fatal: not found. It may have been deleted."
		case apiErr.Status == 422 && apiErr.Message != "":
			return apiErr.Message
		case apiErr.Status >= 500:
			return fmt.Sprintf("The server had a problem (%d). Try again in a bit.", apiErr.Status)
		}

		return apiErr.Error()
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "The server took too long to answer. Are you online?"
		}
		return "Can't reach /dev/null. Are you online?"
	}

	return err.Error()
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

// act runs an action the user waits on like async, spinning the spinner until it's done.
func (a *App) act(work func(ctx context.Context) error, done func(err error)) {
	a.busy++
	a.spin()
	a.async(work, func(err error) {
		a.busy--
		if done != nil {
			done(err)
		} else if err != nil {
			a.flashError(err)
		}
	})
}

// spin redraws the status bar while something is busy, so the spinner turns.
func (a *App) spin() {
	if a.spinning {
		return
	}
	a.spinning = true

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			stop := false
			a.tv.QueueUpdateDraw(func() {
				if a.busy <= 0 && !a.anyLoading() {
					a.spinning, stop = false, true
				}
			})
			if stop {
				return
			}
		}
	}()
}

func (a *App) anyLoading() bool {
	l, ok := a.top().(interface{ Loading() bool })

	return ok && l.Loading()
}

// poll asks for unread notifications, new commits on the logs and the review of the user's pending commits, as the
// web does every 30 seconds.
func (a *App) poll(ctx context.Context) {
	ticker := time.NewTicker(a.opts.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case d := <-a.pollEvery:
			ticker.Reset(d)
			continue
		case <-ticker.C:
		}

		quiet := make(chan bool, 1)
		a.tv.QueueUpdate(func() { quiet <- time.Now().Before(a.quietUntil) })
		select {
		case <-ctx.Done():
			return
		case q := <-quiet:
			if q {
				continue
			}
		}

		me, err := a.client.Me(ctx)
		if a.backOff(err) {
			continue
		}
		if err == nil {
			a.tv.QueueUpdateDraw(func() {
				a.unread = me.UnreadNotificationsCount
				a.me.PasteisRemaining = me.Data.PasteisRemaining
				a.drawHeader()
			})
		}

		a.tv.QueueUpdate(func() {
			for _, t := range a.tabs {
				if f, ok := t.stack[0].(*commitFeed); ok {
					f.checkNewCommits()
				}
			}
			a.checkReviews()
		})
	}
}

// backOff holds polling back for as long as a rate_limited answer says, or a minute, and tells whether it did.
// Call it off the UI goroutine.
func (a *App) backOff(err error) bool {
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "rate_limited" && apiErr.Status != 429 {
		return false
	}

	wait := apiErr.RetryAfter
	if wait <= 0 {
		wait = time.Minute
	}
	a.tv.QueueUpdate(func() { a.quietUntil = time.Now().Add(wait) })

	return true
}

// checkReviews asks how the review of the user's pending commits went, telling them once it's done.
func (a *App) checkReviews() {
	for id, c := range a.pendingReview {
		var thread *api.Thread
		a.async(func(ctx context.Context) (err error) {
			thread, err = a.client.Commit(ctx, id)
			return err
		}, func(err error) {
			if err != nil {
				var apiErr *api.Error
				if errors.As(err, &apiErr) && apiErr.Status == 404 {
					delete(a.pendingReview, id)
				}
				return
			}

			switch thread.Commit.Status {
			case "approved":
				delete(a.pendingReview, id)
				a.succeed("%s was approved: everyone can see it now.", c.ShortHash)
			case "rejected":
				delete(a.pendingReview, id)
				a.fail("%s was rejected in review.", c.ShortHash)
			default:
				return
			}
			a.updateCommit(&thread.Commit)
		})
	}
}

// updateCommit takes a commit's new counts and status into every copy of it on screen, in every tab.
func (a *App) updateCommit(updated *api.Commit) {
	a.views(func(v view) {
		if h, ok := v.(commitHolder); ok {
			h.eachCommit(func(c *api.Commit) {
				if c.ID != updated.ID || c == updated {
					return
				}
				c.Starred, c.StarsCount = updated.Starred, updated.StarsCount
				c.PasteisCount = updated.PasteisCount
				if updated.RepliesCount > 0 || c.RepliesCount == 0 {
					c.RepliesCount = updated.RepliesCount
				}
				if updated.Status != "" {
					c.Status = updated.Status
				}
			})
			h.Invalidate()
		}
	})
}

// removeCommit takes a deleted commit out of every list it is in.
func (a *App) removeCommit(id string) {
	a.views(func(v view) {
		if h, ok := v.(commitHolder); ok {
			h.removeCommit(id)
		}
	})
}

// commitHolder is a view that shows commits.
type commitHolder interface {
	// eachCommit calls f with every commit the view shows, including those quoted and previewed in others.
	eachCommit(f func(c *api.Commit))
	removeCommit(id string)
	Invalidate()
}

// knownCommit finds a commit already on screen by its id or short hash.
func (a *App) knownCommit(ref string) *api.Commit {
	var found *api.Commit
	a.views(func(v view) {
		if h, ok := v.(commitHolder); ok && found == nil {
			h.eachCommit(func(c *api.Commit) {
				if found == nil && (c.ID == ref || c.ShortHash == ref) {
					found = c
				}
			})
		}
	})

	return found
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
	a.choose(question, []string{"Cancel", "Yes"}, func(label string) {
		if label == "Yes" {
			yes()
		}
	})
}

// choose asks the user to pick one of the buttons and runs picked with its label, unless they press esc.
func (a *App) choose(question string, buttons []string, picked func(label string)) {
	modal := tview.NewModal().SetText(question).AddButtons(buttons).
		SetDoneFunc(func(_ int, label string) {
			a.closeModal("confirm")
			if label != "" {
				picked(label)
			}
		})
	styleModal(modal)
	a.openModal("confirm", modal)
}

func styleModal(m *tview.Modal) {
	m.SetBackgroundColor(tcell.GetColor(theme.popover)).SetTextColor(tcell.GetColor(colorInk)).
		SetButtonBackgroundColor(tcell.GetColor(colorWash)).SetButtonTextColor(tcell.GetColor(colorInk)).
		SetButtonActivatedStyle(tcell.StyleDefault.Background(tcell.GetColor(colorPrimary)).Foreground(tcell.GetColor(theme.popover)))
	m.SetBorderColor(tcell.GetColor(colorRail))
}

func (a *App) globalKeys(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyCtrlC {
		a.tv.Stop()
		return nil
	}
	if a.modalOpen() {
		return event
	}

	if a.pending == 'g' {
		a.pending = 0
		if event.Key() == tcell.KeyRune {
			a.goTo(event.Rune())
		}
		return nil
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
	case tcell.KeyCtrlK:
		a.jump()
		return nil
	case tcell.KeyRune:
	default:
		return event
	}

	switch r := event.Rune(); {
	case r >= '1' && r <= '9' && int(r-'1') < len(a.tabs):
		a.switchTab(int(r - '1'))
	case r == 'g':
		a.pending = 'g'
	case r == 'h':
		a.pop()
	case r == 'Q':
		a.tv.Stop()
	case r == 'R':
		a.top().Reload()
		if a.tabs[a.current].name == "Notifications" {
			a.unread = 0
			a.drawHeader()
		}
	case r == 'c':
		a.compose(composeOptions{})
	case r == '/':
		a.jump()
	case r == '?':
		a.help()
	default:
		return event
	}

	return nil
}

// goTo goes where g and the key after it say.
func (a *App) goTo(r rune) {
	// l, e, p and s go where they do on the web; the rest are the tabs the web has no key for.
	tabs := map[rune]string{'l': "Following", 'a': "Latest", 'e': "Trending", 'n': "Notifications", 'b': "Starred", 'p': "Profile"}
	switch {
	case r == 'g':
		if l, ok := a.top().(interface{ Select(int) }); ok {
			l.Select(0)
		}
	case r == 's':
		a.push(newSettings(a))
	case tabs[r] != "":
		a.switchTab(a.tabIndex(tabs[r]))
	}
}

func (a *App) help() {
	text := tview.NewTextView().SetDynamicColors(true).SetText(helpText())
	text.SetBorder(true).SetTitle(" Keys ").SetBackgroundColor(tcell.GetColor(theme.popover))
	text.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Key() == tcell.KeyRune && (event.Rune() == 'j' || event.Rune() == 'k'),
			event.Key() == tcell.KeyDown, event.Key() == tcell.KeyUp, event.Key() == tcell.KeyPgDn, event.Key() == tcell.KeyPgUp:
			return event
		}
		a.closeModal("help")
		return nil
	})

	_, _, width, height := a.root.GetRect()
	a.openModal("help", centered(text, min(78, width), min(32, height)))
}

func helpText() string {
	section := func(title string) string { return fmt.Sprintf("[%s::b]%s[-::-]\n", colorPrimary, title) }
	row := func(k1, l1, k2, l2 string) string {
		left := fmt.Sprintf("  [%s::b]%-10s[-::-] [%s]%-20s[-]", colorInk, k1, colorMuted, l1)
		if k2 == "" {
			return left + "\n"
		}
		return left + fmt.Sprintf(" [%s::b]%-10s[-::-] [%s]%s[-]\n", colorInk, k2, colorMuted, l2) + ""
	}

	return strings.TrimRight(section("Move")+
		row("j k ↓ ↑", "next / previous", "gg G", "first / last")+
		row("^d ^u", "page down / up", "1-6 tab", "switch tab")+
		row("⏎ l", "open the thread", "esc h ⌫", "back")+
		row("g l/a/e", "following/latest/hot", "g n/b/p", "notifs/starred/me")+
		row("g s", "settings", "/ ^k", "go to @user or link")+
		row("Q", "quit", "^c", "quit")+
		"\n"+section("Commits")+
		row("c", "new commit", "R", "refresh")+
		row("r", "reply", "q", "quote")+
		row("o", "show / hide replies", "w", "open in browser")+
		row("s", "star / unstar", "P", "give a pastel")+
		row("u", "author's profile", "", "")+
		row("y", "copy the link", "i", "view the images")+
		row("D", "delete yours", "!", "report")+
		"\n"+section("Profile")+
		row("F", "follow / unfollow", "T", "next tab")+
		row("M", "mute / unmute", "B", "block")+
		"\n"+section("Notifications")+
		row("f", "filter by kind", "e", "everyone in a group")+
		"\n"+section("Composer")+
		row("^s ^⏎", "commit", "esc", "cancel, keeping a draft")+
		row("^o", "attach an image", "^r", "remove the last one")+
		row("^e", "write in $EDITOR", "^p", "preview")+
		row("tab", "complete @mention", "", ""), "\n")
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

// maxColumnWidth is how wide the column of a view gets on a wide terminal, about as wide as the web's feed reads.
const maxColumnWidth = 100

// The aside is the panel right of the column on a terminal wide enough for both, as the web has one.
const asideWidth, asideGap = 34, 3

// column draws a view centered, no wider than maxWidth, with the aside right of it when there's room.
type column struct {
	*tview.Box
	inner    tview.Primitive
	maxWidth int
	// aside draws the panel in its rectangle, if set.
	aside func(screen tcell.Screen, x, y, width, height int)
}

func newColumn(maxWidth int) *column {
	return &column{Box: tview.NewBox(), maxWidth: maxWidth}
}

func (c *column) SetInner(p tview.Primitive) { c.inner = p }

func (c *column) Draw(screen tcell.Screen) {
	c.Box.DrawForSubclass(screen, c)
	if c.inner == nil {
		return
	}
	x, y, width, height := c.GetRect()
	w := min(width, c.maxWidth)
	if c.aside != nil && width >= c.maxWidth+asideGap+asideWidth {
		left := x + (width-c.maxWidth-asideGap-asideWidth)/2
		c.inner.SetRect(left, y, w, height)
		c.inner.Draw(screen)
		c.aside(screen, left+w+asideGap, y, asideWidth, height)
		return
	}
	c.inner.SetRect(x+(width-w)/2, y, w, height)
	c.inner.Draw(screen)
}

func (c *column) Focus(delegate func(p tview.Primitive)) {
	if c.inner != nil {
		delegate(c.inner)
	}
}

func (c *column) HasFocus() bool { return c.inner != nil && c.inner.HasFocus() }

func (c *column) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	if c.inner == nil {
		return nil
	}

	return c.inner.InputHandler()
}

func (c *column) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
	if c.inner == nil {
		return nil
	}

	return c.inner.MouseHandler()
}

func (c *column) PasteHandler() func(text string, setFocus func(p tview.Primitive)) {
	if c.inner == nil {
		return nil
	}

	return c.inner.PasteHandler()
}

// sizeGuard asks for a bigger terminal instead of drawing the TUI squashed.
type sizeGuard struct {
	*tview.Pages
}

const minWidth, minHeight = 40, 12

func (g *sizeGuard) Draw(screen tcell.Screen) {
	width, height := screen.Size()
	if width >= minWidth && height >= minHeight {
		g.Pages.Draw(screen)
		return
	}

	screen.Fill(' ', tcell.StyleDefault.Background(tcell.GetColor(theme.shell)))
	lines := []string{fmt.Sprintf("[%s::b]%s[-::-]", colorInk, logo)}
	for _, l := range tview.WordWrap("Make the terminal a little bigger.", max(width, 1)) {
		lines = append(lines, fmt.Sprintf("[%s]%s[-]", colorMuted, l))
	}
	for i, l := range lines {
		tview.Print(screen, l, 0, height/2-1+i, width, tview.AlignCenter, tcell.GetColor(colorInk))
	}
}
