package tui

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/crypto/ssh"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// accountSettings are the rows of the settings kept on the server that only the user sees: the language commits
// are translated into, the timezone, going live and the devices.
func (s *settingsView) accountSettings() []*setting {
	app := s.app

	return []*setting{
		{
			label: "Translate into",
			value: s.languageValue,
			help:  "⏎ picks the language t translates commits into",
			enter: s.askLanguage,
		},
		{
			label: "Auto-translate",
			value: func() string {
				if s.settings == nil {
					return "…"
				}
				return onOff(s.settings.AutomaticTranslation)
			},
			help:  "⏎ turns translating commits in other languages by themselves on or off, on the web and the apps",
			enter: s.toggleAutomaticTranslation,
		},
		{
			label: "Timezone",
			value: func() string {
				if s.settings == nil {
					return "…"
				}
				if s.settings.Timezone == "" {
					return "not set"
				}
				return s.settings.Timezone
			},
			help:  "⏎ sets the timezone the web shows times in",
			enter: s.askTimezone,
		},
		{
			label: "Live",
			value: func() string {
				if app.me == nil || app.me.Live == nil {
					return "off"
				}
				return fmt.Sprintf("on %s until %s", app.me.Live.Platform, app.me.Live.EndsAt.Local().Format("15:04"))
			},
			help:  "⏎ says you're live on your YouTube or Twitch stream, or ends it",
			enter: s.toggleLive,
		},
		{
			label: "Devices",
			value: func() string { return "keys that sign in as you, like this one" },
			help:  "⏎ lists them, to revoke one",
			enter: func() { app.push(newDevices(app)) },
		},
	}
}

// loadSettings reads the settings only the user sees.
func (s *settingsView) loadSettings() {
	var settings *api.Settings
	s.app.async(func(ctx context.Context) (err error) {
		settings, err = s.app.client.Settings(ctx)
		return err
	}, func(err error) {
		if err != nil {
			s.app.flashError(err)
			return
		}
		s.settings = settings
		s.Invalidate()
	})
}

func (s *settingsView) languageValue() string {
	if s.settings == nil {
		return "…"
	}
	if s.settings.TranslationLanguage == nil {
		return "your device's"
	}

	code := *s.settings.TranslationLanguage
	for _, l := range s.settings.TranslationLanguages {
		if l.Code == code {
			return fmt.Sprintf("%s (%s)", l.Name, code)
		}
	}

	return code
}

// askLanguage asks for a language, by its name or code, completing it from those the server translates into.
func (s *settingsView) askLanguage() {
	if s.settings == nil {
		s.app.flash("Still reading your settings…")
		return
	}

	languages := s.settings.TranslationLanguages
	input := s.app.askLine("Translate commits into", "Japanese, ja, or empty for your device's", "", func(text string) []string {
		text = strings.ToLower(strings.TrimSpace(text))
		if text == "" {
			return nil
		}
		var matches []string
		for _, l := range languages {
			if strings.HasPrefix(strings.ToLower(l.Name), text) || strings.HasPrefix(l.Code, text) || strings.HasPrefix(strings.ToLower(l.NativeName), text) {
				matches = append(matches, fmt.Sprintf("%s (%s)", l.Name, l.Code))
			}
		}
		return matches[:min(len(matches), 10)]
	})
	input.done = func(text string) {
		text = strings.TrimSpace(text)
		var code *string
		if text != "" {
			found, ok := findLanguage(languages, text)
			if !ok {
				s.app.fail("/dev/null doesn't translate into %s.", tview.Escape(text))
				return
			}
			code = &found
		}
		var me *api.CurrentUser
		s.app.act(func(ctx context.Context) (err error) {
			me, err = s.app.client.SetTranslationLanguage(ctx, code)
			return err
		}, func(err error) {
			if err != nil {
				s.app.flashError(err)
				return
			}
			s.app.me = &me.Data
			s.settings.TranslationLanguage = code
			s.app.setTranslationLanguage(code)
			s.Invalidate()
			s.app.succeed("Commits are translated into %s now.", s.languageValue())
		})
	}
}

