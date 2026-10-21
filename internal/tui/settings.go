package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// setting is a row of the settings: what it is, what it's set to, and what Enter, ← and → do on it.
type setting struct {
	label string
	value func() string
	help  string
	// enter runs on Enter, and left and right on ← and →, if set.
	enter       func()
	left, right func()
}

// settingsView lets the user pick their color, pastéis mode and theme, and see who they mute and block.
type settingsView struct {
	*listView[*setting]

	app *App
	// lane is the lane picked with ← and →, before Enter saves it: -1 is the automatic one.
	lane int
	// saveTheme keeps the theme picked for the next run, when the app can.
	saveTheme func(name string) error
}

func newSettings(app *App) *settingsView {
	s := &settingsView{app: app, lane: -1, saveTheme: app.saveTheme}
	if app.me != nil && app.me.PreferredLane != nil {
		s.lane = *app.me.PreferredLane
	}

	s.listView = newListView(s.render)
	s.SetItems([]*setting{
		{
			label: "Color",
			value: s.laneValue,
			help:  "← → pick a lane, ⏎ save it, # a color of your own",
			left:  func() { s.moveLane(-1) },
			right: func() { s.moveLane(1) },
			enter: s.saveLane,
		},
		{
			label: "Pastéis mode",
			value: func() string { return onOff(app.me != nil && app.me.PasteisMode) },
			help:  "⏎ turns it on or off: 10 pastéis a day to give, and their counts shown",
			enter: s.togglePasteis,
		},
		{
			label: "Theme",
			value: func() string { return app.opts.Theme },
			help:  "⏎ cycles auto, dark and light; auto follows the terminal",
			enter: s.cycleTheme,
		},
		{
			label: "Profile",
			value: func() string {
				if app.me == nil || app.me.SyncedAt == nil {
					return "from GitHub"
				}
				return "synced " + relativeTime(*app.me.SyncedAt) + " ago"
			},
			help:  "⏎ takes your name, bio and links from GitHub again",
			enter: s.syncGitHub,
		},
		{
			label: "Muted",
			value: func() string { return "people whose commits you don't see" },
			help:  "⏎ lists them",
			enter: func() { app.push(newSafetyList(app, true)) },
		},
		{
			label: "Blocked",
			value: func() string { return "people who can't see or answer you" },
			help:  "⏎ lists them",
			enter: func() { app.push(newSafetyList(app, false)) },
		},
		{
			label: "Server",
			value: func() string { return app.client.BaseURL },
			help:  "dn login -base-url picks another",
		},
	})
	s.onActivate = func() {
		if item, ok := s.Selected(); ok && item.enter != nil {
			item.enter()
		}
	}

	return s
}

func onOff(on bool) string {
	if on {
		return "on"
	}

	return "off"
}

func (s *settingsView) Title() string { return "Settings" }
func (s *settingsView) Reload()       { s.Invalidate() }

func (s *settingsView) Hints() []hint {
	hints := []hint{{"j/k", "move"}}
	if item, ok := s.Selected(); ok {
		if item.left != nil {
			hints = append(hints, hint{"← →", "pick"})
		}
		if item.enter != nil {
			hints = append(hints, hint{"⏎", "change"})
		}
		if item.label == "Color" {
			hints = append(hints, hint{"#", "own color"})
		}
	}

	return hints
}

func (s *settingsView) render(item *setting, selected bool, width int) []string {
	bar := " "
	if selected {
		bar = fmt.Sprintf("[%s]▌[-]", colorPrimary)
	}

	head := fmt.Sprintf("%s [%s::b]%-14s[-::-] %s", bar, colorInk, item.label, item.value())
	lines := []string{head}
	if selected && item.help != "" {
		lines = append(lines, wrap(fmt.Sprintf("[%s]%s[-]", colorMuted, item.help), bar+"                ", width)...)
	}

	return append(lines, "")
}

// laneValue shows the six lanes and the automatic one, the picked one marked.
func (s *settingsView) laneValue() string {
	var b strings.Builder
	for i, color := range theme.lanes {
		mark := "●"
		if i == s.lane {
			mark = "◉"
		}
		fmt.Fprintf(&b, "[%s]%s[-] ", color, mark)
	}

	auto := "automatic"
	if s.app.me != nil {
		auto += fmt.Sprintf(" ([%s]●[-])", theme.lanes[s.app.me.AutomaticLane%len(theme.lanes)])
	}
	if s.lane == -1 {
		fmt.Fprintf(&b, "[%s::u]%s[-::-][::U]", colorInk, auto)
	} else {
		fmt.Fprintf(&b, "[%s]%s[-]", colorMuted, auto)
	}

	if s.app.me != nil {
		var custom string
		if json.Unmarshal(s.app.me.Lane, &custom) == nil && custom != "" {
			fmt.Fprintf(&b, "  [%s]now %s[-]", custom, tview.Escape(custom))
		}
	}

	return b.String()
}

