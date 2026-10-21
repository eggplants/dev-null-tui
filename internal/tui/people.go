package tui

import (
	"context"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// peopleList is a list of people, like a profile's followers, that opens their profiles and follows them.
type peopleList struct {
	*pager[*api.User]

	// keys handles the keys the owner adds, returning nil for those it used.
	keys func(event *tcell.EventKey) *tcell.EventKey
	// extraHints are the keys the owner adds, for the status bar.
	extraHints func() []hint
	// undo, when set, takes the selected person off the list, as unmuting does in the list of who the user mutes.
	undo *peopleAction
}

// peopleAction is something done to the selected person that takes them off the list.
type peopleAction struct {
	key   rune
	label string
	do    func(ctx context.Context, username string) error
	done  string
}

func newPeopleList(app *App, title string, empty []string, load func(ctx context.Context, cursor string) ([]*api.User, string, error)) *peopleList {
	l := &peopleList{pager: newPager(app, title, empty, personLines)}
	l.load = load
	l.end = ""
	l.onActivate = func() {
		if u, ok := l.Selected(); ok {
			app.push(newProfile(app, u.Username))
		}
	}

	return l
}

func (l *peopleList) Hints() []hint {
	hints := []hint{{"j/k", "move"}}
	if u, ok := l.Selected(); ok {
		hints = append(hints, hint{"⏎", "profile"})
		if l.undo != nil {
			hints = append(hints, hint{string(l.undo.key), l.undo.label})
		} else if l.app.me == nil || u.ID != l.app.me.ID {
			if u.IsFollowed != nil && *u.IsFollowed {
				hints = append(hints, hint{"f", "unfollow"})
			} else {
				hints = append(hints, hint{"f", "follow"})
			}
		}
	}
	if l.extraHints != nil {
		hints = append(hints, l.extraHints()...)
	}

	return hints
}

func (l *peopleList) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	list := l.listView.InputHandler()

	return func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		if l.keys != nil {
			if event = l.keys(event); event == nil {
				return
			}
		}

		u, ok := l.Selected()
		switch {
		case !ok:
		case event.Key() == tcell.KeyEnter || event.Key() == tcell.KeyRune && event.Rune() == 'l':
			l.app.push(newProfile(l.app, u.Username))
			return
		case l.undo != nil && event.Key() == tcell.KeyRune && event.Rune() == l.undo.key:
			l.takeOff(u)
			return
		case l.undo == nil && event.Key() == tcell.KeyRune && event.Rune() == 'f':
			l.follow(u)
			return
		}

		list(event, setFocus)
	}
}

// follow follows or unfollows the selected person, right from the list.
func (l *peopleList) follow(u *api.User) {
	if l.app.me != nil && u.ID == l.app.me.ID {
		return
	}

	follow := u.IsFollowed == nil || !*u.IsFollowed
	l.app.act(func(ctx context.Context) error {
		return l.app.client.Follow(ctx, u.Username, follow)
	}, func(err error) {
		if err != nil {
			l.app.flashError(err)
			return
		}
		u.IsFollowed = &follow
		l.Invalidate()
		if follow {
			l.app.succeed("Following @%s.", u.Username)
		} else {
			l.app.succeed("Unfollowed @%s.", u.Username)
		}
	})
}

func (l *peopleList) takeOff(u *api.User) {
	l.app.act(func(ctx context.Context) error {
		return l.undo.do(ctx, u.Username)
	}, func(err error) {
		if err != nil {
			l.app.flashError(err)
			return
		}
		for i, item := range l.items {
			if item == u {
				l.Remove(i)
			}
		}
		l.app.succeed(l.undo.done, u.Username)
	})
}
