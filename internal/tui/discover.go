package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// asideEvery is how often the aside asks again who to follow and who is live.
const asideEvery = 10 * time.Minute

// refreshAside asks who to follow and which of the people the user follows are live, for the aside.
func (a *App) refreshAside() {
	var suggested, live []api.User
	a.async(func(ctx context.Context) error {
		var err error
		if suggested, err = a.client.SuggestedUsers(ctx, 0); err != nil {
			return err
		}
		live, err = a.client.LiveFollowing(ctx)
		return err
	}, func(err error) {
		// The aside only adds to the screen, so it goes without them until the next try.
		if err != nil {
			return
		}
		a.suggested, a.liveNow = suggested, live
	})
}

// asideDiscoverLines are who to follow and who is live, for the aside, as the web's aside has them.
func (a *App) asideDiscoverLines(width int) []string {
	var lines []string
	if len(a.liveNow) > 0 {
		lines = append(lines, fmt.Sprintf("[%s::b]Live now[-::-]", colorInk))
		for i := range a.liveNow {
			u := &a.liveNow[i]
			lines = append(lines, fmt.Sprintf(" [%s]●[-] [%s]@%s[-]", colorDanger, laneColor(u), tview.Escape(firstLine(u.Username, width-4))))
		}
		lines = append(lines, "")
	}
	if len(a.suggested) > 0 {
		lines = append(lines, fmt.Sprintf("[%s::b]$ who --should-follow[-::-]", colorInk))
		for i := range a.suggested[:min(3, len(a.suggested))] {
			u := &a.suggested[i]
			lines = append(lines, fmt.Sprintf(" [%s]●[-] [%s]@%s[-]", laneColor(u), colorMuted, tview.Escape(firstLine(u.Username, width-4))))
		}
		lines = append(lines, fmt.Sprintf(" [%s]g w for more[-]", colorFaint), "")
	}

	return lines
}

// newWhoToFollow lists the people the web suggests following, with why: f follows one, x says not interested and R
// suggests others.
func newWhoToFollow(app *App) *peopleList {
	round := 0
	l := newPeopleList(app, "Who to follow", emptyState("nobody to suggest right now", "R asks again."),
		func(ctx context.Context, _ string) ([]*api.User, string, error) {
			people, err := app.client.SuggestedUsers(ctx, round)
			return pointers(people), "", err
		})
	l.alsoFollow = true
	l.undo = &peopleAction{
		key: 'x', label: "not interested", done: "You won't see @%s suggested again.",
		do: func(ctx context.Context, u string) error { return app.client.DismissSuggestion(ctx, u) },
	}
	l.extraHints = func() []hint { return []hint{{"R", "others"}} }
	reload := l.pager.Reload
	// R asks for others; the first load is the first round.
	l.reload = func() {
		if l.loaded {
			round++
		}
		reload()
	}

	return l
}
