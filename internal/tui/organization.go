package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// organizationView is an organization's page: what it committed and the commits that mention it, under who it is.
type organizationView struct {
	*tabbedView

	handle string
	org    *api.Organization
	// generation tells answers to an earlier Reload apart.
	generation int
}

func newOrganization(app *App, handle string, org *api.Organization) *organizationView {
	o := &organizationView{tabbedView: newTabbedView(app, "@"+handle), handle: handle, org: org}
	o.self = o
	o.head = o.lines
	o.headOverlay = o.drawAvatar
	o.keys = o.orgKeys
	o.hints = o.orgHints

	commits := func(mentions bool) *commitFeed {
		empty := emptyState("no commits yet")
		if mentions {
			empty = emptyState("nobody mentioned @" + handle + " yet")
		}
		f := newCommitFeed(app, "@"+handle, empty, func(ctx context.Context, cursor string) ([]*api.Commit, string, error) {
			page, err := app.client.OrganizationCommits(ctx, handle, cursor, mentions)
			if err != nil {
				return nil, "", err
			}

			return pointers(page.Data), page.NextCursor, nil
		})
		f.byDay = true
		return f
	}
	o.addCommits("Commits", commits(false))
	o.addCommits("Mentions", commits(true))

	return o
}

// Reload reads the organization again, and the tab under it.
func (o *organizationView) Reload() {
	o.generation++
	generation := o.generation

	var org *api.Organization
	o.app.async(func(ctx context.Context) (err error) {
		org, err = o.app.client.Organization(ctx, o.handle)
		return err
	}, func(err error) {
		if generation != o.generation {
			return
		}
		if err != nil {
			o.app.flashError(err)
			return
		}
		o.org = org
		o.Invalidate()
	})
	o.tabbedView.Reload()
}

func (o *organizationView) lines(width int) []string {
	if o.org == nil {
		return []string{fmt.Sprintf("   [%s]Reading @%s from /dev/null…[-]", colorMuted, tview.Escape(o.handle)), ""}
	}

	return organizationLines(o.org, width)
}

// organizationLines renders an organization's head, as its page does.
func organizationLines(org *api.Organization, width int) []string {
	u := &org.Data.User
	const indent = "      "
	inner := max(width-len(indent)-1, 10)

	name := u.Name
	if name == "" {
		name = u.Username
	}
	head := fmt.Sprintf("%s  [%s::b]%s[-::-] [%s]@%s[-] %s", strings.Repeat(" ", profileAvatarCol+profileAvatarCols-1),
		colorInk, tview.Escape(name), colorMuted, tview.Escape(u.Username), pill("organization", colorMuted))
	follow := pill("+ F to follow", colorMuted)
	if org.IsFollowing {
		follow = pill("✓ following", colorPrimary)
	}

	lines := []string{"", spread(head, follow+" ", width)}
	if u.Bio != "" {
		lines = append(lines, wrap(fmt.Sprintf("[%s]%s[-]", colorInk, tview.Escape(u.Bio)), indent, inner+len(indent))...)
	}

	var facts []string
	for _, f := range []string{u.Location, strings.TrimPrefix(strings.TrimPrefix(u.Website, "https://"), "http://")} {
		if f != "" {
			facts = append(facts, tview.Escape(f))
		}
	}
	facts = append(facts, "github.com/"+tview.Escape(u.Username))
	lines = append(lines, wrap(fmt.Sprintf("[%s]%s[-]", colorMuted, strings.Join(facts, " · ")), indent, inner+len(indent))...)

	if u.FollowersCount != nil {
		lines = append(lines, indent+fmt.Sprintf("[%s::b]%s[-::-] [%s]followers[-]", colorInk, count(*u.FollowersCount), colorMuted))
	}
	if admins := org.Data.Admins; len(admins) > 0 {
		names := make([]string, len(admins))
		for i := range admins {
			names[i] = fmt.Sprintf("[%s]@%s[-]", laneColor(&admins[i]), tview.Escape(admins[i].Username))
		}
		lines = append(lines, wrap(fmt.Sprintf("[%s]Admins[-] %s", colorMuted, strings.Join(names, " ")), indent, inner+len(indent))...)
	}

	return append(lines, "")
}

func (o *organizationView) drawAvatar(screen tcell.Screen, top, x, y, width, height int) {
	if o.org != nil {
		o.app.drawAvatar(screen, &o.org.Data.User, x+profileAvatarCol, top+profileAvatarLine, profileAvatarCols, profileAvatarRows, y, height)
	}
}

func (o *organizationView) orgHints() []hint {
	if o.org == nil {
		return nil
	}
	if o.org.IsFollowing {
		return []hint{{"F", "unfollow"}}
	}

	return []hint{{"F", "follow"}}
}

func (o *organizationView) orgKeys(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() != tcell.KeyRune || event.Rune() != 'F' {
		return event
	}
	if o.org == nil {
		return nil
	}

	follow := !o.org.IsFollowing
	o.app.act(func(ctx context.Context) error {
		return o.app.client.FollowOrganization(ctx, o.handle, follow)
	}, func(err error) {
		if err != nil {
			o.app.flashError(err)
			return
		}
		o.org.IsFollowing = follow
		if n := o.org.Data.FollowersCount; n != nil {
			if follow {
				*n++
			} else {
				*n = max(*n-1, 0)
			}
		}
		o.Invalidate()
		if follow {
			o.app.succeed("Following @%s.", o.handle)
		} else {
			o.app.succeed("Unfollowed @%s.", o.handle)
		}
	})

	return nil
}