func (s *settingsView) moveLane(by int) {
	// -1 is automatic, after lane 5.
	s.lane = (s.lane+1+by+7)%7 - 1
	s.Invalidate()
}

func (s *settingsView) saveLane() {
	var lane *int
	if s.lane >= 0 {
		l := s.lane
		lane = &l
	}

	s.saveMe(func(ctx context.Context) (*api.CurrentUser, error) { return s.app.client.SetLane(ctx, lane) }, "Saved your color.")
}

func (s *settingsView) askColor() {
	input := tview.NewInputField().SetLabel("Your color: ").SetPlaceholder("#3a88fe").SetFieldWidth(9)
	input.SetBorder(true).SetTitle(" A color of your own ")
	input.SetDoneFunc(func(key tcell.Key) {
		s.app.closeModal("color")
		if key != tcell.KeyEnter {
			return
		}
		color := strings.ToLower(strings.TrimSpace(input.GetText()))
		if !strings.HasPrefix(color, "#") {
			color = "#" + color
		}
		if !hexColor.MatchString(color) {
			s.app.fail("A color is # and six hex digits, like #3a88fe.")
			return
		}
		s.saveMe(func(ctx context.Context) (*api.CurrentUser, error) { return s.app.client.SetColor(ctx, color) }, "Saved your color.")
	})
	s.app.openModal("color", centered(input, 30, 3))
}

func (s *settingsView) togglePasteis() {
	on := s.app.me == nil || !s.app.me.PasteisMode
	done := "Pastéis mode is off."
	if on {
		done = "Pastéis mode is on: P gives a pastel."
	}
	s.saveMe(func(ctx context.Context) (*api.CurrentUser, error) { return s.app.client.SetPasteisMode(ctx, on) }, done)
}

func (s *settingsView) syncGitHub() {
	s.saveMe(s.app.client.SyncGitHub, "Took your profile from GitHub again.")
}

// saveMe changes something about the user and takes their new state from the answer, redrawing everything their
// color or pastéis mode shows on.
func (s *settingsView) saveMe(action func(ctx context.Context) (*api.CurrentUser, error), done string) {
	var me *api.CurrentUser
	s.app.act(func(ctx context.Context) (err error) {
		me, err = action(ctx)
		return err
	}, func(err error) {
		if err != nil {
			s.app.flashError(err)
			return
		}
		s.app.me = &me.Data
		showPasteis = me.Data.PasteisMode
		if me.Data.PreferredLane != nil {
			s.lane = *me.Data.PreferredLane
		}
		s.app.recolorMe()
		s.app.succeed("%s", done)
	})
}

func (s *settingsView) cycleTheme() {
	next := map[string]string{"auto": "dark", "dark": "light", "light": "auto"}[s.app.opts.Theme]
	if next == "" {
		next = "auto"
	}
	s.app.opts.Theme = next

	p := darkPalette
	switch next {
	case "light":
		p = lightPalette
	case "auto":
		p = s.app.autoTheme
	}
	s.app.setTheme(p)

	if s.saveTheme != nil {
		if err := s.saveTheme(next); err != nil {
			s.app.fail("Couldn't keep the theme for next time: %s", tview.Escape(err.Error()))
			return
		}
	}
	s.app.succeed("Theme: %s.", next)
}

func (s *settingsView) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	list := s.listView.InputHandler()

	return func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		item, ok := s.Selected()
		switch {
		case !ok:
		case event.Key() == tcell.KeyEnter && item.enter != nil:
			item.enter()
			return
		// h is back everywhere, so only the arrows pick.
		case event.Key() == tcell.KeyLeft && item.left != nil:
			item.left()
			return
		case event.Key() == tcell.KeyRight && item.right != nil:
			item.right()
			return
		case event.Key() == tcell.KeyRune && event.Rune() == '#':
			s.askColor()
			return
		}

		list(event, setFocus)
	}
}

// newSafetyList lists the people the user mutes, or blocks, with the key that takes each off the list.
func newSafetyList(app *App, muted bool) *peopleList {
	load, title := app.client.Blocked, "Blocked"
	empty := emptyState("you block nobody", "B on a profile blocks someone.")
	undo := &peopleAction{key: 'B', label: "unblock", done: "Unblocked @%s.", do: func(ctx context.Context, u string) error { return app.client.Block(ctx, u, false) }}
	if muted {
		load, title = app.client.Muted, "Muted"
		empty = emptyState("you mute nobody", "M on a profile mutes someone: their commits stay out of your logs.")
		undo = &peopleAction{key: 'M', label: "unmute", done: "Unmuted @%s.", do: func(ctx context.Context, u string) error { return app.client.Mute(ctx, u, false) }}
	}

	l := newPeopleList(app, title, empty, func(ctx context.Context, cursor string) ([]*api.User, string, error) {
		page, err := load(ctx, cursor)
		if err != nil {
			return nil, "", err
		}

		return pointers(page.Data), page.NextCursor, nil
	})
	l.undo = undo

	return l
}
