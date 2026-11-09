package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// newInbox is the Remotes tab: the user's remotes and orgs, Open first and Stashed after, with the requests waiting to be
// fetched when there are any, as the web's Remotes page has them.
func newInbox(app *App) *tabbedView {
	t := newTabbedView(app, "Remotes")
	t.head = func(width int) []string {
		if app.me != nil && app.me.Remotes == nil {
			return []string{fmt.Sprintf(" [%s]Remotes are off: nobody can message you. g s turns them on.[-]", colorWarning), ""}
		}
		return nil
	}
	t.keys = func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() != tcell.KeyRune {
			return event
		}
		switch event.Rune() {
		case 'n':
			app.newRemote()
		case 'N':
			app.newOrg()
		default:
			return event
		}
		return nil
	}
	t.hints = func() []hint { return []hint{{"n", "new remote"}, {"N", "new org"}} }

	t.addConversations("Open", newConversations(app, api.InboxOpen, emptyState("no conversations yet", "n pushes to someone, N starts an org.")))
	// Requests from people the user doesn't follow are only fetched where the server has them on.
	if app.me != nil && app.me.Remotes != nil && app.me.Remotes.Requests {
		t.addConversations("Fetched", newConversations(app, api.InboxFetched, emptyState("no requests", "People you don't follow land here first.")))
	}
	t.addConversations("Stashed", newConversations(app, api.InboxStashed, emptyState("nothing stashed", "S stashes a conversation until someone writes in it.")))

	return t
}

// addConversations adds a tab of conversations.
func (t *tabbedView) addConversations(name string, l *conversations) {
	l.header = t.header
	l.headerOverlay = t.overlayHead
	l.keys = t.tabKeys
	l.extraHints = t.tabHints
	t.add(name, l)
}

// conversations are a tab of the inbox.
type conversations struct {
	*pager[*api.Conversation]

	tab string
	// keys and extraHints are what the inbox adds on every tab.
	keys       func(event *tcell.EventKey) *tcell.EventKey
	extraHints func() []hint
}

func newConversations(app *App, tab string, empty []string) *conversations {
	l := &conversations{tab: tab}
	l.pager = newPager(app, "Remotes", empty, func(c *api.Conversation, selected bool, width int) []string {
		return conversationLines(app, c, selected, width)
	})
	l.end = ""
	l.load = func(ctx context.Context, cursor string) ([]*api.Conversation, string, error) {
		inbox, err := app.client.Inbox(ctx, tab, "", cursor)
		if err != nil {
			return nil, "", err
		}
		if tab == api.InboxOpen {
			unread := inbox.UnreadCount
			app.tv.QueueUpdate(func() { app.setRemotesUnread(unread) })
		}
		next := ""
		if inbox.HasMore && len(inbox.Data) > 0 {
			next = inbox.Data[len(inbox.Data)-1].ID
		}

		return pointers(inbox.Data), next, nil
	}
	l.overlay = func(screen tcell.Screen, c *api.Conversation, top, x, y, width, height int) {
		if u := conversationFace(c); u != nil && top >= y && top < y+height {
			app.drawAvatar(screen, u, x+1, top, avatarCols, 1, y, height)
		}
	}
	l.onActivate = func() {
		if c, ok := l.Selected(); ok {
			app.push(newRemoteThread(app, remoteOf(c), conversationName(c)))
		}
	}

	return l
}

// remoteOf names the conversation to the API.
func remoteOf(c *api.Conversation) api.Remote {
	if c.Kind == api.ConversationOrg {
		return api.Remote{OrgID: c.ID}
	}
	if c.Partner != nil {
		return api.Remote{Handle: c.Partner.Username}
	}

	return api.Remote{}
}

// conversationFace is whose avatar stands for a conversation: the partner's, or an org's first member's.
func conversationFace(c *api.Conversation) *api.User {
	if c.Partner != nil {
		return c.Partner
	}
	if len(c.Members) > 0 {
		return &c.Members[0].User
	}

	return nil
}

