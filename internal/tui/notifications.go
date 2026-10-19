package tui

import (
	"context"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
)

type notifications struct {
	*pager[*api.Notification]
}

// newNotifications lists the notification groups. Loading them marks them read, as opening the page does on the web.
func newNotifications(app *App) *notifications {
	n := &notifications{pager: newPager(app, "Notifications", "No notifications yet.", notificationLines)}
	n.load = func(ctx context.Context, cursor string) ([]*api.Notification, string, error) {
		page, err := app.client.Notifications(ctx, cursor)
		if err != nil {
			return nil, "", err
		}

		return pointers(page.Data), page.NextCursor, nil
	}
	n.afterReload = func() {
		app.unread = 0
		app.drawHeader()
	}

	return n
}

func (n *notifications) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	list := n.listView.InputHandler()

	return func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		item, ok := n.Selected()
		if ok && (event.Key() == tcell.KeyEnter || event.Key() == tcell.KeyRune && event.Rune() == 'l') {
			n.open(item)
			return
		}
		if ok && event.Key() == tcell.KeyRune && event.Rune() == 'u' && item.Actor != nil {
			n.app.push(newProfile(n.app, item.Actor.Username))
			return
		}

		list(event, setFocus)
	}
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
