package tui

import (
	"context"
	"crypto/rand"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
	"github.com/eggplants/dev-null-tui/internal/commit"
)

// typingEvery is how often the composer says the user is typing while they are, as the web's does.
const typingEvery = 4 * time.Second

// write opens the message composer, for a reply to a message when reply is set.
func (t *remoteThread) write(reply *api.Message) {
	if t.thread != nil && (t.thread.Access == "restricted" || t.thread.Access == "closed" || t.thread.Access == "waiting") {
		t.app.fail("You can't push to them right now.")
		return
	}
	a := t.app

	var images []attachment
	area := &composeArea{TextArea: tview.NewTextArea().SetPlaceholder("Push a message…").SetWordWrap(true)}
	popover := tcell.StyleDefault.Background(tcell.GetColor(theme.popover))
	area.SetPlaceholderStyle(popover.Foreground(tcell.GetColor(colorFaint)))
	area.SetTextStyle(popover.Foreground(tcell.GetColor(colorInk)))
	area.SetBackgroundColor(tcell.GetColor(theme.popover))
	status := tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	attached := tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	for _, v := range []*tview.TextView{status, attached} {
		v.SetBackgroundColor(tcell.GetColor(theme.popover))
	}

	form := tview.NewFlex().SetDirection(tview.FlexRow)
	if reply != nil {
		who := "someone"
		if u := t.sender(reply.UserID); u != nil {
			who = "@" + u.Username
		}
		about := tview.NewTextView().SetDynamicColors(true).SetWrap(false).
			SetText(fmt.Sprintf(" [%s]↪ replying to %s: %s[-]", colorMuted, tview.Escape(who), tview.Escape(firstLine(messageSummary(reply), composeWidth-20))))
		about.SetBackgroundColor(tcell.GetColor(theme.popover))
		form.AddItem(about, 1, 0, false)
	}
	form.AddItem(area, 0, 1, true).AddItem(attached, 0, 0, false).AddItem(status, 1, 0, false)
	form.SetBorder(true).SetTitle(" Push to " + tview.Escape(t.title) + " ").SetBackgroundColor(tcell.GetColor(theme.popover))

	var typedAt time.Time
	update := func() {
		n := utf8.RuneCountInString(area.GetText())
		color := colorMuted
		if n > api.MaxMessageLength {
			color = colorDanger
		}
		keys := fmt.Sprintf("[%s::b]^s[-::-] [%s]push[-]  [%s::b]^o[-::-] [%s]image[-]  [%s::b]esc[-::-] [%s]close[-]", colorInk, colorMuted, colorInk, colorMuted, colorInk, colorMuted)
		status.SetText(spread(keys, fmt.Sprintf("[%s]%d/%d[-]", color, n, api.MaxMessageLength), composeWidth-2))
		lines := attachedLines(images, composeWidth-2)
		form.ResizeItem(attached, len(lines), 0)
		attached.SetText(strings.Join(lines, "\n"))
	}
	area.SetChangedFunc(func() {
		update()
		// Saying so every few seconds while there is text is what shows them the user is typing.
		if area.GetText() != "" && time.Since(typedAt) > typingEvery {
			typedAt = time.Now()
			a.async(func(ctx context.Context) error { return a.client.Typing(ctx, t.remote) }, func(error) {})
		}
	})
	update()

	attach := func(img attachment) {
		images = append(images, img)
		update()
	}
	area.pasted = func(text string) bool {
		if len(images) >= commit.MaxImages || strings.Contains(strings.TrimSpace(text), "\n") {
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

	sending := false
	area.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter && event.Modifiers()&(tcell.ModCtrl|tcell.ModMeta) != 0 {
			event = tcell.NewEventKey(tcell.KeyCtrlS, 0, tcell.ModCtrl)
		}
		switch event.Key() {
		case tcell.KeyEscape:
			if strings.TrimSpace(area.GetText()) == "" && len(images) == 0 {
				a.closeModal("message")
				return nil
			}
			a.confirm("Throw this message away?", func() { a.closeModal("message") })
			return nil
		case tcell.KeyCtrlS:
			body := strings.TrimSpace(area.GetText())
			switch {
			case sending:
			case body == "" && len(images) == 0:
				a.fail("Write something, or attach an image.")
			case utf8.RuneCountInString(body) > api.MaxMessageLength:
				a.fail("A message is up to %d characters.", api.MaxMessageLength)
			default:
				sending = true
				t.push(body, reply, images, func() { sending = false })
			}
			return nil
		case tcell.KeyCtrlO:
			if len(images) >= commit.MaxImages {
				a.fail("A message can carry up to %d images.", commit.MaxImages)
			} else {
				a.pickImage(attach)
			}
			return nil
		case tcell.KeyCtrlV:
			if len(images) < commit.MaxImages {
				a.pasteImage(attach)
			}
			return nil
		case tcell.KeyCtrlR:
			if len(images) > 0 {
				images = images[:len(images)-1]
				update()
			}
			return nil
		case tcell.KeyCtrlE:
			a.editInEditor(area.TextArea)
			return nil
		}
		return event
	})

	height := 10
	if reply != nil {
		height++
	}
	a.openModal("message", centered(form, composeWidth, height))
}

