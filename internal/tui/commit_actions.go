package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// isMine tells whether the user wrote c.
func (a *App) isMine(c *api.Commit) bool {
	return a.me != nil && c.Author != nil && c.Author.ID == a.me.ID
}

// togglePin pins one of the user's top-level commits to their profile, in place of the one pinned before, or unpins
// it, as Pin does on the web.
func (f *commitFeed) togglePin(c *api.Commit) {
	switch {
	case !f.app.isMine(c):
		f.app.fail("You can only pin your own commits.")
		return
	case c.ParentID != nil:
		f.app.fail("Only commits, not replies, can be pinned.")
		return
	}

	pin := !c.IsPinned
	var updated *api.Commit
	f.app.act(func(ctx context.Context) (err error) {
		updated, err = f.app.client.Pin(ctx, c.ID, pin)
		return err
	}, func(err error) {
		if err != nil {
			f.app.flashError(err)
			return
		}

		updated.ID = c.ID
		f.app.setPinned(c, updated.IsPinned)
		if updated.IsPinned {
			f.app.succeed("Pinned %s to the top of your profile.", c.ShortHash)
		} else {
			f.app.succeed("Unpinned %s.", c.ShortHash)
		}
	})
}

// setPinned takes it into every view that c is the user's one pinned commit now, or that they have none.
func (a *App) setPinned(c *api.Commit, pinned bool) {
	a.views(func(v view) {
		h, ok := v.(commitHolder)
		if !ok {
			return
		}
		h.eachCommit(func(other *api.Commit) {
			if a.isMine(other) {
				other.IsPinned = pinned && other.ID == c.ID
			}
		})
		if p, ok := v.(*profileView); ok && p.isMe() && p.profile != nil {
			var top *api.Commit
			if pinned {
				copied := *c
				copied.IsPinned = true
				top = &copied
			}
			p.profile.PinnedCommit = top
			if f, ok := p.tabs[0].(*commitFeed); ok {
				f.pin(top)
			}
		}
		h.Invalidate()
	})
}

// deleted is the commit the user deleted last, which they can still undo until the window closes.
type deleted struct {
	commit *api.Commit
	at     time.Time
}

func (d *deleted) undoable() bool {
	return d != nil && time.Since(d.at) < api.RestoreWindow*time.Second
}

// undoDelete brings back the commit the user deleted last, as Undo does on the web within its 30 seconds.
func (a *App) undoDelete() {
	d := a.lastDeleted
	if !d.undoable() {
		a.flash("Nothing to undo: a deleted commit can only come back for %d seconds.", api.RestoreWindow)
		return
	}

	a.act(func(ctx context.Context) error {
		_, err := a.client.Restore(ctx, d.commit.ID)
		return err
	}, func(err error) {
		if err != nil {
			a.flashError(err)
			return
		}
		a.lastDeleted = nil
		a.views(func(v view) {
			if _, ok := v.(commitHolder); ok && loaded(v) {
				v.Reload()
			}
		})
		a.succeed("Restored %s.", d.commit.ShortHash)
	})
}

// vote asks which of a poll's options to vote for and votes for it, showing the results it answers with.
func (f *commitFeed) vote(c *api.Commit) {
	p := c.Poll
	switch {
	case p == nil:
		f.app.flash("This commit has no poll.")
		return
	case p.IsClosed:
		f.app.flash("This poll is closed.")
		return
	case p.HasVoted:
		f.app.flash("You voted on this poll already.")
		return
	}

	labels := make([]string, 0, len(p.Options)+1)
	for _, o := range p.Options {
		labels = append(labels, o.Label)
	}
	labels = append(labels, "Cancel")

	f.app.choose("Vote for", labels, func(label string) {
		for _, o := range p.Options {
			if o.Label != label {
				continue
			}
			option := o.ID
			var updated *api.Commit
			f.app.act(func(ctx context.Context) (err error) {
				updated, err = f.app.client.Vote(ctx, c.ID, option)
				return err
			}, func(err error) {
				if err != nil {
					f.app.flashError(err)
					return
				}
				updated.ID = c.ID
				c.Poll = updated.Poll
				f.app.updateCommit(c)
				f.app.succeed("Voted for %s.", label)
			})
			return
		}
	})
}

// toggleTranslation shows a commit in the language the user reads commits in, as Translate does on the web, or
// its own words again.
func (f *commitFeed) toggleTranslation(c *api.Commit) {
	if c.Translation != nil {
		c.Translation = nil
		f.app.flash("Showing %s as written.", c.ShortHash)
		f.Invalidate()
		return
	}
	if c.DeletedAt != nil || c.Body == "" {
		f.app.flash("There is nothing to translate.")
		return
	}

	var t *api.Translation
	f.app.act(func(ctx context.Context) error {
		language, err := f.app.translationLanguage(ctx)
		if err != nil {
			return err
		}
		t, err = f.app.client.Translate(ctx, c.ID, language)
		return err
	}, func(err error) {
		var apiErr *api.Error
		switch {
		case errors.As(err, &apiErr) && apiErr.Status == 503:
			f.app.fail("Couldn't translate %s. Press t to try again.", c.ShortHash)
		case err != nil:
			f.app.flashError(err)
		case !t.Available:
			f.app.flash("%s is in your language already.", c.ShortHash)
		case t.Translated == nil:
			f.app.flash("The translation isn't ready yet. Press t again in a moment.")
		default:
			c.Translation = t
			f.app.flash("t shows %s as written again.", c.ShortHash)
			f.Invalidate()
		}
	})
}

// translationLanguage is the language the user has commits translated into, asked for once. Call it off the UI
// goroutine.
func (a *App) translationLanguage(ctx context.Context) (string, error) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()

	if a.language == "" {
		settings, err := a.client.Settings(ctx)
		if err != nil {
			return "", err
		}
		a.language = "en"
		if settings.TranslationLanguage != nil && *settings.TranslationLanguage != "" {
			a.language = *settings.TranslationLanguage
		}
	}

	return a.language, nil
}

// showStars lists who starred a commit.
func (f *commitFeed) showStars(c *api.Commit) {
	if c.StarsCount == 0 {
		f.app.flash("Nobody starred %s yet.", c.ShortHash)
		return
	}

	id := c.ID
	l := newPeopleList(f.app, fmt.Sprintf("Stars on %s", c.ShortHash), emptyState("no stars yet"), func(ctx context.Context, cursor string) ([]*api.User, string, error) {
		page, err := f.app.client.Stars(ctx, id, cursor)
		if err != nil {
			return nil, "", err
		}

		return pointers(page.Data), page.NextCursor, nil
	})
	f.app.push(l)
}