// findLanguage finds a language by its code, its name, or a completion like "Japanese (ja)".
func findLanguage(languages []api.Language, text string) (string, bool) {
	if open := strings.LastIndex(text, "("); open >= 0 && strings.HasSuffix(text, ")") {
		text = text[open+1 : len(text)-1]
	}
	for _, l := range languages {
		if strings.EqualFold(l.Code, text) || strings.EqualFold(l.Name, text) || strings.EqualFold(l.NativeName, text) {
			return l.Code, true
		}
	}

	return "", false
}

// setTranslationLanguage has t translate into the language the user picked, or the device's.
func (a *App) setTranslationLanguage(code *string) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	a.language = ""
	if code != nil {
		a.language = *code
	}
}

func (s *settingsView) toggleAutomaticTranslation() {
	if s.settings == nil {
		s.app.flash("Still reading your settings…")
		return
	}

	on := !s.settings.AutomaticTranslation
	s.app.act(func(ctx context.Context) error {
		_, err := s.app.client.SetAutomaticTranslation(ctx, on)
		return err
	}, func(err error) {
		if err != nil {
			s.app.flashError(err)
			return
		}
		s.settings.AutomaticTranslation = on
		s.Invalidate()
		s.app.succeed("Automatic translation is %s.", onOff(on))
	})
}

// askTimezone asks for an IANA timezone, completing it from the ones this machine knows.
func (s *settingsView) askTimezone() {
	current := ""
	if s.settings != nil {
		current = s.settings.Timezone
	}
	zones := timezones()
	input := s.app.askLine("Timezone", "Asia/Tokyo", current, func(text string) []string {
		text = strings.ToLower(strings.TrimSpace(text))
		if text == "" {
			return nil
		}
		var matches []string
		for _, z := range zones {
			if strings.Contains(strings.ToLower(z), text) {
				matches = append(matches, z)
			}
		}
		return matches[:min(len(matches), 10)]
	})
	input.done = func(text string) {
		timezone := strings.TrimSpace(text)
		if timezone == "" {
			return
		}
		s.app.act(func(ctx context.Context) error {
			_, err := s.app.client.SetTimezone(ctx, timezone)
			return err
		}, func(err error) {
			if err != nil {
				s.app.flashError(err)
				return
			}
			if s.settings != nil {
				s.settings.Timezone = timezone
			}
			s.Invalidate()
			s.app.succeed("Your timezone is %s.", tview.Escape(timezone))
		})
	}
}

// timezones are the IANA timezones, from the zone table this machine has, if it has one.
func timezones() []string {
	var zones []string
	for _, line := range strings.Split(zoneTable(), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if fields := strings.Fields(line); len(fields) >= 3 {
			zones = append(zones, fields[2])
		}
	}
	sort.Strings(zones)

	return append([]string{"UTC"}, zones...)
}

// toggleLive ends the user's stream, or asks for its link and how long it goes on to say they are live on it.
func (s *settingsView) toggleLive() {
	app := s.app
	if app.me != nil && app.me.Live != nil {
		app.confirm("End your stream? Nobody sees you as live any more.", func() {
			s.saveMe(app.client.EndLive, "Your stream ended.")
		})
		return
	}

	input := app.askLine("Your stream", "https://www.twitch.tv/you or a YouTube link", "", nil)
	input.done = func(text string) {
		link := strings.TrimSpace(text)
		if u, err := url.Parse(link); err != nil || u.Host == "" {
			app.fail("That isn't a link to a stream.")
			return
		}
		hours := app.askLine("For how many hours", fmt.Sprintf("1 to %d", api.MaxLiveHours), "2", nil)
		hours.done = func(text string) {
			n, err := strconv.Atoi(strings.TrimSpace(text))
			if err != nil || n < 1 || n > api.MaxLiveHours {
				app.fail("Say you're live for 1 to %d hours.", api.MaxLiveHours)
				return
			}
			s.saveMe(func(ctx context.Context) (*api.CurrentUser, error) { return app.client.GoLive(ctx, link, n) },
				fmt.Sprintf("You're live for %d hour%s.", n, plural(n)))
		}
	}
}

// lineInput is a one-line question in a dialog; done runs with the answer on Enter.
type lineInput struct {
	done func(text string)
}

