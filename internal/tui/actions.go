package tui

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
	"github.com/eggplants/dev-null-tui/internal/browser"
)

// openURL opens a link from the API in the browser, if it's http(s), and on the instance's own host when sameHost is
// set, so a commit can't have the browser open something else.
func (a *App) openURL(link string, sameHost bool) {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		a.fail("Not opening %s: it isn't a web link.", tview.Escape(link))
		return
	}
	if base, err := url.Parse(a.client.BaseURL); sameHost && (err != nil || !strings.EqualFold(u.Host, base.Host)) {
		a.fail("Not opening %s: it isn't on %s.", tview.Escape(link), tview.Escape(base.Host))
		return
	}

	if err := browser.Open(u.String()); err != nil {
		a.flashError(err)
	}
}

// copyLink puts a commit's link on the clipboard.
func (a *App) copyLink(c *api.Commit) {
	if err := a.copyText(c.HTMLURL); err != nil {
		a.flashError(err)
		return
	}
	a.succeed("Copied link to %s.", c.ShortHash)
}

// copyText puts text on the clipboard with the first clipboard command there is, and else asks the terminal to with
// OSC 52, which also works over SSH.
func (a *App) copyText(text string) error {
	for _, cmd := range clipboardCommands() {
		if _, err := exec.LookPath(cmd[0]); err != nil {
			continue
		}
		c := exec.Command(cmd[0], cmd[1:]...)
		c.Stdin = strings.NewReader(text)
		if c.Run() == nil {
			return nil
		}
	}

	if a.screen == nil {
		return errors.New("there's no clipboard to copy to")
	}
	if tty, ok := a.screen.Tty(); ok {
		_, err := fmt.Fprintf(tty, "\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(text)))
		return err
	}

	return errors.New("there's no clipboard to copy to")
}

func clipboardCommands() [][]string {
	// Over SSH, the clipboard of the machine dn runs on isn't the one the user has; OSC 52 reaches theirs.
	if os.Getenv("SSH_TTY") != "" {
		return nil
	}

	switch runtime.GOOS {
	case "darwin":
		return [][]string{{"pbcopy"}}
	case "windows":
		return [][]string{{"clip"}}
	}

	return [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
}

// clipboardImage saves the image on the clipboard to a temporary PNG and returns its path.
func clipboardImage(ctx context.Context) (string, error) {
	f, err := os.CreateTemp("", "dn-clipboard-*.png")
	if err != nil {
		return "", err
	}
	path := f.Name()
	f.Close()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(`set f to open for access POSIX file %q with write permission
write (the clipboard as «class PNGf») to f
close access f`, path)
		cmd = exec.CommandContext(ctx, "osascript", "-e", script)
	default:
		var out []byte
		if _, err := exec.LookPath("wl-paste"); err == nil {
			out, err = exec.CommandContext(ctx, "wl-paste", "--type", "image/png").Output()
			if err != nil {
				return "", errors.New("the clipboard has no image")
			}
		} else if _, err := exec.LookPath("xclip"); err == nil {
			out, err = exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-t", "image/png", "-o").Output()
			if err != nil {
				return "", errors.New("the clipboard has no image")
			}
		} else {
			return "", errors.New("pasting an image needs wl-paste or xclip")
		}
		return path, os.WriteFile(path, out, 0o600)
	}

	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(path)
		if strings.Contains(string(out), "-1700") || strings.Contains(string(out), "class PNGf") {
			return "", errors.New("the clipboard has no image")
		}
		return "", fmt.Errorf("reading the clipboard: %s", strings.TrimSpace(string(out)))
	}

	return path, nil
}