// push sends a message, closing the composer once it's in, or calls failed so it can be fixed.
func (t *remoteThread) push(body string, reply *api.Message, images []attachment, failed func()) {
	a := t.app
	draft := api.NewMessage{Body: body, ClientID: newUUID()}
	if reply != nil {
		id := reply.ID
		draft.ReplyToID = &id
	}

	timeout := apiTimeout
	if len(images) > 0 {
		timeout = uploadTimeout
	}
	var sent *api.Message
	a.actFor(timeout, func(ctx context.Context) error {
		var files []api.File
		if len(images) > 0 {
			var err error
			if files, err = readAttachments(images); err != nil {
				return err
			}
		}
		var err error
		sent, err = a.client.Push(ctx, t.remote, draft, files)
		return err
	}, func(err error) {
		if err != nil {
			a.flashError(err)
			failed()
			return
		}
		a.closeModal("message")
		if t.thread == nil {
			// The first message started the remote, which there is now a thread of to read.
			t.Reload()
		} else {
			t.add(sent)
		}
		a.reloadInbox(nil)
	})
}

// add puts a message the user just pushed at the bottom of the thread, unless polling brought it in already.
func (t *remoteThread) add(m *api.Message) {
	for _, item := range t.items {
		if item.ID == m.ID {
			return
		}
	}
	t.Append([]*api.Message{m})
	t.newest = max(t.newest, m.ID)
	t.readThrough = max(t.readThrough, m.ID)
	t.Select(len(t.items) - 1)
	t.updateFooter()
}

// newUUID makes a random UUID, for a message's client_id.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// orgMembers lists who is in an org, with what a maintainer can do to them: make them a maintainer or a contributor,
// or take them out. + adds people and L leaves.
type orgMembers struct {
	*listView[*api.OrgMember]

	app    *App
	thread *remoteThread
}

func newOrgMembers(app *App, thread *remoteThread) *orgMembers {
	o := &orgMembers{app: app, thread: thread}
	o.listView = newListView(o.render)

	return o
}

func (o *orgMembers) Title() string { return "Members" }

func (o *orgMembers) Reload() {
	if o.thread.thread == nil {
		return
	}
	o.SetItems(pointers(o.thread.thread.Members))
}

func (o *orgMembers) render(m *api.OrgMember, selected bool, width int) []string {
	bar := " "
	if selected {
		bar = fmt.Sprintf("[%s]▌[-]", colorPrimary)
	}
	role := pill(m.Role, colorMuted)
	if m.Role == "maintainer" {
		role = pill(m.Role, colorPrimary)
	}

	return []string{spread(bar+" "+authorName(&m.User), role+" ", width), ""}
}

