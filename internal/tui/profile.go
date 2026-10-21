package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// profileTabNames are a profile's tabs, as the web has them.
var profileTabNames = []string{"Commits", "Replies", "Followers", "Following"}

// profileView shows a user's profile above one of its tabs: their commits, their replies, their followers or whom
// they follow.
type profileView struct {
	*tview.Box

	app      *App
	username string
	profile  *api.Profile

	tabs    []view
	current int
	// generation tells answers to an earlier Reload apart.
	generation int
}

func newProfile(app *App, username string) *profileView {
	p := &profileView{Box: tview.NewBox(), app: app, username: username}

	commits := func(replies bool) *commitFeed {
		empty := emptyState("no commits yet")
		if replies {
			empty = emptyState("no replies yet")
		}
		f := newCommitFeed(app, "@"+username, empty, func(ctx context.Context, cursor string) ([]*api.Commit, string, error) {
			page, err := app.client.UserCommits(ctx, username, cursor, replies)
			if err != nil {
				return nil, "", err
			}

			return pointers(page.Data), page.NextCursor, nil
		})
		f.byDay = true
		f.header = p.header
		f.headerOverlay = p.drawAvatar
		f.keys = p.keys
		f.hints = p.hints
		return f
	}
	people := func(load func(ctx context.Context, username, cursor string) (*api.Page[api.User], error), empty []string) *peopleList {
		l := newPeopleList(app, "@"+username, empty, func(ctx context.Context, cursor string) ([]*api.User, string, error) {
			page, err := load(ctx, username, cursor)
			if err != nil {
				return nil, "", err
			}

			return pointers(page.Data), page.NextCursor, nil
		})
		l.header = p.header
		l.headerOverlay = p.drawAvatar
		l.keys = p.keys
		l.extraHints = p.hints
		return l
	}

	p.tabs = []view{
		commits(false),
		commits(true),
		people(app.client.Followers, emptyState("no followers yet")),
		people(app.client.Following, emptyState("not following anyone yet")),
	}

	return p
}

func (p *profileView) tab() view { return p.tabs[p.current] }

func (p *profileView) Title() string { return "@" + p.username }

func (p *profileView) Loaded() bool { return loaded(p.tab()) }
func (p *profileView) Loading() bool {
	l, ok := p.tab().(interface{ Loading() bool })
	return ok && l.Loading()
}

func (p *profileView) Hints() []hint { return p.tab().Hints() }

func (p *profileView) Position() (int, int) {
	if l, ok := p.tab().(interface{ Position() (int, int) }); ok {
		return l.Position()
	}

	return 0, 0
}

func (p *profileView) Select(i int) {
	if l, ok := p.tab().(interface{ Select(int) }); ok {
		l.Select(i)
	}
}

// Reload fetches the profile again, and the tab under it.
func (p *profileView) Reload() {
	p.generation++
	generation := p.generation

	var profile *api.Profile
	p.app.async(func(ctx context.Context) (err error) {
		profile, err = p.app.client.Profile(ctx, p.username)
		return err
	}, func(err error) {
		if generation != p.generation {
			return
		}
		if err != nil {
			p.app.flashError(err)
			return
		}
		p.profile = profile
		if f, ok := p.tabs[0].(*commitFeed); ok {
			f.pin(profile.PinnedCommit)
		}
		p.invalidate()
	})
	p.tab().Reload()
}

func (p *profileView) invalidate() {
	for _, t := range p.tabs {
		if h, ok := t.(interface{ Invalidate() }); ok {
			h.Invalidate()
		}
	}
}

// header is what every tab shows above its list: the profile and the tabs. The pinned commit is the first of the
// commits, so it can be selected and shows its images.
func (p *profileView) header(width int) []string {
	if p.profile == nil {
		return []string{fmt.Sprintf("   [%s]Reading @%s from /dev/null…[-]", colorMuted, tview.Escape(p.username))}
	}

	me, left := "", -1
	if p.app.me != nil {
		me, left = p.app.me.Username, p.app.me.PasteisRemaining
	}
	lines := profileLines(p.profile, me, left, width)

	return append(lines, "", profileTabs(profileTabNames, p.current, width), fmt.Sprintf("[%s]%s[-]", theme.hairline, strings.Repeat("─", max(width, 0))))
}