// conversationName is what a conversation is called: the partner's name, or the org's, or its members'.
func conversationName(c *api.Conversation) string {
	if c.Partner != nil {
		return "@" + c.Partner.Username
	}
	if c.Name != "" {
		return c.Name
	}

	names := make([]string, 0, len(c.Members))
	for _, m := range c.Members {
		names = append(names, "@"+m.User.Username)
	}
	if len(names) == 0 {
		return "org"
	}

	return strings.Join(names, ", ")
}

func conversationLines(app *App, c *api.Conversation, selected bool, width int) []string {
	bar := " "
	if selected {
		bar = fmt.Sprintf("[%s]▌[-]", colorPrimary)
	}

	name := tview.Escape(firstLine(conversationName(c), max(width-30, 10)))
	color := colorInk
	if c.Partner != nil {
		name = authorName(c.Partner)
	} else {
		name = fmt.Sprintf("[%s::b]%s[-::-] [%s]· org of %d[-]", colorInk, name, colorMuted, len(c.Members)+1)
	}
	var marks []string
	if c.PinnedAt != nil {
		marks = append(marks, "◇")
	}
	if c.MutedUntil != nil && c.MutedUntil.After(time.Now()) {
		marks = append(marks, "muted")
	}
	if c.LastMessageAt != nil {
		marks = append(marks, relativeTime(*c.LastMessageAt))
	}
	right := fmt.Sprintf("[%s]%s[-]", colorMuted, strings.Join(marks, " · "))
	if c.Ahead > 0 {
		right += fmt.Sprintf(" [%s:%s:b]%d[-:-:-]", theme.popover, colorPrimary, c.Ahead)
	}

	head := bar + avatarRoom + name
	lines := []string{spread(head, right+" ", width)}

	preview := fmt.Sprintf("[%s::i]no messages yet[-::-]", colorFaint)
	if m := c.LastMessage; m != nil {
		who := ""
		if app.me != nil && m.UserID == app.me.ID {
			who = "you: "
		} else if c.Kind == api.ConversationOrg && m.User != nil {
			who = "@" + m.User.Username + ": "
		}
		preview = fmt.Sprintf("[%s]%s%s[-]", colorMuted, tview.Escape(who), tview.Escape(firstLine(messageSummary(m), width)))
		if c.Ahead > 0 {
			color = colorInk
			preview = fmt.Sprintf("[%s]%s%s[-]", color, tview.Escape(who), tview.Escape(firstLine(messageSummary(m), width)))
		}
	}
	lines = append(lines, truncateLines(wrap(preview, bar+avatarRoom, width), 1)...)

	return append(lines, "")
}

// messageSummary is a message in a line: its words, or what it carries.
func messageSummary(m *api.Message) string {
	switch {
	case m.Event != nil:
		return eventText(m)
	case m.Body != "":
		return strings.Join(strings.Fields(m.Body), " ")
	case len(m.Images) > 0:
		return fmt.Sprintf("▣ %d image%s", len(m.Images), plural(len(m.Images)))
	case m.Voice != nil:
		return "♪ voice note"
	}

	return ""
}

func (l *conversations) Hints() []hint {
	hints := []hint{{"j/k", "move"}}
	if c, ok := l.Selected(); ok {
		hints = append(hints, hint{"⏎", "open"})
		if l.tab == api.InboxFetched {
			hints = append(hints, hint{"a", "accept"}, hint{"x", "drop"})
		} else {
			if c.IsArchived {
				hints = append(hints, hint{"S", "unstash"})
			} else {
				hints = append(hints, hint{"S", "stash"})
			}
			hints = append(hints, hint{"m", "mute"})
			if c.PinnedAt != nil {
				hints = append(hints, hint{"p", "unpin"})
			} else {
				hints = append(hints, hint{"p", "pin"})
			}
		}
	}
	if l.extraHints != nil {
		hints = append(hints, l.extraHints()...)
	}

	return hints
}

