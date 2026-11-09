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

// remoteEvery is how often an open remote asks for new messages, about as often as the web polls one.
var remoteEvery = 4 * time.Second

// remoteThread is a remote or an org: its messages, oldest at the top and the newest at the bottom, as a chat has them.
type remoteThread struct {
	*listView[*api.Message]

	app    *App
	remote api.Remote
	title  string
	// thread is the last page read, for what it says of the conversation: who is in it, how far they read, whether
	// the user can write.
	thread *api.RemoteThread

	hasOlder bool
	loading  bool
	loaded   bool
	// newest and reactions are how far the messages and their reactions were read, to ask only for what changed.
	newest, reactions int64
	// readThrough is how far the user read, as the server knows it.
	readThrough int64
	// generation tells answers to an earlier Reload apart, and polling whether the thread polls for new messages.
	generation int
	polling    bool
	// asking are the messages whose link cards are being read again.
	asking map[int64]bool
}

func newRemoteThread(app *App, remote api.Remote, title string) *remoteThread {
	t := &remoteThread{app: app, remote: remote, title: title}
	t.listView = newListView(t.render)
	t.divider = t.dayDivider

	return t
}

func (t *remoteThread) Title() string { return t.title }
func (t *remoteThread) Loaded() bool  { return t.loaded }
func (t *remoteThread) Loading() bool { return t.loading }

// Reload reads the latest page of messages, and starts asking for new ones while the thread is open.
func (t *remoteThread) Reload() {
	t.generation++
	generation := t.generation
	t.loading = true
	t.SetFooter(fmt.Sprintf("   [%s]Pulling…[-]", colorMuted))
	t.app.spin()

	var thread *api.RemoteThread
	t.app.async(func(ctx context.Context) (err error) {
		thread, err = t.app.client.Messages(ctx, t.remote, api.ThreadPage{})
		return err
	}, func(err error) {
		if generation != t.generation {
			return
		}
		t.loading = false
		if isNotFound(err) && t.remote.Handle != "" {
			// A remote that hasn't started yet has nothing to read: the first message starts it.
			t.loaded = true
			t.SetItems(nil)
			t.SetFooter(emptyState("nothing here yet", "c pushes the first message.")...)
			return
		}
		if err != nil {
			t.SetFooter(fmt.Sprintf("   [%s]%s[-]", colorDanger, tview.Escape(errorMessage(err))), fmt.Sprintf("   [%s]Press R to try again.[-]", colorMuted))
			t.app.flashError(err)
			return
		}

		t.loaded = true
		t.take(thread)
		t.SetItems(pointers(thread.Messages))
		t.Select(len(t.items) - 1)
		t.updateFooter()
		t.markRead()
		if !t.polling {
			t.polling = true
			go t.poll(remoteEvery)
		}
	})
}

// take keeps what a page of the thread says about the conversation.
func (t *remoteThread) take(thread *api.RemoteThread) {
	t.thread = thread
	t.hasOlder = thread.HasOlder
	t.reactions = max(t.reactions, thread.ReactionsVersion)
	t.readThrough = max(t.readThrough, thread.ReadThroughID)
	for _, m := range thread.Messages {
		t.newest = max(t.newest, m.ID)
	}
	if thread.Org != nil && thread.Org.Name != nil && *thread.Org.Name != "" {
		t.title = *thread.Org.Name
	}
}

func (t *remoteThread) updateFooter() {
	var lines []string
	if t.thread != nil {
		if who := t.typing(); who != "" {
			lines = append(lines, fmt.Sprintf("   [%s::i]%s typing…[-::-]", colorMuted, tview.Escape(who)))
		}
		switch t.thread.Access {
		case "request":
			lines = append(lines, fmt.Sprintf("   [%s]Your first message goes to their requests until they accept it.[-]", colorMuted))
		case "waiting":
			lines = append(lines, fmt.Sprintf("   [%s]Waiting for them to accept your request.[-]", colorMuted))
		case "restricted", "closed":
			lines = append(lines, fmt.Sprintf("   [%s]You can't push to them.[-]", colorWarning))
		}
	}
	if len(t.items) == 0 && len(lines) == 0 {
		lines = emptyState("nothing here yet", "c pushes the first message.")
	}
	t.SetFooter(lines...)
}