// report asks why a commit should be reported and sends it to the moderators.
func (a *App) report(c *api.Commit) {
	if a.me != nil && c.Author != nil && c.Author.ID == a.me.ID {
		a.fail("That's your own commit.")
		return
	}

	labels := make([]string, len(api.ReportReasons))
	for i, r := range api.ReportReasons {
		labels[i] = r.Label
	}
	labels = append(labels, "Cancel")

	a.choose(fmt.Sprintf("Report %s by @%s? The moderators get it, not the author.", c.ShortHash, authorUsername(c)), labels, func(label string) {
		for _, r := range api.ReportReasons {
			if r.Label != label {
				continue
			}
			reason := r.ID
			a.act(func(ctx context.Context) error {
				return a.client.Report(ctx, c.ID, reason, "")
			}, func(err error) {
				if err != nil {
					a.flashError(err)
					return
				}
				a.succeed("Reported %s. Thanks for keeping /dev/null tidy.", c.ShortHash)
			})
		}
	})
}

var (
	uuidPattern    = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	hashPattern    = regexp.MustCompile(`^[0-9a-f]{7}$`)
	usernameInPath = regexp.MustCompile(`^/@([A-Za-z0-9-]+)/?$`)
	usernameAlone  = regexp.MustCompile(`^@?([A-Za-z0-9][A-Za-z0-9-]{0,38})$`)
)

// jump asks where to go: someone's @name, a profile's or a commit's link, or a commit's id or short hash.
func (a *App) jump() {
	input := tview.NewInputField().SetLabel("Go to ").SetPlaceholder("@username, a commit's link or hash").
		SetFieldBackgroundColor(tcell.GetColor(colorWash)).SetFieldTextColor(tcell.GetColor(colorInk)).
		SetLabelColor(tcell.GetColor(colorPrimary)).SetPlaceholderTextColor(tcell.GetColor(colorFaint))
	input.SetBorder(true).SetTitle(" / ").SetBackgroundColor(tcell.GetColor(theme.popover))
	input.SetDoneFunc(func(key tcell.Key) {
		a.closeModal("jump")
		if key == tcell.KeyEnter {
			a.goToRef(strings.TrimSpace(input.GetText()))
		}
	})

	_, _, width, _ := a.root.GetRect()
	a.openModal("jump", centered(input, min(70, width), 3))
}

// goToRef opens what ref names, telling the user when it names nothing it can open.
func (a *App) goToRef(ref string) {
	if ref == "" {
		return
	}

	if id := uuidPattern.FindString(ref); id != "" {
		a.push(newThread(a, strings.ToLower(id)))
		return
	}
	if u, err := url.Parse(ref); err == nil && u.Host != "" {
		if m := usernameInPath.FindStringSubmatch(u.Path); m != nil {
			a.push(newProfile(a, m[1]))
			return
		}
	}
	if hashPattern.MatchString(ref) {
		if c := a.knownCommit(ref); c != nil {
			a.push(newThread(a, c.ID))
			return
		}
		// A hash could be a username too; only commits already read can be found by it.
		if !strings.HasPrefix(ref, "@") {
			a.fail("No commit %s on screen; paste its link to open it.", ref)
			return
		}
	}
	if m := usernameAlone.FindStringSubmatch(ref); m != nil {
		a.push(newProfile(a, m[1]))
		return
	}

	a.fail("%s isn't an @name, a link or a hash.", tview.Escape(ref))
}

// setTheme switches the palette while the app runs, laying every view out again in it.
func (a *App) setTheme(p palette) {
	applyTheme(p)
	a.paint()
	a.views(func(v view) {
		if i, ok := v.(interface{ Invalidate() }); ok {
			i.Invalidate()
		}
	})
	a.drawHeader()
}

// recolorMe takes the user's new color into their commits on screen.
func (a *App) recolorMe() {
	if a.me == nil {
		return
	}

	a.views(func(v view) {
		if h, ok := v.(commitHolder); ok {
			h.eachCommit(func(c *api.Commit) {
				if c.Author != nil && c.Author.ID == a.me.ID {
					c.Author.Lane = a.me.Lane
				}
			})
		}
		if i, ok := v.(interface{ Invalidate() }); ok {
			i.Invalidate()
		}
	})
}

// saveTheme keeps the theme for the next run, if the app was given a way to.
func (a *App) saveTheme(name string) error {
	if a.opts.SaveTheme == nil {
		return nil
	}

	return a.opts.SaveTheme(name)
}