func (l *conversations) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	list := l.listView.InputHandler()

	return func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		if l.keys != nil {
			if event = l.keys(event); event == nil {
				return
			}
		}
		c, ok := l.Selected()
		if ok && event.Key() == tcell.KeyEnter {
			l.onActivate()
			return
		}
		if ok && event.Key() == tcell.KeyRune && l.conversationKey(c, event.Rune()) {
			return
		}
		list(event, setFocus)
	}
}

// conversationKey does what a key does to the selected conversation, telling whether it did anything.
func (l *conversations) conversationKey(c *api.Conversation, r rune) bool {
	app, remote := l.app, remoteOf(c)
	if l.tab == api.InboxFetched {
		switch {
		case r == 'a' && c.Partner != nil:
			l.takeOff(c, func(ctx context.Context) error { return app.client.Merge(ctx, c.Partner.Username) },
				fmt.Sprintf("Accepted @%s: it's in Open now.", c.Partner.Username))
		case r == 'x' && c.Partner != nil:
			app.confirm(fmt.Sprintf("Drop the request from @%s for good? They aren't told.", c.Partner.Username), func() {
				l.takeOff(c, func(ctx context.Context) error { return app.client.Drop(ctx, c.Partner.Username) },
					fmt.Sprintf("Dropped @%s's request.", c.Partner.Username))
			})
		default:
			return r == 'a' || r == 'x'
		}
		return true
	}

	switch r {
	case 'S':
		stash := !c.IsArchived
		done := "Stashed it until someone writes in it."
		if !stash {
			done = "It's back in Open."
		}
		l.takeOff(c, func(ctx context.Context) error { return app.client.Stash(ctx, remote, stash) }, done)
	case 'm':
		app.muteConversation(remote, c.MutedUntil != nil && c.MutedUntil.After(time.Now()), func() { l.Reload() })
	case 'p':
		pin := c.PinnedAt == nil
		app.act(func(ctx context.Context) error { return app.client.PinConversation(ctx, remote, pin) }, func(err error) {
			if err != nil {
				app.flashError(err)
				return
			}
			l.Reload()
		})
	default:
		return false
	}

	return true
}

// takeOff does something that moves a conversation to another tab, and takes it off this one.
func (l *conversations) takeOff(c *api.Conversation, action func(ctx context.Context) error, done string) {
	l.app.act(action, func(err error) {
		if err != nil {
			l.app.flashError(err)
			return
		}
		for i, item := range l.items {
			if item == c {
				l.Remove(i)
			}
		}
		l.app.reloadInbox(l)
		l.app.succeed("%s", done)
	})
}

// reloadInbox has every tab of the inbox but except load again.
func (a *App) reloadInbox(except view) {
	a.views(func(v view) {
		if t, ok := v.(*tabbedView); ok {
			for _, tab := range t.tabs {
				if c, ok := tab.(*conversations); ok && c != except {
					c.forget()
				}
			}
		}
	})
}

// muteConversation asks how long to mute a conversation for, or unmutes it.
func (a *App) muteConversation(r api.Remote, muted bool, done func()) {
	labels := make([]string, 0, len(api.MuteDurations)+2)
	if muted {
		labels = append(labels, "Unmute")
	}
	for _, d := range api.MuteDurations {
		labels = append(labels, d.Label)
	}
	labels = append(labels, "Cancel")

	a.choose("Mute it, for you only?", labels, func(label string) {
		duration := ""
		if label == "Unmute" {
			duration = "off"
		}
		for _, d := range api.MuteDurations {
			if d.Label == label {
				duration = d.ID
			}
		}
		if duration == "" {
			return
		}
		a.act(func(ctx context.Context) error { return a.client.MuteConversation(ctx, r, duration) }, func(err error) {
			if err != nil {
				a.flashError(err)
				return
			}
			if duration == "off" {
				a.succeed("Unmuted it.")
			} else {
				a.succeed("Muted it: %s.", strings.ToLower(label))
			}
			done()
		})
	})
}

