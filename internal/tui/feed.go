package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// commitFeed is a list of commits with the actions the web's commit cards have.
type commitFeed struct {
	*pager[*api.Commit]

	// focusID is the commit a thread is about, shown in full and selected when the thread opens.
	focusID string
	// keys handles the keys a particular feed adds, returning nil for those it used.
	keys func(event *tcell.EventKey) *tcell.EventKey
	// hints are the keys a particular feed adds, for the status bar.
	hints func() []hint
	// byDay puts the day above the first commit made on it, for a list in the order the commits were made.
	byDay bool

	// log is the log the feed shows, if it shows one, and newCount how many commits came in on top of it since.
	log      api.Log
	newCount int
	// suggestions are the people the API suggests following when the Following log is empty.
	suggestions []api.User
	// checking is set while the log is asked for new commits, so a slow answer isn't asked for again.
	checking bool
}

func newCommitFeed(app *App, title string, empty []string, load func(ctx context.Context, cursor string) ([]*api.Commit, string, error)) *commitFeed {
	f := &commitFeed{}
	f.pager = newPager(app, title, empty, func(c *api.Commit, selected bool, width int) []string {
		return commitLines(c, selected, c.ID == f.focusID, width)
	})
	f.load = load
	f.divider = f.dayDivider
	f.onActivate = func() {
		if c, ok := f.Selected(); ok {
			f.open(c)
		}
	}
	f.keep = func(items []*api.Commit) int {
		if c, ok := f.Selected(); ok {
			for i, item := range items {
				if item.ID == c.ID {
					return i
				}
			}
		}
		return 0
	}

	return f
}

// dayDivider names the day above the first commit made on it, in a list that has them in that order.
func (f *commitFeed) dayDivider(prev, c *api.Commit, width int) []string {
	if !f.byDay || c.CreatedAt.IsZero() {
		return nil
	}
	label := dayLabel(c.CreatedAt)
	if prev != nil && !prev.CreatedAt.IsZero() && dayLabel(prev.CreatedAt) == label {
		return nil
	}

	return []string{dividerLine(label, width)}
}

// dividerLine is a label on a hairline across the list.
func dividerLine(label string, width int) string {
	text := fmt.Sprintf(" [%s]──[-] [%s::b]%s[-::-] ", theme.hairline, colorMuted, tview.Escape(label))
	return text + fmt.Sprintf("[%s]%s[-]", theme.hairline, strings.Repeat("─", max(0, width-tview.TaggedStringWidth(text))))
}

func pointers[T any](items []T) []*T {
	out := make([]*T, len(items))
	for i := range items {
		out[i] = &items[i]
	}

	return out
}

func newLogFeed(app *App, log api.Log) *commitFeed {
	empty := emptyState("nothing to read here yet")
	if log == api.LogFollowing {
		empty = emptyState("your log is empty",
			"Find developers to follow in 2 Latest or 3 Trending:",
			"pick a commit, press u for its author's profile, then F to follow.")
	}

	title := map[api.Log]string{api.LogFollowing: "Following", api.LogLatest: "Latest", api.LogTrending: "Trending"}[log]
	var f *commitFeed
	f = newCommitFeed(app, title, empty, func(ctx context.Context, cursor string) ([]*api.Commit, string, error) {
		page, err := app.client.Log(ctx, log, cursor)
		if err != nil {
			return nil, "", err
		}
		if cursor == "" {
			suggestions := page.Suggestions
			app.tv.QueueUpdate(func() { f.suggestions = suggestions })
		}

		return pointers(page.Data), page.NextCursor, nil
	})
	f.log = log
	f.byDay = log != api.LogTrending
	f.afterReload = func() {
		f.newCount = 0
		app.drawHeader()
		if len(f.items) == 0 && len(f.suggestions) > 0 {
			f.SetFooter(append(append([]string{}, f.empty...), suggestionLines(f.suggestions)...)...)
		}
	}

	return f
}

// suggestionLines list the people the API suggests following, for an empty Following log.
func suggestionLines(people []api.User) []string {
	lines := []string{"", fmt.Sprintf("   [%s::b]Who to follow[-::-] [%s](/ and their @name opens a profile)[-]", colorInk, colorMuted)}
	for i := range people {
		u := &people[i]
		line := fmt.Sprintf("   [%s]●[-] %s", laneColor(u), authorName(u))
		if u.Bio != "" {
			line += fmt.Sprintf(" [%s]· %s[-]", colorMuted, tview.Escape(firstLine(u.Bio, 60)))
		}
		lines = append(lines, line)
	}

	return lines
}

// firstLine is the first line of text, cut to n characters.
func firstLine(text string, n int) string {
	text, _, _ = strings.Cut(text, "\n")
	if r := []rune(text); len(r) > n {
		return string(r[:n-1]) + "…"
	}

	return text
}