// typing says who is typing, if anyone.
func (t *remoteThread) typing() string {
	if t.thread.PartnerTyping && t.thread.Partner != nil {
		return "@" + t.thread.Partner.Username + " is"
	}
	if len(t.thread.Typing) == 1 {
		return "@" + t.thread.Typing[0] + " is"
	}
	if len(t.thread.Typing) > 1 {
		return fmt.Sprintf("%d people are", len(t.thread.Typing))
	}

	return ""
}

// poll asks for new messages and reactions while the thread is on screen, and stops once it's closed.
// The interval is handed in, read on the UI goroutine.
func (t *remoteThread) poll(every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for range ticker.C {
		open, shown := make(chan bool, 1), make(chan bool, 1)
		t.app.tv.QueueUpdate(func() {
			open <- t.app.holds(t)
			shown <- t.app.top() == t && !t.loading
		})
		if !<-open {
			t.app.tv.QueueUpdate(func() { t.polling = false })
			return
		}
		if <-shown {
			t.app.tv.QueueUpdate(func() {
				t.fetchNew()
				t.fetchPendingPreviews()
			})
		}
	}
}

// fetchNew asks for the messages after the newest one read, and the reactions changed since.
func (t *remoteThread) fetchNew() {
	if t.loading || !t.loaded {
		return
	}
	generation, page := t.generation, api.ThreadPage{After: t.newest, ReactionsAfter: t.reactions, WithReactionsLog: true}

	var thread *api.RemoteThread
	t.app.async(func(ctx context.Context) (err error) {
		thread, err = t.app.client.Messages(ctx, t.remote, page)
		t.app.backOff(err)
		return err
	}, func(err error) {
		// Polling happens in the background, so a failure waits for the next try.
		if err != nil || generation != t.generation {
			return
		}
		t.mergeNew(thread)
	})
}

// mergeNew takes in what came since: new messages at the bottom, staying on the newest if the reader was on it, and
// the reactions that changed.
func (t *remoteThread) mergeNew(thread *api.RemoteThread) {
	older := t.hasOlder
	atBottom := t.selected >= len(t.items)-1
	t.take(thread)
	t.hasOlder = older

	byID := map[int64]*api.Message{}
	for _, m := range t.items {
		byID[m.ID] = m
	}
	for _, change := range thread.ReactionChanges {
		if m, ok := byID[change.ID]; ok {
			m.Reactions = change.Reactions
		}
	}

	var added []*api.Message
	for i := range thread.Messages {
		m := &thread.Messages[i]
		if old, ok := byID[m.ID]; ok {
			*old = *m
			continue
		}
		added = append(added, m)
	}
	if len(added) > 0 {
		t.Append(added)
		if atBottom {
			t.Select(len(t.items) - 1)
		}
		t.markRead()
	}
	t.updateFooter()
	t.Invalidate()
}

// fetchPendingPreviews reads again the link cards still being made, as the web does while they are.
func (t *remoteThread) fetchPendingPreviews() {
	for _, m := range t.items {
		if m.LinkPreview == nil || !m.LinkPreview.IsPending || t.asking[m.ID] {
			continue
		}
		m := m
		if t.asking == nil {
			t.asking = map[int64]bool{}
		}
		t.asking[m.ID] = true
		var lp *api.LinkPreview
		t.app.async(func(ctx context.Context) (err error) {
			lp, err = t.app.client.MessageLinkPreview(ctx, m.ID)
			return err
		}, func(err error) {
			delete(t.asking, m.ID)
			if err != nil {
				return
			}
			// A card that couldn't be made goes away.
			m.LinkPreview = lp
			t.Invalidate()
		})
	}
}

// loadOlder reads the page before the oldest message on screen, keeping the selection where it was.
func (t *remoteThread) loadOlder() {
	if !t.hasOlder || t.loading || len(t.items) == 0 {
		return
	}
	t.loading = true
	generation, before := t.generation, t.items[0].ID

	var thread *api.RemoteThread
	t.app.async(func(ctx context.Context) (err error) {
		thread, err = t.app.client.Messages(ctx, t.remote, api.ThreadPage{Before: before})
		return err
	}, func(err error) {
		if generation != t.generation {
			return
		}
		t.loading = false
		if err != nil {
			t.app.flashError(err)
			return
		}
		t.hasOlder = thread.HasOlder
		older := pointers(thread.Messages)
		selected := t.selected + len(older)
		t.SetItems(append(older, t.items...))
		t.Select(selected)
	})
}

