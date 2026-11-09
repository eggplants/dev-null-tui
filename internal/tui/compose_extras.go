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

// commitExtras are what a new commit can carry besides its words and images: a poll, and the time it is scheduled
// for.
type commitExtras struct {
	poll *api.NewPoll
	at   *time.Time
	// participants are the people besides its author the parent was addressed to, whom a reply goes to as well unless
	// they're excluded, as the web's Replying to has them.
	participants []api.Participant
	excluded     map[string]bool
	// author is the handle of the organization the commit is posted as, or empty for the user.
	author string
}

// recipients are the ids of the participants a reply goes to.
func (e commitExtras) recipients() []string {
	var ids []string
	for _, p := range e.participants {
		if !e.excluded[p.ID] {
			ids = append(ids, p.ID)
		}
	}

	return ids
}

// replyingTo says whom a reply goes to: the parent's author and the participants it includes.
func (e commitExtras) replyingTo(author string) string {
	names := []string{"@" + author}
	for _, p := range e.participants {
		if !e.excluded[p.ID] {
			names = append(names, "@"+p.Handle)
		}
	}
	text := "↳ replying to " + strings.Join(names, " ")
	if len(e.participants) > 0 {
		text += " · ^n choose"
	}

	return text
}

// chooseRecipients asks which of the participants a reply goes to, one toggle at a time, until Done.
func (a *App) chooseRecipients(e *commitExtras, done func()) {
	labels := make([]string, 0, len(e.participants)+1)
	for _, p := range e.participants {
		mark := "✓"
		if e.excluded[p.ID] {
			mark = "·"
		}
		labels = append(labels, mark+" @"+p.Handle)
	}
	labels = append(labels, "Done")

	a.choose("Who else does the reply go to?", labels, func(label string) {
		for i, p := range e.participants {
			if labels[i] == label {
				if e.excluded == nil {
					e.excluded = map[string]bool{}
				}
				e.excluded[p.ID] = !e.excluded[p.ID]
				done()
				a.chooseRecipients(e, done)
				return
			}
		}
		done()
	})
}

// lines say what the commit carries, under its images in the composer.
func (e commitExtras) lines(width int) []string {
	var lines []string
	if p := e.poll; p != nil {
		text := fmt.Sprintf("▤ poll: %s · %s", strings.Join(p.Options, " / "), pollDurationLabel(p.Duration))
		lines = append(lines, fmt.Sprintf("[%s]%s  ^l change[-]", colorPrimary, tview.Escape(firstLine(text, width-12))))
	}
	if e.at != nil {
		lines = append(lines, fmt.Sprintf("[%s]◷ goes out %s  ^t change[-]", colorPrimary, e.at.Local().Format("Mon Jan 2 15:04")))
	}

	return lines
}

func pollDurationLabel(minutes int) string {
	for _, d := range api.PollDurations {
		if d.Minutes == minutes {
			return d.Label
		}
	}

	return fmt.Sprintf("%d minutes", minutes)
}

// editPoll asks for a poll's options and how long it stays open, starting from the one the commit has, if any. done
// runs with the poll, or nil once it's taken off.
func (a *App) editPoll(current *api.NewPoll, done func(p *api.NewPoll)) {
	form := tview.NewForm()
	options := make([]*tview.InputField, api.MaxPollOptions)
	for i := range options {
		value := ""
		if current != nil && i < len(current.Options) {
			value = current.Options[i]
		}
		label := fmt.Sprintf("Option %d", i+1)
		if i >= api.MinPollOptions {
			label += " (if any)"
		}
		options[i] = tview.NewInputField().SetLabel(label).SetText(value).SetFieldWidth(api.MaxPollOptionLength + 1).
			SetAcceptanceFunc(func(text string, _ rune) bool { return len([]rune(text)) <= api.MaxPollOptionLength })
		form.AddFormItem(options[i])
	}

	labels := make([]string, len(api.PollDurations))
	picked := 1
	for i, d := range api.PollDurations {
		labels[i] = d.Label
		if current != nil && d.Minutes == current.Duration {
			picked = i
		}
	}
	duration := tview.NewDropDown().SetLabel("Open for").SetOptions(labels, nil).SetCurrentOption(picked)
	form.AddFormItem(duration)

	closeForm := func() { a.closeModal("poll") }
	form.AddButton("Save", func() {
		var picked []string
		seen := map[string]bool{}
		for _, o := range options {
			text := strings.TrimSpace(o.GetText())
			if text == "" {
				continue
			}
			if seen[strings.ToLower(text)] {
				a.fail("Each option of a poll has to be different.")
				return
			}
			seen[strings.ToLower(text)] = true
			picked = append(picked, text)
		}
		if len(picked) < api.MinPollOptions {
			a.fail("A poll needs at least %d options.", api.MinPollOptions)
			return
		}
		i, _ := duration.GetCurrentOption()
		closeForm()
		done(&api.NewPoll{Options: picked, Duration: api.PollDurations[i].Minutes})
	})
	if current != nil {
		form.AddButton("Remove", func() {
			closeForm()
			done(nil)
		})
	}
	form.AddButton("Cancel", closeForm)
	form.SetCancelFunc(closeForm)

	form.SetBorder(true).SetTitle(" Poll ").SetBackgroundColor(tcell.GetColor(theme.popover))
	form.SetFieldBackgroundColor(tcell.GetColor(colorWash)).SetFieldTextColor(tcell.GetColor(colorInk)).
		SetLabelColor(tcell.GetColor(colorMuted)).SetButtonBackgroundColor(tcell.GetColor(colorWash)).
		SetButtonTextColor(tcell.GetColor(colorInk))

	a.openModal("poll", centered(form, 72, 2*api.MaxPollOptions+7))
}

