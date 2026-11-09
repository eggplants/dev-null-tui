package tui

import (
	"context"
	"fmt"
	"html"
	"path/filepath"
	"sort"
	"strconv"
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
	// done runs after the commit was made, with it when the API answered with it.
	done func(created *api.Commit)
}

// composeArea is the composer's text area, which takes a pasted image path, as a file dropped on the terminal
// pastes, as an image to attach.
type composeArea struct {
	*tview.TextArea
	pasted func(text string) bool
}

func (c *composeArea) PasteHandler() func(text string, setFocus func(p tview.Primitive)) {
	paste := c.TextArea.PasteHandler()

	return func(text string, setFocus func(p tview.Primitive)) {
		if c.pasted == nil || !c.pasted(text) {
			paste(text, setFocus)
		}
	}
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
	// extras are the poll and the time the commit is scheduled for, if it has them.
	var extras commitExtras

	area := &composeArea{TextArea: tview.NewTextArea().SetPlaceholder("What did you ship?").SetWordWrap(true)}
	popover := tcell.StyleDefault.Background(tcell.GetColor(theme.popover))
	area.SetPlaceholderStyle(popover.Foreground(tcell.GetColor(colorFaint)))
	area.SetTextStyle(popover.Foreground(tcell.GetColor(colorInk)))
	area.SetSelectedStyle(tcell.StyleDefault.Background(tcell.GetColor(colorPrimary)).Foreground(tcell.GetColor(theme.popover)))
	preview := tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	attached := tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	completions := tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	counter := tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	for _, t := range []*tview.TextView{preview, attached, completions, counter} {
		t.SetBackgroundColor(tcell.GetColor(theme.popover))
	}

	if about == nil {
		if draft := a.sys.Draft(); draft != "" {
			area.SetText(draft, true)
		}
	}

	form := tview.NewFlex().SetDirection(tview.FlexRow)
	if about != nil {
		quoted := tview.NewTextView().SetDynamicColors(true).SetWrap(false)
		quoted.SetBackgroundColor(tcell.GetColor(theme.popover))
		lines := commitLines(about, false, false, composeWidth-2)
		lines = truncateLines(lines[:len(lines)-1], 5)
		quoted.SetText(strings.Join(lines, "\n"))
		form.AddItem(quoted, len(lines), 0, false)
		label := "replying to"
		if opts.quoted != nil {
			label = "quoting"
		}
		replying := tview.NewTextView().SetDynamicColors(true).SetText(fmt.Sprintf(" [%s]│[-] [%s]↳ %s @%s[-]", laneColor(about.Author), colorMuted, label, authorUsername(about)))
		replying.SetBackgroundColor(tcell.GetColor(theme.popover))
		form.AddItem(replying, 1, 0, false)
	}
	form.AddItem(area, 0, 1, true).AddItem(preview, 0, 0, false).AddItem(attached, 0, 0, false).AddItem(completions, 1, 0, false).AddItem(counter, 1, 0, false)
	form.SetBorder(true).SetTitle(title).SetBackgroundColor(tcell.GetColor(theme.popover))
	area.SetBackgroundColor(tcell.GetColor(theme.popover))

	showingPreview := false
	updatePreview := func() {
		if !showingPreview {
			form.ResizeItem(area, 0, 1).ResizeItem(preview, 0, 0)
			return
		}
		body := normalizeBody(area.GetText())
		lines := bodyLines(previewSegments(body), body, nil, composeWidth-6)
		if len(lines) == 0 {
			lines = []string{fmt.Sprintf("[%s]Nothing to preview yet.[-]", colorFaint)}
		}
		preview.SetText(strings.Join(prefixed(fmt.Sprintf(" [%s]│[-] ", laneColor(&a.me.User)), lines), "\n"))
		form.ResizeItem(area, 0, 1).ResizeItem(preview, 0, 1)
	}

	var mention *mentionState
	var issue *issueState
	// asking counts the suggestions asked for, so only the last of a quick run of keys is sent.
	asking := 0
	var updateCounter func()
	// issues are the /dev/null issues found for each #number typed so far, nil for none.
	issues := map[int]*api.Issue{}
	suggestIssue := func(number int) {
		asking++
		ask := asking
		time.AfterFunc(mentionDelay, func() {
			a.tv.QueueUpdate(func() {
				if ask != asking {
					return
				}
				var found *api.Issue
				a.async(func(ctx context.Context) (err error) {
					found, err = a.client.SuggestIssue(ctx, number)
					return err
				}, func(err error) {
					if err == nil {
						issues[number] = found
						updateCounter()
					}
				})
			})
		})
	}
	// suggested are the people the API suggests for each mention typed so far.
	suggested := map[string][]api.User{}
	suggestMentions := func(prefix string) {
		asking++
		ask := asking
		time.AfterFunc(mentionDelay, func() {
			a.tv.QueueUpdate(func() {
				if ask != asking {
					return
				}
				var found []api.User
				a.async(func(ctx context.Context) (err error) {
					reply, quote := "", ""
					if opts.parent != nil {
						reply = opts.parent.ID
					}
					if opts.quoted != nil {
						quote = opts.quoted.ID
					}
					found, err = a.client.SuggestMentions(ctx, prefix, reply, quote)
					return err
				}, func(err error) {
					// Suggestions only add to the people on screen, so one that fails goes without.
					if err != nil {
						return
					}
					suggested[prefix] = found
					updateCounter()
				})
			})
		})
	}
	updateCounter = func() {
		n, oversized := bodyLength(area.GetText())
		color := colorMuted
		if n > maxBodyLength || oversized {
			color = colorDanger
		}
		// The ring of the web's avatar fills up as a commit nears its limit; here a bar does.
		const ring = 8
		filled := min(ring, n*ring/maxBodyLength)
		bar := fmt.Sprintf("[%s]%s[%s]%s[-]", color, strings.Repeat("━", filled), colorRail, strings.Repeat("━", ring-filled))
		key := func(k, label string) string {
			return fmt.Sprintf("[%s::b]%s[-::-] [%s]%s[-]", colorInk, k, colorMuted, label)
		}
		keys := strings.Join([]string{key("^s", "commit"), key("^e", "editor"), key("^p", "preview"), key("^o", "image")}, " ")
		if about == nil {
			keys += " " + key("^l", "poll") + " " + key("^t", "later")
		}
		counter.SetText(spread(keys, fmt.Sprintf("%s [%s]%d/%d[-]", bar, color, n, maxBodyLength), composeWidth-2))

		// One line per image, so a long name doesn't push the others out of sight.
		lines := append(attachedLines(images, composeWidth-2), extras.lines(composeWidth-2)...)
		form.ResizeItem(attached, len(lines), 0)
		attached.SetText(strings.Join(lines, "\n"))

		mention = a.mentionAt(area.TextArea)
		if mention != nil {
			if found, ok := suggested[mention.prefix]; ok {
				mention.add(found)
			} else {
				suggestMentions(mention.prefix)
			}
		}
		issue = issueAt(area.TextArea)
		if issue != nil {
			if found, ok := issues[issue.number]; ok {
				issue.found, issue.checked = found, true
			} else {
				suggestIssue(issue.number)
			}
		}
		switch {
		case mention != nil:
			completions.SetText(mention.line(composeWidth - 2))
		case issue != nil:
			completions.SetText(issue.line(composeWidth - 2))
		default:
			completions.SetText(fmt.Sprintf("[%s]@ and tab completes a name, # an issue[-]", colorFaint))
		}
		if showingPreview {
			updatePreview()
		}
	}
	area.SetChangedFunc(updateCounter)
	area.SetMovedFunc(updateCounter)
	updateCounter()

	attach := func(img attachment) {
		images = append(images, img)
		updateCounter()
	}
	// canAttach tells whether another image can go on the commit, saying why not if it can't.
	canAttach := func() bool {
		switch {
		case extras.poll != nil:
			a.fail("A commit can't carry a poll and images both. ^l takes the poll off.")
		case len(images) >= commit.MaxImages:
			a.fail("A commit can carry up to %d images.", commit.MaxImages)
		default:
			return true
		}
		return false
	}
	area.pasted = func(text string) bool {
		if extras.poll != nil || len(images) >= commit.MaxImages || strings.Contains(strings.TrimSpace(text), "\n") {
			return false
		}
		img, err := checkImage(text)
		if err != nil || !isImageName(img.path) {
			return false
		}
		attach(img)
		a.succeed("Attached %s.", tview.Escape(filepath.Base(img.path)))
		return true
	}

	closeComposer := func() { a.closeModal("compose") }

	submitting := false
	area.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Ctrl+Enter commits as on the web, in the terminals that tell it from Enter.
		if event.Key() == tcell.KeyEnter && event.Modifiers()&(tcell.ModCtrl|tcell.ModMeta) != 0 {
			event = tcell.NewEventKey(tcell.KeyCtrlS, 0, tcell.ModCtrl)
		}

		switch event.Key() {
		case tcell.KeyEscape:
			text := strings.TrimSpace(area.GetText())
			switch {
			case text == "" && len(images) == 0:
				if about == nil {
					a.sys.SaveDraft("")
				}
				closeComposer()
			case about == nil:
				// A new commit's text is kept for next time, as the web's stash@{0}.
				a.sys.SaveDraft(area.GetText())
				closeComposer()
				a.flash("Draft kept in stash@{0}. c brings it back.")
			default:
				a.confirmDiscard(area.TextArea)
			}
			return nil
		case tcell.KeyCtrlS:
			if !submitting {
				submitting = true
				a.submit(strings.TrimSpace(area.GetText()), opts, images, extras, func() { submitting = false }, func() {
					if about == nil {
						a.sys.SaveDraft("")
					}
				})
			}
			return nil
		case tcell.KeyTab:
			if mention != nil && len(mention.matches) > 0 {
				mention.complete(area.TextArea)
				return nil
			}
			if issue != nil && issue.found != nil {
				area.Replace(issue.start, issue.end, issue.found.URL+" ")
				return nil
			}
			return event
		case tcell.KeyCtrlP:
			showingPreview = !showingPreview
			updatePreview()
			return nil
		case tcell.KeyCtrlE:
			a.editInEditor(area.TextArea)
			return nil
		case tcell.KeyCtrlO:
			if canAttach() {
				a.pickImage(attach)
			}
			return nil
		case tcell.KeyCtrlV:
			if canAttach() {
				a.pasteImage(attach)
			}
			return nil
		case tcell.KeyCtrlR:
			if len(images) > 0 {
				images = images[:len(images)-1]
				updateCounter()
			}
			return nil
		case tcell.KeyCtrlG:
			if len(images) == 0 {
				a.flash("Attach an image with ^o first, then ^g describes it.")
				return nil
			}
			a.describeImage(images, func(i int, text string) {
				if i < len(images) {
					images[i].description = text
					updateCounter()
				}
			})
			return nil
		case tcell.KeyCtrlL:
			switch {
			case about != nil:
				a.fail("Only a new commit can ask a poll, not a reply or a quote.")
			case len(images) > 0:
				a.fail("A commit can't carry a poll and images both. ^r takes the images off.")
			default:
				a.editPoll(extras.poll, func(p *api.NewPoll) {
					extras.poll = p
					updateCounter()
				})
			}
			return nil
		case tcell.KeyCtrlT:
			if about != nil {
				a.fail("Only a new commit can be scheduled, not a reply or a quote.")
				return nil
			}
			a.askSchedule(extras.at, func(at *time.Time) {
				extras.at = at
				updateCounter()
			})
			return nil
		}

		return event
	})

	height := 19
	if about != nil {
		height = 24
	}
	_, _, _, screenHeight := a.root.GetRect()
	if screenHeight > 0 {
		height = min(height, screenHeight)
	}
	a.openModal("compose", centered(form, composeWidth, height))
}