// checkNewCommits asks whether commits came in on top of a log, as the web does every 30 seconds. A feed scrolled to
// the top takes them in at once; one scrolled down keeps its place and counts them in its tab until it's reloaded.
// Trending is ranked, so new commits don't go on top of it.
func (f *commitFeed) checkNewCommits() {
	if f.log == "" || f.log == api.LogTrending || !f.loaded || f.loading || f.checking || len(f.items) == 0 {
		return
	}

	since, generation := f.items[0].ID, f.generation
	var n int
	f.checking = true
	f.app.async(func(ctx context.Context) (err error) {
		n, err = f.app.client.NewCommitsCount(ctx, f.log, since)
		f.app.backOff(err)
		return err
	}, func(err error) {
		f.checking = false
		// Polling happens in the background, so a failure waits for the next try rather than interrupting.
		if err != nil || n == 0 || generation != f.generation {
			return
		}

		if f.selected == 0 && f.offset == 0 {
			f.Reload()
			return
		}
		f.newCount = n
		f.app.drawHeader()
	})
}

// prepend puts a commit the user just made on top of the feed, as the web does, without asking the server again.
func (f *commitFeed) prepend(c *api.Commit) {
	if !f.loaded {
		return
	}
	for _, item := range f.items {
		if item.ID == c.ID {
			return
		}
	}

	copied := *c
	f.selected = 0
	f.offset = 0
	f.SetItems(append([]*api.Commit{&copied}, f.items...))
	f.SetFooter()
	if f.cursor == "" {
		f.SetFooter(f.end)
	}
}

func newStarredFeed(app *App) *commitFeed {
	return newCommitFeed(app, "Starred", emptyState("no stars yet", "Press s on a commit to star it; it shows up here."),
		func(ctx context.Context, cursor string) ([]*api.Commit, string, error) {
			page, err := app.client.Starred(ctx, cursor)
			if err != nil {
				return nil, "", err
			}

			return pointers(page.Data), page.NextCursor, nil
		})
}

// newThread shows a commit with the commits it replies to above it and its replies below it, as its page does.
func newThread(app *App, id string) *commitFeed {
	f := newCommitFeed(app, "Thread", nil, nil)
	f.focusID = id
	if c := app.knownCommit(id); c != nil {
		f.title = "Thread " + c.ShortHash
	}

	f.load = func(ctx context.Context, cursor string) ([]*api.Commit, string, error) {
		var items []*api.Commit
		if cursor == "" {
			thread, err := app.client.Commit(ctx, id)
			if err != nil {
				return nil, "", err
			}
			items = append(pointers(thread.Ancestors), &thread.Commit)
		}

		replies, err := app.client.Replies(ctx, id, cursor, false)
		if err != nil {
			return nil, "", err
		}

		return append(items, pointers(replies.Data)...), replies.NextCursor, nil
	}
	f.divider = func(prev, c *api.Commit, width int) []string {
		if prev == nil || prev.ID != id {
			return nil
		}
		return []string{dividerLine(repliesLabel(prev.RepliesCount), width)}
	}
	f.afterReload = func() {
		for i, c := range f.items {
			if c.ID == id {
				f.title = "Thread " + c.ShortHash
				f.Select(i)
				if i == len(f.items)-1 {
					f.SetFooter(dividerLine("No replies yet · r to reply", 0))
				}
			}
		}
	}
	// A thread closes on its last reply, not on the root commit.
	f.end = ""

	return f
}

func repliesLabel(n int) string {
	if n == 1 {
		return "1 reply"
	}

	return fmt.Sprintf("%s replies", count(n))
}

func (f *commitFeed) Hints() []hint {
	c, ok := f.Selected()
	if !ok {
		hints := []hint{{"c", "commit"}, {"R", "refresh"}}
		if f.hints != nil {
			hints = append(f.hints(), hints...)
		}
		return hints
	}

	hints := []hint{{"j/k", "move"}}
	if c.ID != f.focusID {
		hints = append(hints, hint{"⏎", "open"})
	}
	hints = append(hints, hint{"r", "reply"})
	if c.Starred {
		hints = append(hints, hint{"s", "unstar"})
	} else {
		hints = append(hints, hint{"s", "star"})
	}
	hints = append(hints, hint{"Q", "quote"}, hint{"u", "author"})
	if len(c.Images) > 0 {
		hints = append(hints, hint{"i", "images"})
	}
	if f.hints != nil {
		hints = append(hints, f.hints()...)
	}
	hints = append(hints, hint{"y", "copy link"}, hint{"o", "browser"})
	if f.app.me != nil && c.Author != nil && c.Author.ID == f.app.me.ID {
		hints = append(hints, hint{"D", "delete"})
	}

	return hints
}

func (f *commitFeed) eachCommit(fn func(c *api.Commit)) {
	for _, c := range f.items {
		fn(c)
		if c.QuotedCommit != nil {
			fn(c.QuotedCommit)
		}
		if c.ReplyPreview != nil {
			for i := range c.ReplyPreview.Data {
				fn(&c.ReplyPreview.Data[i])
			}
		}
	}
}