// askSchedule asks when the commit goes out, starting from the time it has, if any. done runs with the time, or nil
// when the answer is empty, which commits it right away again.
func (a *App) askSchedule(current *time.Time, done func(at *time.Time)) {
	initial := ""
	if current != nil {
		initial = current.Local().Format("2006-01-02 15:04")
	}

	input := a.askLine("Commit it later", "+2h, 18:30, tomorrow 9:00, 2026-11-12 09:00 · empty: right away", initial, nil)
	input.done = func(text string) {
		if strings.TrimSpace(text) == "" {
			done(nil)
			return
		}
		at, err := parseWhen(text, now())
		switch {
		case err != nil:
			a.fail("%s", tview.Escape(err.Error()))
		case !at.After(now()):
			a.fail("That time has passed.")
		case at.After(now().Add(api.MaxScheduleAhead)):
			a.fail("A commit can be scheduled up to 30 days ahead.")
		default:
			done(&at)
		}
	}
}

// schedule queues the commit for later, closing the composer once it's in the queue.
func (a *App) schedule(draft api.NewCommit, at time.Time, images []attachment, failed, sent func()) {
	timeout := apiTimeout
	if len(images) > 0 {
		timeout = uploadTimeout
	}

	a.actFor(timeout, func(ctx context.Context) error {
		var files []api.File
		if len(images) > 0 {
			var err error
			if files, err = readAttachments(images); err != nil {
				return err
			}
		}
		_, err := a.client.Schedule(ctx, draft, at, files)
		return err
	}, func(err error) {
		if err != nil {
			a.flashError(err)
			failed()
			return
		}

		sent()
		a.closeModal("compose")
		a.rememberAuthor(draft.Author)
		a.succeed("Scheduled for %s. g q lists your queue.", at.Local().Format("Mon Jan 2 15:04"))
		a.views(func(v view) {
			if q, ok := v.(*scheduledView); ok {
				q.Reload()
			}
		})
	})
}

// askAuthor asks which organization to post as, by its handle, completing it from the organizations the search finds
// and those posted as before. done runs with the handle, or "" to post as the user. Only the server knows which
// organizations the user is a member of, so it is the one that says no to the others.
func (a *App) askAuthor(current string, done func(handle string)) {
	asked := map[string][]string{}
	var input *lineInput
	input = a.askLine("Post as", "an organization's @handle; empty posts as you", current, func(text string) []string {
		q := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(text), "@"))
		if q == "" {
			entries := make([]string, len(a.postedAs))
			for i, h := range a.postedAs {
				entries[i] = "@" + h
			}
			return entries
		}
		if entries, ok := asked[q]; ok {
			return entries
		}
		time.AfterFunc(mentionDelay, func() {
			a.tv.QueueUpdate(func() {
				if current := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(input.field.GetText()), "@")); current != q {
					return
				}
				var orgs []api.User
				a.async(func(ctx context.Context) (err error) {
					_, orgs, err = a.client.SearchProfiles(ctx, q)
					return err
				}, func(err error) {
					if err != nil {
						return
					}
					var entries []string
					for _, h := range a.postedAs {
						if strings.HasPrefix(h, q) {
							entries = append(entries, "@"+h)
						}
					}
					for _, o := range orgs {
						if !contains(entries, "@"+o.Username) {
							entries = append(entries, "@"+o.Username)
						}
					}
					asked[q] = entries[:min(len(entries), 8)]
					input.field.Autocomplete()
				})
			})
		})
		return nil
	})
	input.done = func(text string) {
		done(strings.TrimPrefix(strings.TrimSpace(text), "@"))
	}
}

// rememberAuthor keeps an organization the user posted as on top of the ones Alt+a offers.
func (a *App) rememberAuthor(handle string) {
	if handle == "" {
		return
	}
	kept := []string{handle}
	for _, h := range a.postedAs {
		if h != handle {
			kept = append(kept, h)
		}
	}
	a.postedAs = kept[:min(len(kept), 5)]
}