func authorUsername(c *api.Commit) string {
	if c.Author == nil {
		return "unknown"
	}

	return c.Author.Username
}

// issueState is a #number being typed, and the issue it is, once found.
type issueState struct {
	start, end int
	number     int
	found      *api.Issue
	// checked is set once the API answered, with found nil when there is no such issue.
	checked bool
}

// issueAt finds the #number the cursor is at the end of, if it is.
func issueAt(area *tview.TextArea) *issueState {
	text := area.GetText()
	_, cursor, _ := area.GetSelection()
	if cursor > len(text) {
		return nil
	}

	start := cursor
	for start > 0 && text[start-1] >= '0' && text[start-1] <= '9' {
		start--
	}
	if start == cursor || start == 0 || text[start-1] != '#' || text[start] == '0' || cursor-start > 10 {
		return nil
	}
	start--
	if start > 0 {
		if r, _ := utf8.DecodeLastRuneInString(text[:start]); r != ' ' && r != '\n' && r != '(' {
			return nil
		}
	}

	number, err := strconv.Atoi(text[start+1 : cursor])
	if err != nil {
		return nil
	}

	return &issueState{start: start, end: cursor, number: number}
}

func (i *issueState) line(width int) string {
	switch {
	case i.found == nil && i.checked:
		return fmt.Sprintf("[%s]/dev/null has no issue #%d[-]", colorFaint, i.number)
	case i.found == nil:
		return fmt.Sprintf("[%s]#%d: looking for the /dev/null issue…[-]", colorFaint, i.number)
	}

	return truncateLines([]string{fmt.Sprintf("[%s::b]tab[-::-] [%s::b]#%d[-::-] [%s]%s[-]", colorInk, colorPrimary, i.found.Number, colorMuted, tview.Escape(i.found.Title))}, 1)[0]
}