// askLine asks a one-line question, completing the answer with complete if it is set.
func (a *App) askLine(title, placeholder, initial string, complete func(text string) []string) *lineInput {
	answer := &lineInput{}
	submit := func(text string) {
		a.closeModal("line")
		if answer.done != nil {
			answer.done(text)
		}
	}
	input := tview.NewInputField().SetText(initial).SetPlaceholder(placeholder).
		SetFieldBackgroundColor(tcell.GetColor(colorWash)).SetFieldTextColor(tcell.GetColor(colorInk)).
		SetPlaceholderTextColor(tcell.GetColor(colorFaint))
	input.SetBorder(true).SetTitle(" " + title + " ").SetBackgroundColor(tcell.GetColor(theme.popover))
	if complete != nil {
		input.SetAutocompleteFunc(complete)
		input.SetAutocompletedFunc(func(text string, _, source int) bool {
			input.SetText(text)
			// Enter on a completion takes it as the answer, as Enter on what was typed does.
			if source == tview.AutocompletedEnter {
				submit(text)
			}
			return source != tview.AutocompletedNavigate
		})
	}
	input.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			submit(input.GetText())
			return
		}
		a.closeModal("line")
	})

	_, _, width, _ := a.root.GetRect()
	a.openModal("line", centered(input, min(70, width), 3))

	return answer
}

// devicesView lists the keys that sign in as the user, and revokes one with D.
type devicesView struct {
	*pager[*api.Device]

	// mine is the fingerprint of the key dn signs with.
	mine string
}

func newDevices(app *App) *devicesView {
	d := &devicesView{}
	if app.client.Signer != nil {
		d.mine = ssh.FingerprintSHA256(app.client.Signer.PublicKey())
	}
	d.pager = newPager(app, "Devices", emptyState("no devices"), d.render)
	d.end = ""
	d.load = func(ctx context.Context, _ string) ([]*api.Device, string, error) {
		devices, err := app.client.Devices(ctx)
		return pointers(devices), "", err
	}

	return d
}

func (d *devicesView) render(dev *api.Device, selected bool, width int) []string {
	bar := " "
	if selected {
		bar = fmt.Sprintf("[%s]▌[-]", colorPrimary)
	}

	name := dev.Name
	if dev.AppName != "" {
		name += " · " + dev.AppName
	}
	head := fmt.Sprintf("[%s::b]%s[-::-]", colorInk, tview.Escape(name))
	if dev.Fingerprint == d.mine {
		head += " " + pill("this one", colorPrimary)
	}

	used := "never used"
	if dev.LastUsedAt != nil {
		used = "used " + ago(*dev.LastUsedAt)
	}
	facts := fmt.Sprintf("[%s]approved %s · %s[-]", colorMuted, dev.ApprovedAt.Local().Format("Jan 2, 2006"), used)

	lines := wrap(head, bar+" ", width)
	lines = append(lines, wrap(facts, bar+" ", width)...)
	lines = append(lines, wrap(fmt.Sprintf("[%s]%s[-]", colorFaint, dev.Fingerprint), bar+" ", width)...)

	return append(lines, "")
}

// ago says how long ago t was, as "3m ago", or "just now".
func ago(t time.Time) string {
	if rel := relativeTime(t); rel != "now" {
		return rel + " ago"
	}

	return "just now"
}

func (d *devicesView) Hints() []hint {
	hints := []hint{{"j/k", "move"}}
	if _, ok := d.Selected(); ok {
		hints = append(hints, hint{"D", "revoke"})
	}

	return hints
}

func (d *devicesView) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	list := d.listView.InputHandler()

	return func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		if dev, ok := d.Selected(); ok && event.Key() == tcell.KeyRune && event.Rune() == 'D' {
			d.revoke(dev)
			return
		}
		list(event, setFocus)
	}
}

func (d *devicesView) revoke(dev *api.Device) {
	question := fmt.Sprintf("Revoke %s? It can't sign in as you any more.", dev.Name)
	self := dev.Fingerprint == d.mine
	if self {
		question = "Revoke this device? dn signs out, and dn login signs it in again."
	}

	d.app.confirm(question, func() {
		d.app.act(func(ctx context.Context) error {
			return d.app.client.RevokeDevice(ctx, dev.ID)
		}, func(err error) {
			if err != nil {
				d.app.flashError(err)
				return
			}
			if self {
				d.app.fail("This device is revoked. Quit and run `dn login` to sign in again.")
			} else {
				d.app.succeed("Revoked %s.", tview.Escape(dev.Name))
			}
			d.Reload()
		})
	})
}
