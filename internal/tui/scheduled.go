package tui

import (
	"context"
	"fmt"
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// scheduledView is the user's queue of scheduled commits, as the web's Scheduled page lists it: ⏎ commits one now
// and D takes it off the queue.
type scheduledView struct {
	*pager[*api.ScheduledCommit]
}

func newScheduled(app *App) *scheduledView {
	q := &scheduledView{}
	q.pager = newPager(app, "Scheduled", emptyState("nothing scheduled", "^t in the composer schedules a new commit for later."), scheduledLines)
	q.end = ""
	q.load = func(ctx context.Context, cursor string) ([]*api.ScheduledCommit, string, error) {
		page := 1
		if cursor != "" {
			page, _ = strconv.Atoi(cursor)
		}
		res, err := app.client.Scheduled(ctx, page)
		if err != nil {
			return nil, "", err
		}
		next := ""
		if res.Meta.CurrentPage < res.Meta.LastPage {
			next = strconv.Itoa(res.Meta.CurrentPage + 1)
		}

		return pointers(res.Data), next, nil
	}

	return q
}

func scheduledLines(c *api.ScheduledCommit, selected bool, width int) []string {
	bar := " "
	if selected {
		bar = fmt.Sprintf("[%s]▌[-]", colorPrimary)
	}
	inner := max(width-3, 10)

	when := c.ScheduledAt.Local().Format("Mon Jan 2 15:04")
	status := fmt.Sprintf("[%s]◷ %s[-]", colorPrimary, when)
	switch c.Status {
	case "scheduled":
	case "deleted":
		status = fmt.Sprintf("[%s]went out %s, deleted since[-]", colorFaint, when)
	case "pending":
		status = fmt.Sprintf("[%s]went out %s · pending review[-]", colorWarning, when)
	case "rejected":
		status = fmt.Sprintf("[%s]went out %s · rejected[-]", colorDanger, when)
	default:
		status = fmt.Sprintf("[%s]✓ went out %s[-]", colorSuccess, when)
	}

	lines := []string{bar + " " + status}
	if c.Body != nil {
		body := truncateLines(bodyLines(c.PreviewSegments, *c.Body, nil, inner), 4)
		if c.PreviewSegments == nil {
			body = truncateLines(bodyLines(c.BodySegments, *c.Body, nil, inner), 4)
		}
		lines = append(lines, prefixed(bar+"  ", body)...)
	}
	var carries []string
	if n := c.ImageCount; n > 0 {
		carries = append(carries, fmt.Sprintf("▣ %d image%s", n, plural(n)))
	}
	if len(c.PollOptions) > 0 {
		carries = append(carries, "▤ poll: "+tview.Escape(firstLine(fmt.Sprint(c.PollOptions), inner-20)))
	}
	if len(carries) > 0 {
		lines = append(lines, bar+"  "+fmt.Sprintf("[%s]%s[-]", colorMuted, joinDot(carries)))
	}

	return append(lines, "")
}

func joinDot(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " · "
		}
		out += p
	}

	return out
}

func (q *scheduledView) Hints() []hint {
	hints := []hint{{"j/k", "move"}, {"c", "commit"}}
	if c, ok := q.Selected(); ok && c.Status == "scheduled" {
		hints = append(hints, hint{"⏎", "commit now"}, hint{"D", "cancel"})
	}

	return hints
}

func (q *scheduledView) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	list := q.listView.InputHandler()

	return func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		c, ok := q.Selected()
		switch {
		case !ok || c.Status != "scheduled":
		case event.Key() == tcell.KeyEnter:
			q.submitNow(c)
			return
		case event.Key() == tcell.KeyRune && event.Rune() == 'D':
			q.cancel(c)
			return
		}
		list(event, setFocus)
	}
}

func (q *scheduledView) submitNow(c *api.ScheduledCommit) {
	q.app.confirm("Commit it now, before its time?", func() {
		q.app.act(func(ctx context.Context) error {
			_, err := q.app.client.SubmitScheduled(ctx, c.ID)
			return err
		}, func(err error) {
			if err != nil {
				q.app.flashError(err)
				q.Reload()
				return
			}
			q.app.succeed("Committed it now. It shows on your profile once it's reviewed.")
			q.Reload()
		})
	})
}

func (q *scheduledView) cancel(c *api.ScheduledCommit) {
	q.app.confirm("Cancel this scheduled commit? It never goes out.", func() {
		q.app.act(func(ctx context.Context) error {
			return q.app.client.CancelScheduled(ctx, c.ID)
		}, func(err error) {
			if err != nil {
				q.app.flashError(err)
				q.Reload()
				return
			}
			for i, item := range q.items {
				if item == c {
					q.Remove(i)
				}
			}
			q.app.succeed("Canceled it.")
		})
	})
}