// maintains tells whether the user maintains the org.
func (o *orgMembers) maintains() bool {
	return o.thread.thread != nil && o.thread.thread.Role != nil && *o.thread.thread.Role == "maintainer"
}

func (o *orgMembers) Hints() []hint {
	hints := []hint{{"j/k", "move"}, {"⏎", "profile"}}
	if o.maintains() {
		hints = append(hints, hint{"+", "add people"}, hint{"m", "maintainer / contributor"}, hint{"D", "remove"}, hint{"E", "rename"})
	}

	return append(hints, hint{"L", "leave"})
}

func (o *orgMembers) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	list := o.listView.InputHandler()
	org := o.thread.remote.OrgID

	return func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		m, ok := o.Selected()
		switch {
		case event.Key() == tcell.KeyEnter && ok:
			o.app.push(newProfile(o.app, m.User.Username))
		case event.Key() != tcell.KeyRune:
			list(event, setFocus)
		case event.Rune() == 'L':
			o.app.confirm("Leave this org? You stop getting its messages.", func() {
				o.change(func(ctx context.Context) error {
					return o.app.client.RemoveOrgMember(ctx, org, o.app.me.Username)
				}, "You left the org.", true)
			})
		case !o.maintains():
			list(event, setFocus)
		case event.Rune() == '+':
			o.app.pickCandidate("Add to the org", "org", org, func(u *api.User) {
				o.app.choose(fmt.Sprintf("Show @%s the messages from before they joined?", u.Username), []string{"Share history", "Only from now", "Cancel"}, func(label string) {
					if label == "Cancel" {
						return
					}
					share := label == "Share history"
					o.change(func(ctx context.Context) error {
						return o.app.client.AddOrgMembers(ctx, org, []string{u.Username}, share)
					}, fmt.Sprintf("Added @%s.", u.Username), false)
				})
			})
		case event.Rune() == 'm' && ok:
			role := "maintainer"
			if m.Role == "maintainer" {
				role = "contributor"
			}
			o.change(func(ctx context.Context) error {
				return o.app.client.SetOrgRole(ctx, org, m.User.Username, role)
			}, fmt.Sprintf("@%s is a %s now.", m.User.Username, role), false)
		case event.Rune() == 'D' && ok:
			o.app.confirm(fmt.Sprintf("Take @%s out of the org?", m.User.Username), func() {
				o.change(func(ctx context.Context) error {
					return o.app.client.RemoveOrgMember(ctx, org, m.User.Username)
				}, fmt.Sprintf("Took @%s out.", m.User.Username), false)
			})
		case event.Rune() == 'E':
			name := o.app.askLine("Rename the org", "empty names it after its members", o.thread.title, nil)
			name.done = func(text string) {
				title := strings.TrimSpace(text)
				o.change(func(ctx context.Context) error { return o.app.client.RenameOrg(ctx, org, title) }, "Renamed the org.", false)
			}
		default:
			list(event, setFocus)
		}
	}
}

// change does something to the org, then reads the thread again for who is in it, closing the list when the user
// left.
func (o *orgMembers) change(action func(ctx context.Context) error, done string, left bool) {
	o.app.act(action, func(err error) {
		if err != nil {
			o.app.flashError(err)
			return
		}
		o.app.succeed("%s", done)
		o.app.reloadInbox(nil)
		if left {
			o.app.pop()
			o.app.pop()
			return
		}

		var thread *api.RemoteThread
		o.app.async(func(ctx context.Context) (err error) {
			thread, err = o.app.client.Messages(ctx, o.thread.remote, api.ThreadPage{})
			return err
		}, func(err error) {
			if err != nil {
				return
			}
			o.thread.take(thread)
			o.Reload()
			o.thread.Reload()
		})
	})
}