// markRead tells the server the user read through the newest message, if they haven't yet.
func (t *remoteThread) markRead() {
	if t.newest <= t.readThrough || t.app.top() != t {
		return
	}
	through := t.newest
	t.readThrough = through
	t.app.async(func(ctx context.Context) error {
		return t.app.client.MarkRead(ctx, t.remote, through)
	}, func(err error) {
		if err == nil {
			t.app.reloadInbox(nil)
		}
	})
}

// Select moves to a message, reading the page before once the reader reaches the oldest.
func (t *remoteThread) Select(i int) {
	t.listView.Select(i)
	if t.selected == 0 && t.hasOlder {
		t.loadOlder()
	}
}

// mine tells whether the user pushed m.
func (t *remoteThread) mine(m *api.Message) bool {
	return t.app.me != nil && m.UserID == t.app.me.ID
}

// sender is who pushed a message.
func (t *remoteThread) sender(id string) *api.User {
	if t.app.me != nil && id == t.app.me.ID {
		return &t.app.me.User
	}
	if t.thread != nil {
		if p := t.thread.Partner; p != nil && p.ID == id {
			return p
		}
		for i := range t.thread.Members {
			if t.thread.Members[i].User.ID == id {
				return &t.thread.Members[i].User
			}
		}
	}

	return nil
}

func (t *remoteThread) dayDivider(prev, m *api.Message, width int) []string {
	if m.CreatedAt.IsZero() {
		return nil
	}
	if prev != nil && !prev.CreatedAt.IsZero() && dayLabel(prev.CreatedAt) == dayLabel(m.CreatedAt) {
		return nil
	}

	return []string{dividerLine(dayLabel(m.CreatedAt), width)}
}

func (t *remoteThread) render(m *api.Message, selected bool, width int) []string {
	bar := " "
	if selected {
		bar = fmt.Sprintf("[%s]▌[-]", colorPrimary)
	}
	inner := max(width-3, 10)

	if m.Event != nil {
		return []string{bar + fmt.Sprintf("  [%s::i]%s · %s[-::-]", colorFaint, tview.Escape(eventText(m)), m.CreatedAt.Local().Format("15:04")), ""}
	}

	user := m.User
	if user == nil {
		user = t.sender(m.UserID)
	}
	name := fmt.Sprintf("[%s]unknown[-]", colorMuted)
	if user != nil {
		name = authorName(user)
	}
	head := fmt.Sprintf("%s %s [%s]· %s[-]", bar, name, colorMuted, m.CreatedAt.Local().Format("15:04"))
	if m.PinnedAt != nil {
		head += fmt.Sprintf(" [%s]· ◇ pinned[-]", colorMuted)
	}
	lines := []string{head}

	rail := bar + "  "
	if r := m.ReplyTo; r != nil {
		who := "someone"
		if u := t.sender(r.UserID); u != nil {
			who = "@" + u.Username
		}
		what := strings.Join(strings.Fields(r.Body), " ")
		if what == "" && r.ImagesCount > 0 {
			what = fmt.Sprintf("▣ %d image%s", r.ImagesCount, plural(r.ImagesCount))
		} else if what == "" && r.HasVoice {
			what = "♪ voice note"
		}
		lines = append(lines, rail+fmt.Sprintf("[%s]↪ %s: %s[-]", colorFaint, tview.Escape(who), tview.Escape(firstLine(what, inner-len(who)-4))))
	}
	if m.Body != "" {
		lines = append(lines, prefixed(rail, bodyLines(m.BodySegments, m.Body, m.Mentions, inner))...)
	}
	if n := len(m.Images); n > 0 {
		lines = append(lines, rail+fmt.Sprintf("[%s]▣ %d image%s · i to view[-]", colorMuted, n, plural(n)))
	}
	if v := m.Voice; v != nil {
		d := time.Duration(v.DurationMS) * time.Millisecond
		lines = append(lines, rail+fmt.Sprintf("[%s]♪ voice note %d:%02d · w plays it in the browser[-]", colorMuted, int(d.Minutes()), int(d.Seconds())%60))
	}
	if lp := m.LinkPreview; lp != nil && lp.IsPending {
		lines = append(lines, rail+fmt.Sprintf("[%s]↗ reading the link…[-]", colorFaint))
	} else if lp != nil && lp.Title != "" {
		lines = append(lines, prefixed(rail, boxed(linkPreviewLines(lp, inner-4), inner))...)
	}
	if len(m.Reactions) > 0 {
		lines = append(lines, rail+reactionLine(m.Reactions, t.app.me))
	}
	if t.mine(m) && t.thread != nil && t.thread.Partner != nil && m.ID == t.newestMine() && t.thread.PartnerReadThroughID >= m.ID {
		lines = append(lines, rail+fmt.Sprintf("[%s]✓ pulled[-]", colorMuted))
	}

	return append(lines, "")
}

