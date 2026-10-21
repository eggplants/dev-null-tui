package tui

import (
	"context"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
)

type notifications struct {
	*pager[*api.Notification]

	// typ is the kind the list is filtered to: "all" or one of api.NotificationTypes.
	typ string
	// group is the group this list expands, if it does.
	group *api.Notification
}

// newNotifications lists the notification groups. Loading them marks them read, as opening the page does on the web.
func newNotifications(app *App) *notifications {
	n := &notifications{typ: "all"}
	n.pager = newPager(app, "Notifications", nil, notificationLines)
	n.end = ""
	n.setEmpty()
	n.load = func(ctx context.Context, cursor string) ([]*api.Notification, string, error) {
		page, err := app.client.Notifications(ctx, cursor, n.typ)
		if err != nil {
			return nil, "", err
		}

		return pointers(page.Data), page.NextCursor, nil
	}
	n.afterReload = func() {
		app.unread = 0
		app.drawHeader()
	}
	n.onActivate = func() {
		if item, ok := n.Selected(); ok {
			n.open(item)
		}
	}

	return n
}

// newNotificationGroup lists everyone and everything a grouped notification stands for, like "and 4 others".
func newNotificationGroup(app *App, group *api.Notification, typ string) *notifications {
	n := &notifications{typ: typ, group: group}
	n.pager = newPager(app, "Group", emptyState("nothing in this group"), notificationLines)
	n.end = ""
	n.load = func(ctx context.Context, cursor string) ([]*api.Notification, string, error) {
		page, err := app.client.NotificationGroup(ctx, group.ID, cursor, typ)
		if err != nil {
			return nil, "", err
		}

		return pointers(page.Data), page.NextCursor, nil
	}
	n.onActivate = func() {
		if item, ok := n.Selected(); ok {
			n.open(item)
		}
	}

	return n
}

func (n *notifications) setEmpty() {
	if n.typ == "all" {
		n.empty = emptyState("no notifications yet", "$ cat /dev/notifications")
		n.title = "Notifications"
		return
	}

	n.empty = emptyState("no "+n.typ+" here", "$ cat /dev/notifications | grep "+n.typ)
	n.title = "Notifications · " + strings.ToUpper(n.typ[:1]) + n.typ[1:]
}

func (n *notifications) Hints() []hint {
	hints := []hint{{"j/k", "move"}}
	if item, ok := n.Selected(); ok {
		hints = append(hints, hint{"⏎", "open"})
		if item.Actor != nil {
			hints = append(hints, hint{"u", "profile"})
		}
		if n.group == nil && (item.ActorsCount > 1 || item.EventCount > 1) {
			hints = append(hints, hint{"e", "everyone"})
		}
	}
	if n.group == nil {
		hints = append(hints, hint{"f", "filter: " + n.typ})
	}

	return append(hints, hint{"R", "refresh"})
}

func (n *notifications) eachCommit(f func(c *api.Commit)) {
	for _, item := range n.items {
		if item.Commit != nil {
			f(item.Commit)
		}
	}
}

func (n *notifications) removeCommit(id string) {
	for i := len(n.items) - 1; i >= 0; i-- {
		if c := n.items[i].Commit; c != nil && c.ID == id {
			n.Remove(i)
		}
	}
}

func (n *notifications) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	list := n.listView.InputHandler()

	return func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		if event.Key() == tcell.KeyRune && event.Rune() == 'f' && n.group == nil {
			n.nextFilter()
			return
		}

		item, ok := n.Selected()
		switch {
		case !ok:
		case event.Key() == tcell.KeyEnter || event.Key() == tcell.KeyRune && event.Rune() == 'l':
			n.open(item)
			return
		case event.Key() == tcell.KeyRune && event.Rune() == 'u' && item.Actor != nil:
			n.app.push(newProfile(n.app, item.Actor.Username))
			return
		case event.Key() == tcell.KeyRune && event.Rune() == 'e' && n.group == nil && (item.ActorsCount > 1 || item.EventCount > 1):
			n.app.push(newNotificationGroup(n.app, item, n.typ))
			return
		}

		list(event, setFocus)
	}
}

// nextFilter moves the filter on to the next kind of notification, and back to all of them after the last.
func (n *notifications) nextFilter() {
	types := append([]string{"all"}, api.NotificationTypes...)
	for i, t := range types {
		if t == n.typ {
			n.typ = types[(i+1)%len(types)]
			break
		}
	}
	n.setEmpty()
	n.Reload()
}

// open opens what a notification is about: the commit, or for a follow the follower's profile.
func (n *notifications) open(item *api.Notification) {
	switch {
	case item.Commit != nil:
		n.app.push(newThread(n.app, item.Commit.ID))
	case item.Actor != nil:
		n.app.push(newProfile(n.app, item.Actor.Username))
	}
}