// setRemotesUnread shows how many conversations have unread messages on the Remotes tab.
func (a *App) setRemotesUnread(n int) {
	if a.remotesUnread != n {
		a.remotesUnread = n
		a.drawHeader()
	}
}

// newRemote asks whom to push to, finding them as the web's New remote does, and opens the remote with them.
func (a *App) newRemote() {
	a.pickCandidate("New remote", "remote", "", func(u *api.User) {
		a.push(newRemoteThread(a, api.Remote{Handle: u.Username}, "@"+u.Username))
	})
}

// pickCandidate asks for someone by name, completing it from the people who can be messaged or added for purpose,
// and runs picked with them.
func (a *App) pickCandidate(title, purpose, org string, picked func(u *api.User)) {
	// found are the people each entry stands for, and asked the entries for each term, both only read and written
	// on the UI goroutine, where tview completes.
	found := map[string]*api.User{}
	asked := map[string][]string{}
	var input *lineInput
	input = a.askLine(title, "a name or @handle", "", func(text string) []string {
		q := strings.TrimPrefix(strings.TrimSpace(text), "@")
		if q == "" {
			return nil
		}
		if entries, ok := asked[q]; ok {
			return entries
		}
		// The people are asked for off the UI goroutine, a moment after the last key, and completed once they come.
		time.AfterFunc(mentionDelay, func() {
			a.tv.QueueUpdate(func() {
				if current := strings.TrimPrefix(strings.TrimSpace(input.field.GetText()), "@"); current != q {
					return
				}
				var candidates []api.Candidate
				a.async(func(ctx context.Context) (err error) {
					candidates, err = a.client.Candidates(ctx, q, purpose, org)
					return err
				}, func(err error) {
					if err != nil {
						return
					}
					var entries []string
					for i := range candidates {
						c := &candidates[i]
						entry := "@" + c.User.Username
						if c.User.Name != "" && c.User.Name != c.User.Username {
							entry += " (" + c.User.Name + ")"
						}
						if c.Reason != nil {
							entry += " — " + *c.Reason
						}
						found[entry] = &c.User
						entries = append(entries, entry)
					}
					asked[q] = entries[:min(len(entries), 8)]
					input.field.Autocomplete()
				})
			})
		})
		return nil
	})
	input.done = func(text string) {
		text = strings.TrimSpace(text)
		if u, ok := found[text]; ok {
			if strings.Contains(text, " — ") {
				a.fail("%s", tview.Escape(text[strings.Index(text, " — ")+len(" — "):]))
				return
			}
			picked(u)
			return
		}
		handle := strings.TrimPrefix(strings.Fields(text + " ")[0], "@")
		if handle == "" {
			return
		}
		picked(&api.User{Username: handle})
	}
}

// newOrg asks for the people to start an org with, and a name for it, and opens it.
func (a *App) newOrg() {
	input := a.askLine("New org: who's in it", "@ada @grace @linus", "", nil)
	input.done = func(text string) {
		var members []string
		for _, f := range strings.Fields(strings.ReplaceAll(text, ",", " ")) {
			if h := strings.TrimPrefix(f, "@"); h != "" {
				members = append(members, h)
			}
		}
		if len(members) == 0 {
			a.fail("An org needs at least one other person.")
			return
		}
		name := a.askLine("Name it", "optional: empty names it after its members", "", nil)
		name.done = func(text string) {
			title := strings.TrimSpace(text)
			var id string
			a.act(func(ctx context.Context) (err error) {
				id, err = a.client.CreateOrg(ctx, title, members)
				return err
			}, func(err error) {
				if err != nil {
					a.flashError(err)
					return
				}
				if title == "" {
					title = "@" + strings.Join(members, ", @")
				}
				a.reloadInbox(nil)
				a.push(newRemoteThread(a, api.Remote{OrgID: id}, title))
			})
		}
	}
}