// mentionDelay is how long the composer waits for the next key before asking whom a mention being typed could be.
const mentionDelay = 250 * time.Millisecond

// composeWidth is the composer's width, border included.
const composeWidth = 78

// previewSegments splits a body into prose, inline code and code blocks the way the server's CodeSpans does, but
// without highlighting the code, for the composer's preview.
func previewSegments(body string) []api.Segment {
	parts := splitCode(body)
	var segments []api.Segment
	isBlock := func(i int) bool { return i >= 0 && i < len(parts) && i%2 == 1 && strings.HasPrefix(parts[i], "```") }

	for i, part := range parts {
		if i%2 == 1 {
			if isBlock(i) {
				lines := strings.Split(part, "\n")
				language, _, _ := strings.Cut(strings.TrimLeft(lines[0], "`"), " ")
				code := ""
				if len(lines) > 2 {
					code = strings.Join(lines[1:len(lines)-1], "\n")
				}
				segments = append(segments, api.Segment{Type: "block", Language: language, HTML: html.EscapeString(code)})
			} else {
				segments = append(segments, api.Segment{Type: "code", Code: part[1 : len(part)-1]})
			}
			continue
		}

		if isBlock(i - 1) {
			part = strings.TrimPrefix(part, "\n")
		}
		if isBlock(i + 1) {
			part = strings.TrimSuffix(part, "\n")
		}
		if part != "" {
			segments = append(segments, api.Segment{Type: "text", Text: part})
		}
	}

	return segments
}

