package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rivo/uniseg"

	"github.com/eggplants/dev-null-tui/internal/api"
	"github.com/eggplants/dev-null-tui/internal/commit"
)

type composeOptions struct {
	parent *api.Commit
	quoted *api.Commit
	// done runs after the commit was made.
	done func()
}

// compose opens the composer for a new commit, a reply to parent, or a quote of quoted.
func (a *App) compose(opts composeOptions) {
	title, about := " New commit ", (*api.Commit)(nil)
	switch {
	case opts.parent != nil:
		title, about = " Reply ", opts.parent
	case opts.quoted != nil:
		title, about = " Quote ", opts.quoted
	}

	var images []attachment

	area := tview.NewTextArea().SetPlaceholder("What did you ship?").SetWordWrap(true)
	attached := tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	counter := tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignRight)

	form := tview.NewFlex().SetDirection(tview.FlexRow)
	if about != nil {
		preview := tview.NewTextView().SetDynamicColors(true)
		lines := commitLines(about, false, false, 70)
		preview.SetText(strings.Join(truncateLines(lines[:len(lines)-1], 4), "\n"))
		form.AddItem(preview, min(len(lines)-1, 4), 0, false)
	}
	form.AddItem(area, 0, 1, true).AddItem(attached, 0, 0, false).AddItem(counter, 1, 0, false)
	form.SetBorder(true).SetTitle(title)

	updateCounter := func() {
		n, oversized := bodyLength(area.GetText())
		color := colorMuted
		if n > maxBodyLength || oversized {
			color = "red"
		}
		counter.SetText(fmt.Sprintf("[%s]%d/%d  ^o image  ^s commit  esc cancel[-]", color, n, maxBodyLength))

		// One line per image, so a long name doesn't push the others out of sight.
		form.ResizeItem(attached, len(images), 0)
		attached.SetText(strings.Join(attachedLines(images, composeWidth-2), "\n"))
	}
	area.SetChangedFunc(updateCounter)
	updateCounter()

	submitting := false
	area.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			if strings.TrimSpace(area.GetText()) == "" && len(images) == 0 {
				a.closeModal("compose")
			} else {
				a.confirmDiscard(area)
			}
			return nil
		case tcell.KeyCtrlS:
			if !submitting {
				submitting = true
				a.submit(strings.TrimSpace(area.GetText()), opts, images, func() { submitting = false })
			}
			return nil
		case tcell.KeyCtrlO:
			switch {
			case opts.quoted != nil:
				a.flash("[red]A quote can't carry images.[-]")
			case len(images) >= commit.MaxImages:
				a.flash("[red]A commit can carry up to %d images.[-]", commit.MaxImages)
			default:
				a.pickImage(func(img attachment) {
					images = append(images, img)
					updateCounter()
				})
			}
			return nil
		case tcell.KeyCtrlR:
			if len(images) > 0 {
				images = images[:len(images)-1]
				updateCounter()
			}
			return nil
		}

		return event
	})

	a.openModal("compose", centered(form, composeWidth, 17))
}

// composeWidth is the composer's width, border included.
const composeWidth = 76

// attachedLines lists the images picked so far, a line each, cutting names down to fit width.
func attachedLines(images []attachment, width int) []string {
	const remove = "  ^r remove"

	lines := make([]string, len(images))
	for i, img := range images {
		size := fmt.Sprintf(" (%s)", humanSize(img.size))
		name := shortenName(filepath.Base(img.path), width-uniseg.StringWidth("▣ "+size+remove))
		lines[i] = fmt.Sprintf("[%s]▣ %s%s", colorLink, tview.Escape(name), size)
		// ^r takes off the last one.
		if i == len(images)-1 {
			lines[i] += remove
		}
		lines[i] += "[-]"
	}

	return lines
}

func (a *App) confirmDiscard(area *tview.TextArea) {
	modal := tview.NewModal().SetText("Discard this draft?").AddButtons([]string{"Keep editing", "Discard"}).
		SetDoneFunc(func(_ int, label string) {
			a.root.RemovePage("discard")
			if label == "Discard" {
				a.closeModal("compose")
			} else {
				a.tv.SetFocus(area)
			}
		})
	a.openModal("discard", modal)
}

// submit sends the draft and closes the composer, or calls failed and leaves it open so the draft can be fixed.
func (a *App) submit(body string, opts composeOptions, images []attachment, failed func()) {
	if len(images) > 0 {
		a.submitWithImages(body, opts, images, failed)
		return
	}

	if !a.checkLength(body) {
		failed()
		return
	}

	draft := api.NewCommit{Body: body}
	if opts.parent != nil {
		draft.ParentID = opts.parent.ID
	}
	if opts.quoted != nil {
		draft.QuotedCommitID = opts.quoted.ID
	}

	a.flash("[%s]Committing…[-]", colorMuted)
	var created *api.Commit
	a.async(func(ctx context.Context) (err error) {
		created, err = a.client.CreateCommit(ctx, draft)
		return err
	}, func(err error) {
		if err != nil {
			a.flashError(err)
			failed()
			return
		}

		a.closeModal("compose")
		if created.Status == "pending" {
			a.flash("Committed %s. It's pending review, so only you see it until it's approved.", created.ShortHash)
		} else {
			a.flash("Committed %s.", created.ShortHash)
		}
		if opts.done != nil {
			opts.done()
		}
	})
}

// checkLength tells the user what is wrong with the body's length, if anything, before the server would.
func (a *App) checkLength(body string) bool {
	n, oversized := bodyLength(body)
	switch {
	case oversized:
		a.flash("[red]Each link must not be greater than %d characters.[-]", maxURLLength)
	case n < minBodyLength || n > maxBodyLength || utf8.RuneCountInString(body) > maxRawBodyLength:
		a.flash("[red]A commit is %d to %d characters, counting each link as up to %d.[-]", minBodyLength, maxBodyLength, urlWeight)
	default:
		return true
	}

	return false
}

// submitWithImages sends a commit with images the way `dn commit --image` does, signed with an SSH key on the
// user's GitHub account: the signed API only takes JSON, so it can't carry images.
func (a *App) submitWithImages(body string, opts composeOptions, images []attachment, failed func()) {
	if body == "" {
		a.flash("[red]A commit with images needs some text.[-]")
		failed()
		return
	}
	if !a.checkLength(body) {
		failed()
		return
	}

	options := commit.Options{BaseURL: a.client.BaseURL, Message: body, Passphrase: a.askPassphrase}
	if opts.parent != nil {
		options.ReplyTo = opts.parent.ID
	}
	for _, img := range images {
		options.Images = append(options.Images, img.path)
	}

	a.flash("[%s]Uploading %d image(s)…[-]", colorMuted, len(images))
	var answer string
	a.async(func(context.Context) (err error) {
		// Uploads can take longer than the API's usual timeout.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		answer, err = a.commitWithImages(ctx, options)
		return err
	}, func(err error) {
		if err != nil {
			a.flashError(err)
			failed()
			return
		}

		a.closeModal("compose")
		a.flash("%s", tview.Escape(strings.TrimSpace(answer)))
		if opts.done != nil {
			opts.done()
		}
	})
}