// newestMine is the newest message the user pushed.
func (t *remoteThread) newestMine() int64 {
	for i := len(t.items) - 1; i >= 0; i-- {
		if t.mine(t.items[i]) {
			return t.items[i].ID
		}
	}

	return 0
}

// reactionLine counts each emoji on a message, the ones the user left in the primary color.
func reactionLine(reactions []api.Reaction, me *api.CurrentUserData) string {
	var order []string
	counts, mine := map[string]int{}, map[string]bool{}
	for _, r := range reactions {
		if counts[r.Emoji] == 0 {
			order = append(order, r.Emoji)
		}
		counts[r.Emoji]++
		if me != nil && r.UserID == me.ID {
			mine[r.Emoji] = true
		}
	}

	parts := make([]string, len(order))
	for i, e := range order {
		color := colorMuted
		if mine[e] {
			color = colorPrimary
		}
		parts[i] = fmt.Sprintf("%s [%s]%d[-]", e, color, counts[e])
	}

	return strings.Join(parts, "  ")
}

// eventText says what happened in an org, as the web's thread says it.
func eventText(m *api.Message) string {
	who, whom := "someone", "someone"
	if m.User != nil {
		who = "@" + m.User.Username
	}
	if m.Subject != nil {
		whom = "@" + m.Subject.Username
	}

	switch *m.Event {
	case "created":
		return who + " started the org"
	case "added":
		return who + " added " + whom
	case "removed":
		return who + " removed " + whom
	case "left":
		return who + " left"
	case "renamed":
		if m.Body != "" {
			return who + " renamed the org to " + m.Body
		}
		return who + " took the org's name away"
	case "promoted":
		return who + " made " + whom + " a maintainer"
	case "demoted":
		return who + " made " + whom + " a contributor"
	case "inherited":
		return who + " maintains the org now"
	case "ping":
		return who + " pinged"
	case "call_started", "call_answered":
		return who + " called"
	case "call_cancelled", "call_declined", "call_missed", "call_failed":
		return "missed call from " + who
	}

	return who + " " + strings.ReplaceAll(*m.Event, "_", " ")
}

// ownsKey says c pushes a message here, not a commit.
func (t *remoteThread) ownsKey(r rune) bool { return r == 'c' }

func (t *remoteThread) Hints() []hint {
	hints := []hint{{"j/k", "move"}, {"c", "push"}}
	if m, ok := t.Selected(); ok && m.Event == nil {
		hints = append(hints, hint{"r", "reply"}, hint{"e", "react"})
		if m.PinnedAt != nil {
			hints = append(hints, hint{"p", "unpin"})
		} else {
			hints = append(hints, hint{"p", "pin"})
		}
		if len(m.Images) > 0 {
			hints = append(hints, hint{"i", "images"})
		}
	}
	hints = append(hints, hint{"P", "ping"})
	if t.remote.OrgID != "" {
		hints = append(hints, hint{"M", "members"})
	}

	return hints
}