// drawAvatar draws the profile's avatar in its head, whose first line is at top.
func (p *profileView) drawAvatar(screen tcell.Screen, top, x, y, width, height int) {
	if p.profile != nil {
		p.app.drawAvatar(screen, &p.profile.Data, x+profileAvatarCol, top+profileAvatarLine, profileAvatarCols, profileAvatarRows, y, height)
	}
}

func (p *profileView) isMe() bool {
	return p.app.me != nil && p.username == p.app.me.Username
}

func (p *profileView) hints() []hint {
	hints := []hint{{"T", "next tab"}}
	if p.profile == nil || p.isMe() {
		return hints
	}

	follow := "follow"
	if p.profile.IsFollowing {
		follow = "unfollow"
	}
	mute := "mute"
	if p.profile.IsMuting {
		mute = "unmute"
	}

	return append(hints, hint{"F", follow}, hint{"M", mute}, hint{"B", "block"})
}

// switchTab shows another of the profile's tabs, loading it the first time.
func (p *profileView) switchTab(i int) {
	p.current = i
	p.invalidate()
	p.app.show(p)
	if !loaded(p.tab()) {
		p.tab().Reload()
	}
}

func (p *profileView) keys(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() != tcell.KeyRune {
		return event
	}

	switch event.Rune() {
	case 'T':
		p.switchTab((p.current + 1) % len(p.tabs))
	case 'F':
		if p.profile == nil || p.isMe() {
			return nil
		}
		follow := !p.profile.IsFollowing
		done := "Unfollowed @%s."
		if follow {
			done = "Following @%s."
		}
		p.relate(func(ctx context.Context) error { return p.app.client.Follow(ctx, p.username, follow) }, done)
	case 'M':
		if p.profile == nil || p.isMe() {
			return nil
		}
		mute := !p.profile.IsMuting
		done := "Unmuted @%s."
		if mute {
			done = "Muted @%s: their commits stay out of your logs."
		}
		p.relate(func(ctx context.Context) error { return p.app.client.Mute(ctx, p.username, mute) }, done)
	case 'B':
		if p.profile == nil || p.isMe() {
			return nil
		}
		p.app.confirm(fmt.Sprintf("Block @%s? They can't follow you or answer your commits, and you both unfollow each other.", p.username), func() {
			p.relate(func(ctx context.Context) error { return p.app.client.Block(ctx, p.username, true) }, "Blocked @%s. Settings (g s) lists who you block.")
		})
	default:
		return event
	}

	return nil
}

// relate follows, mutes or blocks the user, then reads the profile again to show it.
func (p *profileView) relate(action func(ctx context.Context) error, done string) {
	p.app.act(action, func(err error) {
		if err != nil {
			p.app.flashError(err)
			return
		}
		p.Reload()
		p.app.succeed(done, p.username)
	})
}

func (p *profileView) eachCommit(f func(c *api.Commit)) {
	if p.profile != nil && p.profile.PinnedCommit != nil {
		f(p.profile.PinnedCommit)
	}
	for _, t := range p.tabs {
		if h, ok := t.(commitHolder); ok {
			h.eachCommit(f)
		}
	}
}

func (p *profileView) removeCommit(id string) {
	if p.profile != nil && p.profile.PinnedCommit != nil && p.profile.PinnedCommit.ID == id {
		p.profile.PinnedCommit = nil
	}
	for _, t := range p.tabs {
		if h, ok := t.(commitHolder); ok {
			h.removeCommit(id)
		}
	}
}

func (p *profileView) Invalidate() { p.invalidate() }

// prepend puts a commit the user just made on top of their commits.
func (p *profileView) prepend(c *api.Commit) {
	if f, ok := p.tabs[0].(*commitFeed); ok && c.ParentID == nil {
		f.prepend(c)
	}
	if f, ok := p.tabs[1].(*commitFeed); ok && c.ParentID != nil {
		f.prepend(c)
	}
}

func (p *profileView) Draw(screen tcell.Screen) {
	x, y, width, height := p.GetRect()
	p.tab().SetRect(x, y, width, height)
	p.tab().Draw(screen)
}

func (p *profileView) Focus(delegate func(tview.Primitive)) { delegate(p.tab()) }
func (p *profileView) HasFocus() bool                       { return p.tab().HasFocus() }

func (p *profileView) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return p.tab().InputHandler()
}

func (p *profileView) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
	return p.tab().MouseHandler()
}
