package tui

import (
	"context"
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
	"github.com/eggplants/dev-null-tui/internal/browser"
)

// commitFeed is a list of commits with the actions the web's commit cards have.
type commitFeed struct {
	*pager[*api.Commit]

	// focusID is the commit a thread is about, shown in full and selected when the thread opens.
	focusID string
	// keys handles the keys a particular feed adds, returning nil for those it used.
	keys func(event *tcell.EventKey) *tcell.EventKey
}

func newCommitFeed(app *App, title, empty string, load func(ctx context.Context, cursor string) ([]*api.Commit, string, error)) *commitFeed {
	f := &commitFeed{}
	f.pager = newPager(app, title, empty, func(c *api.Commit, selected bool, width int) []string {
		return commitLines(c, selected, c.ID == f.focusID, width)
	})
	f.load = load

	return f
}

func pointers[T any](items []T) []*T {
	out := make([]*T, len(items))
	for i := range items {
		out[i] = &items[i]
	}

	return out
}

func newLogFeed(app *App, log api.Log) *commitFeed {
	empty := "Nothing here yet."
	if log == api.LogFollowing {
		empty = "Your log is empty. Follow some developers from Latest or Trending."
	}

	return newCommitFeed(app, string(log), empty, func(ctx context.Context, cursor string) ([]*api.Commit, string, error) {
		page, err := app.client.Log(ctx, log, cursor)
		if err != nil {
			return nil, "", err
		}

		return pointers(page.Data), page.NextCursor, nil
	})
}

func newSavedFeed(app *App) *commitFeed {
	return newCommitFeed(app, "Saved", "Commits you save with s show up here.", func(ctx context.Context, cursor string) ([]*api.Commit, string, error) {
		page, err := app.client.Saved(ctx, cursor)
		if err != nil {
			return nil, "", err
		}

		return pointers(page.Data), page.NextCursor, nil
	})
}

// newThread shows a commit with the commits it replies to above it and its replies below it, as its page does.
func newThread(app *App, id string) *commitFeed {
	f := newCommitFeed(app, "Thread", "", nil)
	f.focusID = id

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
	f.header = func(width int) []string {
		return []string{fmt.Sprintf("[::b]%s[::-] [%s](esc to go back)[-]", tview.Escape(f.title), colorMuted), ""}
	}
	f.afterReload = func() {
		for i, c := range f.items {
			if c.ID == id {
				f.title = "Thread " + c.ShortHash
				f.Select(i)
			}
		}
	}

	return f
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
		f.open(c)
		return nil
	}
	if event.Key() != tcell.KeyRune {
		return event
	}

	switch event.Rune() {
	case 'l':
		f.open(c)
	case 'r':
		f.app.compose(composeOptions{parent: c, done: f.Reload})
	case 'Q':
		f.app.compose(composeOptions{quoted: c})
	case 'f':
		f.update(c, func(ctx context.Context) (*api.Commit, error) { return f.app.client.Star(ctx, c.ID, !c.Starred) })
	case 's':
		f.update(c, func(ctx context.Context) (*api.Commit, error) { return f.app.client.Save(ctx, c.ID, !c.IsSaved) })
	case 'P':
		f.update(c, func(ctx context.Context) (*api.Commit, error) { return f.app.client.GivePastel(ctx, c.ID) })
	case 'u':
		if c.Author != nil {
			f.app.push(newProfile(f.app, c.Author.Username))
		}
	case 'o':
		if err := browser.Open(c.HTMLURL); err != nil {
			f.app.flashError(err)
		}
	case 'D':
		f.delete(c)
	default:
		return event
	}

	return nil
}

func (f *commitFeed) open(c *api.Commit) {
	if c.ID != f.focusID {
		f.app.push(newThread(f.app, c.ID))
	}
}

// update runs an action that answers with the commit's new state, and takes the counts and flags from it, keeping
// what only a list has, like the reply preview.
func (f *commitFeed) update(c *api.Commit, action func(ctx context.Context) (*api.Commit, error)) {
	var updated *api.Commit
	f.app.async(func(ctx context.Context) (err error) {
		updated, err = action(ctx)
		return err
	}, func(err error) {
		if err != nil {
			f.app.flashError(err)
			return
		}

		c.Starred, c.StarsCount = updated.Starred, updated.StarsCount
		c.IsSaved, c.PasteisCount = updated.IsSaved, updated.PasteisCount
		c.RepliesCount = updated.RepliesCount
		f.Invalidate()
	})
}

func (f *commitFeed) delete(c *api.Commit) {
	if c.Author == nil || c.Author.ID != f.app.me.ID {
		f.app.flash("[red]You can only delete your own commits.[-]")
		return
	}

	f.app.confirm(fmt.Sprintf("Delete commit %s?", c.ShortHash), func() {
		f.app.async(func(ctx context.Context) error {
			return f.app.client.DeleteCommit(ctx, c.ID)
		}, func(err error) {
			if err != nil {
				f.app.flashError(err)
				return
			}

			for i, item := range f.items {
				if item == c {
					f.Remove(i)
				}
			}
			f.app.flash("Deleted %s.", c.ShortHash)
		})
	})
}