// attachedLines lists the images picked so far, a line each, cutting names down to fit width.
func attachedLines(images []attachment, width int) []string {
	const keys = "  ^g alt ^r remove"

	lines := make([]string, len(images))
	for i, img := range images {
		size := fmt.Sprintf(" (%s)", humanSize(img.size))
		if img.description != "" {
			size += " · alt"
		}
		name := shortenName(filepath.Base(img.path), width-uniseg.StringWidth("▣ "+size+keys))
		lines[i] = fmt.Sprintf("[%s]▣ %s%s", colorPrimary, tview.Escape(name), size)
		// ^g describes them and ^r takes off the last one.
		if i == len(images)-1 {
			lines[i] += keys
		}
		lines[i] += "[-]"
	}

	return lines
}

func isImageName(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	}

	return false
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
	styleModal(modal)
	a.openModal("discard", modal)
}

// editInEditor hands the text to the user's editor, with the terminal theirs until they're done.
func (a *App) editInEditor(area *tview.TextArea) {
	var text string
	var err error
	a.tv.Suspend(func() { text, err = a.sys.Edit(area.GetText()) })
	if err != nil {
		a.flashError(err)
		return
	}
	area.SetText(text, true)
}

// pasteImage attaches the image on the clipboard, saved to a temporary file.
func (a *App) pasteImage(attach func(attachment)) {
	var path string
	a.act(func(ctx context.Context) (err error) {
		path, err = a.sys.ClipboardImage(ctx)
		return err
	}, func(err error) {
		if err != nil {
			a.flashError(err)
			return
		}
		img, err := checkImage(path)
		if err != nil {
			a.flashError(err)
			return
		}
		attach(img)
		a.succeed("Attached the image on the clipboard.")
	})
}

