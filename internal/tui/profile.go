package tui

import (
	"context"
	"fmt"

	"github.com/gdamore/tcell/v2"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// newProfile shows a user's profile above their commits, or their replies after T.
func newProfile(app *App, username string) *commitFeed {
	var profile *api.Profile
	replies := false

	f := newCommitFeed(app, "@"+username, "No commits yet.", nil)
	f.load = func(ctx context.Context, cursor string) ([]*api.Commit, string, error) {
		if cursor == "" {
			p, err := app.client.Profile(ctx, username)
			if err != nil {
				return nil, "", err
			}
			// The header reads the profile while drawing, so it's swapped in on the UI goroutine.
			app.tv.QueueUpdate(func() { profile = p })
		}

		page, err := app.client.UserCommits(ctx, username, cursor, replies)
		if err != nil {
			return nil, "", err
		}

		return pointers(page.Data), page.NextCursor, nil
	}

	f.header = func(width int) []string {
		if profile == nil {
			return nil
		}

		lines := profileLines(profile, width)
		if pinned := profile.PinnedCommit; pinned != nil && !replies {
			lines = append(lines, fmt.Sprintf("[%s]Pinned[-]", colorMuted))
			lines = append(lines, commitLines(pinned, false, false, width)...)
		}
		mode := "Commits"
		if replies {
			mode = "Replies"
		}

		return append(lines, fmt.Sprintf("[::b]%s[::-] [%s](T to switch)[-]", mode, colorMuted), "")
	}

	f.keys = func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() != tcell.KeyRune {
			return event
		}

		switch event.Rune() {
		case 'T':
			replies = !replies
			f.empty = "No commits yet."
			if replies {
				f.empty = "No replies yet."
			}
			f.Reload()
		case 'F':
			if profile == nil || profile.Data.ID == app.me.ID {
				return nil
			}
			follow := !profile.IsFollowing
			app.async(func(ctx context.Context) error {
				return app.client.Follow(ctx, username, follow)
			}, func(err error) {
				if err != nil {
					app.flashError(err)
					return
				}
				f.Reload()
				if follow {
					app.flash("Following @%s.", username)
				} else {
					app.flash("Unfollowed @%s.", username)
				}
			})
		default:
			return event
		}

		return nil
	}

	return f
}