func (t *remoteThread) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	list := t.listView.InputHandler()

	return func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		m, ok := t.Selected()
		if ok && m.Event != nil {
			ok = false
		}
		switch {
		case event.Key() == tcell.KeyEnter:
			t.write(nil)
		case event.Key() != tcell.KeyRune:
			list(event, setFocus)
			t.afterMove()
		case event.Rune() == 'c':
			t.write(nil)
		case event.Rune() == 'r' && ok:
			t.write(m)
		case event.Rune() == 'e' && ok:
			t.react(m)
		case event.Rune() == 'p' && ok:
			t.pin(m)
		case event.Rune() == 'i' && ok && len(m.Images) > 0:
			t.app.push(newImageViewer(t.app, m.Images))
		case event.Rune() == 'w' && ok && m.Voice != nil:
			t.app.openURL(m.Voice.URL, true)
		case event.Rune() == 'y' && ok:
			if err := t.app.sys.Copy(m.Body); err != nil {
				t.app.flashError(err)
			} else {
				t.app.succeed("Copied the message.")
			}
		case event.Rune() == 'P':
			t.ping()
		case event.Rune() == 'm':
			muted := t.thread != nil && t.thread.MutedUntil != nil && t.thread.MutedUntil.After(time.Now())
			t.app.muteConversation(t.remote, muted, func() { t.Reload() })
		case event.Rune() == 'M' && t.remote.OrgID != "":
			t.app.push(newOrgMembers(t.app, t))
		default:
			list(event, setFocus)
			t.afterMove()
		}
	}
}

// afterMove reads the page before once the selection reaches the oldest message.
func (t *remoteThread) afterMove() {
	if t.selected == 0 && t.hasOlder {
		t.loadOlder()
	}
}

// pin pins a message to the top of the conversation, or unpins it.
func (t *remoteThread) pin(m *api.Message) {
	pin := m.PinnedAt == nil
	t.app.act(func(ctx context.Context) error { return t.app.client.PinMessage(ctx, m.ID, pin) }, func(err error) {
		if err != nil {
			t.app.flashError(err)
			return
		}
		if pin {
			now := now()
			m.PinnedAt = &now
			t.app.succeed("Pinned the message.")
		} else {
			m.PinnedAt = nil
			t.app.succeed("Unpinned the message.")
		}
		t.Invalidate()
	})
}

// ping pings the partner, or everyone in the org.
func (t *remoteThread) ping() {
	t.app.act(func(ctx context.Context) error { return t.app.client.Ping(ctx, t.remote) }, func(err error) {
		if err != nil {
			t.app.flashError(err)
			return
		}
		t.app.succeed("Pinged %s.", tview.Escape(t.title))
		t.fetchNew()
	})
}

// defaultReactions are offered before the user has reacted with anything.
var defaultReactions = []string{"👍", "❤️", "😂", "🎉", "👀", "🚀"}

// react asks which emoji to leave on a message, from those the user reacts with most, and leaves it, or takes it off
// if they left it already.
func (t *remoteThread) react(m *api.Message) {
	var usage []string
	t.app.act(func(ctx context.Context) (err error) {
		usage, err = t.app.client.ReactionUsage(ctx)
		return err
	}, func(err error) {
		emoji := append([]string{}, usage...)
		for _, e := range defaultReactions {
			if len(emoji) >= 6 {
				break
			}
			if !contains(emoji, e) {
				emoji = append(emoji, e)
			}
		}
		emoji = emoji[:min(len(emoji), 6)]

		t.app.choose("React with", append(emoji, "Cancel"), func(e string) {
			if e == "Cancel" {
				return
			}
			left := false
			for _, r := range m.Reactions {
				left = left || r.Emoji == e && t.app.me != nil && r.UserID == t.app.me.ID
			}
			var reactions []api.Reaction
			t.app.act(func(ctx context.Context) (err error) {
				reactions, err = t.app.client.React(ctx, m.ID, e, !left)
				return err
			}, func(err error) {
				if err != nil {
					t.app.flashError(err)
					return
				}
				if reactions != nil || left {
					m.Reactions = reactions
				}
				t.Invalidate()
			})
		})
	})
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}

	return false
}

// holds tells whether v is open in any tab.
func (a *App) holds(v view) bool {
	for _, t := range a.tabs {
		for _, s := range t.stack {
			if s == v {
				return true
			}
		}
	}

	return false
}