// mentionState is an @name being typed, and the people it could be.
type mentionState struct {
	start, end int
	// prefix is what was typed after the @, in lower case.
	prefix  string
	matches []*api.User
	index   int
}

// maxMentionMatches is how many people a mention being typed suggests.
const maxMentionMatches = 5

// add suggests more people after those already suggested, as the API finds them.
func (m *mentionState) add(people []api.User) {
	seen := map[string]bool{}
	for _, u := range m.matches {
		seen[strings.ToLower(u.Username)] = true
	}
	for i := range people {
		if u := &people[i]; len(m.matches) < maxMentionMatches && !seen[strings.ToLower(u.Username)] {
			seen[strings.ToLower(u.Username)] = true
			m.matches = append(m.matches, u)
		}
	}
}

// mentionAt finds the @name the cursor is in, and who it could be among the people already on screen.
func (a *App) mentionAt(area *tview.TextArea) *mentionState {
	text := area.GetText()
	_, cursor, _ := area.GetSelection()
	if cursor > len(text) {
		return nil
	}

	start := cursor
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:start])
		if r == '@' {
			start -= size
			break
		}
		if !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return nil
		}
		start -= size
	}
	if start >= len(text) || text[start] != '@' {
		return nil
	}
	// An @ in the middle of a word is an email address, not a mention.
	if start > 0 {
		if r, _ := utf8.DecodeLastRuneInString(text[:start]); r != ' ' && r != '\n' && r != '(' {
			return nil
		}
	}

	prefix := strings.ToLower(text[start+1 : cursor])
	return &mentionState{start: start, end: cursor, prefix: prefix, matches: a.peopleMatching(prefix)}
}

// peopleMatching finds the people on screen whose username starts with prefix first, then those whose name has it.
func (a *App) peopleMatching(prefix string) []*api.User {
	seen := map[string]*api.User{}
	add := func(u *api.User) {
		if u != nil && u.Username != "" && (a.me == nil || u.ID != a.me.ID) {
			seen[strings.ToLower(u.Username)] = u
		}
	}
	a.views(func(v view) {
		if h, ok := v.(commitHolder); ok {
			h.eachCommit(func(c *api.Commit) { add(c.Author) })
		}
		if n, ok := v.(*notifications); ok {
			for _, item := range n.items {
				add(item.Actor)
			}
		}
		if p, ok := v.(*profileView); ok {
			if p.profile != nil {
				add(&p.profile.Data)
			}
			for _, t := range p.tabs {
				if l, ok := t.(*peopleList); ok {
					for _, u := range l.items {
						add(u)
					}
				}
			}
		}
	})

	var byName, byFullName []*api.User
	for name, u := range seen {
		switch {
		case strings.HasPrefix(name, prefix):
			byName = append(byName, u)
		case prefix != "" && strings.Contains(strings.ToLower(u.Name), prefix):
			byFullName = append(byFullName, u)
		}
	}
	sort.Slice(byName, func(i, j int) bool { return byName[i].Username < byName[j].Username })
	sort.Slice(byFullName, func(i, j int) bool { return byFullName[i].Username < byFullName[j].Username })

	matches := append(byName, byFullName...)
	return matches[:min(len(matches), maxMentionMatches)]
}