func (f *commitFeed) removeCommit(id string) {
	for i := len(f.items) - 1; i >= 0; i-- {
		c := f.items[i]
		if c.ID == id && c.ID != f.focusID {
			f.Remove(i)
			continue
		}
		if c.ID == id {
			now := now()
			c.DeletedAt = &now
		}
		if c.ReplyPreview != nil {
			kept := c.ReplyPreview.Data[:0]
			for _, r := range c.ReplyPreview.Data {
				if r.ID != id {
					kept = append(kept, r)
				} else if c.RepliesCount > 0 {
					c.RepliesCount--
				}
			}
			c.ReplyPreview.Data = kept
		}
		if c.QuotedCommit != nil && c.QuotedCommit.ID == id {
			c.QuotedCommit = nil
		}
	}
	f.Invalidate()
}

func (f *commitFeed) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	list := f.listView.InputHandler()

	return func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		if f.keys != nil {
			if event = f.keys(event); event == nil {
				return
			}
		}
		if event = f.commitKeys(event); event != nil {
			list(event, setFocus)
		}
	}
}

func (f *commitFeed) commitKeys(event *tcell.EventKey) *tcell.EventKey {
	c, ok := f.Selected()
	if !ok {
		return event
	}

	if event.Key() == tcell.KeyEnter {
		if c.ID == f.focusID {
			f.reply(c)
		} else {
			f.open(c)
		}
		return nil
	}
	if event.Key() != tcell.KeyRune {
		return event
	}

	switch event.Rune() {
	case 'l':
		f.open(c)
	case 'r':
		f.reply(c)
	case 'Q':
		f.app.compose(composeOptions{quoted: c})
	case 's':
		star := !c.Starred
		f.update(c, func(ctx context.Context) (*api.Commit, error) { return f.app.client.Star(ctx, c.ID, star) })
	case 'P':
		f.givePastel(c)
	case 'u':
		if c.Author != nil {
			f.app.push(newProfile(f.app, c.Author.Username))
		}
	case 'o':
		f.app.openURL(c.HTMLURL, true)
	case 'y':
		f.app.copyLink(c)
	case 'i':
		if len(c.Images) == 0 {
			f.app.flash("This commit has no images.")
			break
		}
		f.app.push(newImageViewer(f.app, c.Images))
	case 'D':
		f.delete(c)
	case '!':
		f.app.report(c)
	default:
		return event
	}

	return nil
}

func (f *commitFeed) open(c *api.Commit) {
	if c.DeletedAt != nil && c.RepliesCount == 0 {
		return
	}
	if c.ID != f.focusID {
		f.app.push(newThread(f.app, c.ID))
	}
}

// reply opens the composer for a reply to c, reloading the feed once it's made so the reply shows.
func (f *commitFeed) reply(c *api.Commit) {
	if c.DeletedAt != nil {
		f.app.fail("You can't reply to a deleted commit.")
		return
	}

	f.app.compose(composeOptions{parent: c, done: func(*api.Commit) {
		c.RepliesCount++
		f.app.updateCommit(c)
		f.Reload()
	}})
}

// update runs an action that answers with the commit's new state, and takes the counts and flags from it into every
// copy of the commit on screen, keeping what only a list has, like the reply preview.
func (f *commitFeed) update(c *api.Commit, action func(ctx context.Context) (*api.Commit, error)) {
	var updated *api.Commit
	f.app.act(func(ctx context.Context) (err error) {
		updated, err = action(ctx)
		return err
	}, func(err error) {
		if err != nil {
			f.app.flashError(err)
			return
		}

		updated.ID = c.ID
		c.Starred, c.StarsCount, c.PasteisCount = updated.Starred, updated.StarsCount, updated.PasteisCount
		f.app.updateCommit(c)
	})
}

// givePastel gives the commit a pastel, saying how many the user has left today.
func (f *commitFeed) givePastel(c *api.Commit) {
	if !showPasteis {
		f.app.flash("Turn pastéis mode on in the settings (g S) to give pastéis.")
		return
	}

	var answer *api.Pastel
	f.app.act(func(ctx context.Context) (err error) {
		answer, err = f.app.client.GivePastel(ctx, c.ID)
		return err
	}, func(err error) {
		if err != nil {
			f.app.flashError(err)
			return
		}

		c.PasteisCount = answer.Commit.PasteisCount
		f.app.updateCommit(c)
		f.app.me.PasteisRemaining = answer.PasteisRemaining
		f.app.succeed("Gave %s a pastel de nata · %d left today.", c.ShortHash, answer.PasteisRemaining)
	})
}

func (f *commitFeed) delete(c *api.Commit) {
	if c.Author == nil || c.Author.ID != f.app.me.ID {
		f.app.fail("You can only delete your own commits.")
		return
	}

	f.app.confirm(fmt.Sprintf("Delete commit %s? It goes to /dev/null for good.", c.ShortHash), func() {
		f.app.act(func(ctx context.Context) error {
			return f.app.client.DeleteCommit(ctx, c.ID)
		}, func(err error) {
			if err != nil {
				f.app.flashError(err)
				return
			}

			delete(f.app.pendingReview, c.ID)
			f.app.removeCommit(c.ID)
			f.app.succeed("rm %s → /dev/null", c.ShortHash)
		})
	})
}
