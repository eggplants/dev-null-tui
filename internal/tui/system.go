package tui

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/eggplants/dev-null-tui/internal/browser"
)

// System is what the TUI does outside its screen and the API: the browser, the clipboard, the editor and the draft
// kept between runs. The app is given one, so tests can see what it does without doing it.
type System interface {
	// Open opens a link in the browser.
	Open(url string) error
	// Copy puts text on the clipboard.
	Copy(text string) error
	// ClipboardImage saves the image on the clipboard to a file and returns its path.
	ClipboardImage(ctx context.Context) (string, error)
	// Edit has the user edit text in their editor, which takes the terminal while the TUI is suspended, and
	// returns what they saved.
	Edit(text string) (string, error)
	// Draft is the new commit kept from the last time the composer closed, and SaveDraft keeps one, or forgets it
	// when text is blank.
	Draft() string
	SaveDraft(text string)
}

// osSystem is the System of the machine dn runs on.
type osSystem struct {
	// draftPath is where the draft is kept; without one, nothing is kept.
	draftPath string
	// tty is the terminal, for asking it to copy with OSC 52 when there's no clipboard command.
	tty func() (io.Writer, bool)
}

func (s *osSystem) Open(url string) error { return browser.Open(url) }

// Copy uses the first clipboard command there is, and else asks the terminal to with OSC 52, which also works over
// SSH.
func (s *osSystem) Copy(text string) error {
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

	if s.tty != nil {
		if tty, ok := s.tty(); ok {
			_, err := fmt.Fprintf(tty, "\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(text)))
			return err
		}
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

// ClipboardImage saves the image on the clipboard to a temporary PNG.
func (s *osSystem) ClipboardImage(ctx context.Context) (string, error) {
	f, err := os.CreateTemp("", "dn-clipboard-*.png")
	if err != nil {
		return "", err
	}
	path := f.Name()
	f.Close()

	var out []byte
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(`set f to open for access POSIX file %q with write permission
write (the clipboard as «class PNGf») to f
close access f`, path)
		if out, err = exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput(); err != nil {
			os.Remove(path)
			if strings.Contains(string(out), "-1700") || strings.Contains(string(out), "class PNGf") {
				return "", errors.New("the clipboard has no image")
			}
			return "", fmt.Errorf("reading the clipboard: %s", strings.TrimSpace(string(out)))
		}
		return path, nil
	case "windows":
		script := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; $i = [Windows.Forms.Clipboard]::GetImage(); if ($i -eq $null) { exit 1 }; $i.Save('%s')`, strings.ReplaceAll(path, "'", "''"))
		if err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-STA", "-Command", script).Run(); err != nil {
			os.Remove(path)
			return "", errors.New("the clipboard has no image")
		}
		return path, nil
	}

	switch {
	case lookPath("wl-paste"):
		out, err = exec.CommandContext(ctx, "wl-paste", "--type", "image/png").Output()
	case lookPath("xclip"):
		out, err = exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-t", "image/png", "-o").Output()
	default:
		os.Remove(path)
		return "", errors.New("pasting an image needs wl-paste or xclip")
	}
	if err != nil || len(out) == 0 {
		os.Remove(path)
		return "", errors.New("the clipboard has no image")
	}

	return path, os.WriteFile(path, out, 0o600)
}

func lookPath(name string) bool {
	_, err := exec.LookPath(name)

	return err == nil
}

// Edit runs $VISUAL, or $EDITOR, or else vi (Notepad on Windows) on a temporary file holding text.
func (s *osSystem) Edit(text string) (string, error) {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}

	f, err := os.CreateTemp("", "dn-commit-*.md")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(text)
	f.Close()
	if err != nil {
		return "", err
	}

	var cmd *exec.Cmd
	switch {
	case runtime.GOOS == "windows" && editor == "":
		cmd = exec.Command("notepad", f.Name())
	case runtime.GOOS == "windows":
		cmd = exec.Command(editor, f.Name())
	default:
		if editor == "" {
			editor = "vi"
		}
		// The editor may be a command with arguments, like "code --wait".
		cmd = exec.Command("sh", "-c", editor+` "$1"`, "sh", f.Name())
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s exited: %w", cmd.Path, err)
	}

	data, err := os.ReadFile(f.Name())
	if err != nil {
		return "", err
	}

	return strings.TrimRight(string(data), "\n"), nil
}

func (s *osSystem) Draft() string {
	if s.draftPath == "" {
		return ""
	}
	data, err := os.ReadFile(s.draftPath)
	if err != nil {
		return ""
	}

	return string(data)
}

func (s *osSystem) SaveDraft(text string) {
	if s.draftPath == "" {
		return
	}
	if strings.TrimSpace(text) == "" {
		_ = os.Remove(s.draftPath)
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.draftPath), 0o700); err == nil {
		_ = os.WriteFile(s.draftPath, []byte(text), 0o600)
	}
}