func (m *mentionState) line(width int) string {
	if len(m.matches) == 0 {
		return fmt.Sprintf("[%s]nobody goes by that[-]", colorFaint)
	}

	parts := []string{fmt.Sprintf("[%s::b]tab[-::-]", colorInk)}
	for i, u := range m.matches {
		style := colorMuted
		if i == m.index {
			style = colorPrimary + "::b"
		}
		parts = append(parts, fmt.Sprintf("[%s]@%s[-::-]", style, tview.Escape(u.Username)))
	}

	return truncateLines([]string{strings.Join(parts, " ")}, 1)[0]
}

// complete puts the picked name in place of what was typed of it.
func (m *mentionState) complete(area *tview.TextArea) {
	u := m.matches[m.index%len(m.matches)]
	area.Replace(m.start, m.end, "@"+u.Username+" ")
}

// submit sends the draft and closes the composer, or calls failed and leaves it open so the draft can be fixed.
func (a *App) submit(body string, opts composeOptions, images []attachment, extras commitExtras, failed, sent func()) {
	// A commit with images may go without words, as on the web.
	if (len(images) == 0 || body != "") && !a.checkLength(body) {
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
	draft.Poll = extras.poll
	draft.ImageDescriptions = descriptions(images)
	if extras.at != nil {
		if !extras.at.After(now()) {
			a.fail("That time has passed. ^t picks another.")
			failed()
			return
		}
		a.schedule(draft, *extras.at, images, failed, sent)
		return
	}

	timeout := apiTimeout
	if len(images) > 0 {
		// Uploads can take longer than the API's usual timeout.
		timeout = uploadTimeout
		a.flash("Uploading %d image%s…", len(images), plural(len(images)))
	}

	var created *api.Commit
	a.actFor(timeout, func(ctx context.Context) (err error) {
		if len(images) == 0 {
			created, err = a.client.CreateCommit(ctx, draft)
			return err
		}
		files, err := readAttachments(images)
		if err != nil {
			return err
		}
		created, err = a.client.CreateCommitWithImages(ctx, draft, files)
		return err
	}, func(err error) {
		if err != nil {
			a.flashError(err)
			failed()
			return
		}

		sent()
		a.closeModal("compose")
		summary := tview.Escape(firstLine(body, 40))
		if created.Status == "pending" {
			a.pendingReview[created.ID] = created
			a.succeed("[log %s] %s · pending review, so only you see it until it's approved.", created.ShortHash, summary)
		} else {
			a.succeed("[log %s] %s", created.ShortHash, summary)
		}
		a.committed(created)
		if opts.done != nil {
			opts.done(created)
		}
	})
}

// committed puts a commit the user just made on top of the logs and their profile.
func (a *App) committed(c *api.Commit) {
	if c.Author == nil && a.me != nil {
		c.Author = &a.me.User
	}
	for _, t := range a.tabs {
		switch root := t.stack[0].(type) {
		case *commitFeed:
			if (root.log == api.LogLatest || root.log == api.LogFollowing) && c.ParentID == nil {
				root.prepend(c)
			}
		case *profileView:
			if root.isMe() {
				root.prepend(c)
			}
		}
	}
}

// checkLength tells the user what is wrong with the body's length, if anything, before the server would.
func (a *App) checkLength(body string) bool {
	n, oversized := bodyLength(body)
	switch {
	case oversized:
		a.fail("Each link must not be greater than %d characters.", maxURLLength)
	case n < minBodyLength || n > maxBodyLength || utf8.RuneCountInString(body) > maxRawBodyLength:
		a.fail("A commit is %d to %d characters, counting each link as up to %d.", minBodyLength, maxBodyLength, urlWeight)
	default:
		return true
	}

	return false
}
