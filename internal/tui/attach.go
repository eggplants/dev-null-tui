package tui

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/commit"
)

// attachment is an image picked for a commit.
type attachment struct {
	path string
	size int64
}

// expandHome turns a leading ~/ into the home directory.
func expandHome(path string) string {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}

	return path
}

// completePath lists the files and directories that complete the path typed so far, directories ending in "/".
// Hidden ones only show up once a "." is typed.
func completePath(current string) []string {
	dir, prefix := filepath.Split(current)
	entries, err := os.ReadDir(expandHome(filepath.Clean(dir + ".")))
	if err != nil {
		return nil
	}

	var matches []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		if e.IsDir() {
			name += "/"
		}
		matches = append(matches, dir+name)
	}
	sort.Strings(matches)

	return matches[:min(len(matches), 30)]
}

// imagePath turns what was typed or pasted into a path. Pasting a file from a file manager can give it quoted
// ('/a/b c.png'), with its spaces escaped (/a/b\ c.png), or as a file:// URI.
func imagePath(text string) string {
	text = strings.TrimSpace(text)

	switch {
	case strings.HasPrefix(text, "file://"):
		if u, err := url.Parse(text); err == nil {
			return u.Path
		}
	case len(text) >= 2 && (text[0] == '\'' || text[0] == '"') && text[len(text)-1] == text[0]:
		text = text[1 : len(text)-1]
	case runtime.GOOS != "windows" && strings.Contains(text, `\`):
		text = backslashEscape.ReplaceAllString(text, "$1")
	}

	return expandHome(text)
}

var backslashEscape = regexp.MustCompile(`\\(.)`)

// checkImage checks what the server would refuse before anything is sent: a missing file or one over 10 MB.
func checkImage(path string) (attachment, error) {
	path = imagePath(path)
	if path == "" {
		return attachment{}, errors.New("type the path of an image")
	}

	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return attachment{}, fmt.Errorf("could not read the image %s", path)
	}
	if info.Size() > commit.MaxImageSize {
		return attachment{}, fmt.Errorf("the image %s is larger than 10 MB", path)
	}

	return attachment{path: path, size: info.Size()}, nil
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// pickImage asks for an image's path, completing it with Tab, and hands what it picked to add.
func (a *App) pickImage(add func(attachment)) {
	field := tview.NewInputField().SetLabel("Path ").SetPlaceholder("~/Pictures/shipped.png")
	hint := tview.NewTextView().SetDynamicColors(true).
		SetText(fmt.Sprintf("[%s]JPEG, PNG or WebP, up to 10 MB  ⇥ complete  ⏎ attach  esc cancel[-]", colorMuted))

	attach := func(path string) {
		img, err := checkImage(path)
		if err != nil {
			hint.SetText(fmt.Sprintf("[red]%s[-]", tview.Escape(err.Error())))
			return
		}
		a.closeModal("attach")
		add(img)
	}

	field.SetAutocompleteFunc(completePath)
	field.SetAutocompletedFunc(func(text string, _ int, source int) bool {
		if source == tview.AutocompletedNavigate {
			return false
		}
		field.SetText(text)
		// A directory opens up for the next part of the path.
		if strings.HasSuffix(text, "/") {
			return false
		}
		// Enter on a file attaches it, as Enter does once the list is gone.
		if source == tview.AutocompletedEnter {
			attach(text)
		}
		return true
	})
	field.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEscape:
			a.closeModal("attach")
		case tcell.KeyEnter:
			attach(field.GetText())
		}
	})

	// Two lines, so an error about a long path isn't cut off.
	form := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(field, 1, 0, true).AddItem(hint, 2, 0, false)
	form.SetBorder(true).SetTitle(" Attach an image ")

	a.openModal("attach", centered(form, 70, 5))
}

// askPassphrase asks for a private key's passphrase in a dialog. It is called off the UI goroutine, from the
// commit being sent, and waits for the answer.
func (a *App) askPassphrase(path string) ([]byte, error) {
	answer := make(chan []byte, 1)

	a.tv.QueueUpdateDraw(func() {
		field := tview.NewInputField().SetLabel("Passphrase ").SetMaskCharacter('*')
		field.SetDoneFunc(func(key tcell.Key) {
			a.closeModal("passphrase")
			if key == tcell.KeyEnter {
				answer <- []byte(field.GetText())
			} else {
				answer <- nil
			}
		})

		form := tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(tview.NewTextView().SetText(tview.Escape(path)), 1, 0, false).
			AddItem(field, 1, 0, true)
		form.SetBorder(true).SetTitle(" Unlock your SSH key ")

		a.openModal("passphrase", centered(form, 70, 4))
	})

	if pass := <-answer; pass != nil {
		return pass, nil
	}

	return nil, errors.New("no passphrase given: add the key to your SSH agent with ssh-add")
}
