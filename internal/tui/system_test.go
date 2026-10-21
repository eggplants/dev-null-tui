package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// fakeSystem records what the app asks of the machine instead of doing it.
type fakeSystem struct {
	mu     sync.Mutex
	opened []string
	copied []string
	draft  string
	edited string
	image  string
}

func (f *fakeSystem) Open(url string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, url)
	return nil
}

func (f *fakeSystem) Copy(text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.copied = append(f.copied, text)
	return nil
}

func (f *fakeSystem) ClipboardImage(context.Context) (string, error) { return f.image, nil }
func (f *fakeSystem) Edit(string) (string, error)                    { return f.edited, nil }

func (f *fakeSystem) Draft() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.draft
}

func (f *fakeSystem) SaveDraft(text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.draft = text
}

func (f *fakeSystem) snapshot() fakeSystem {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fakeSystem{opened: append([]string{}, f.opened...), copied: append([]string{}, f.copied...), draft: f.draft}
}

func withSystem(sys System) func(*App) {
	return func(a *App) { a.sys = sys }
}

func TestCopiesAndOpensACommitsLink(t *testing.T) {
	sys := &fakeSystem{}
	h := start(t, &richAPI{}, withSystem(sys))
	h.waitFor("abc1234")

	h.typeText("y")
	h.waitFor("Copied link to abc1234.")
	h.typeText("w")
	h.waitUntil(func(string) bool { return len(sys.snapshot().opened) == 1 }, "open the link")

	got := sys.snapshot()
	if len(got.copied) != 1 || !strings.HasSuffix(got.copied[0], "/@alice/commits/11111111-2222-3333-4444-555555555555") || got.opened[0] != got.copied[0] {
		t.Errorf("copied %q, opened %q", got.copied, got.opened)
	}

	// A link to another host isn't opened, whatever the API says.
	h.app.tv.QueueUpdateDraw(func() { h.app.openURL("https://example.com/phish", true) })
	h.waitFor("Not opening https://example.com/phish")
	h.app.tv.QueueUpdateDraw(func() { h.app.openURL("javascript:alert(1)", false) })
	h.waitFor("it isn't a web link")
	if n := len(sys.snapshot().opened); n != 1 {
		t.Errorf("opened %d links", n)
	}
}

func TestKeepsTheDraft(t *testing.T) {
	sys := &fakeSystem{}
	h := start(t, &richAPI{}, withSystem(sys))
	h.waitFor("abc1234")

	h.typeText("c")
	h.waitFor("New commit")
	h.typeText("half a thought")
	h.key(tcell.KeyEscape)
	h.waitFor("Draft kept in stash@{0}")
	if got := sys.snapshot().draft; got != "half a thought" {
		t.Fatalf("draft = %q", got)
	}

	h.typeText("c")
	h.waitFor("half a thought")
	h.typeText(" finished")
	h.key(tcell.KeyCtrlS)
	h.waitFor("[log 9999999] half a thought finished")
	if got := sys.snapshot().draft; got != "" {
		t.Errorf("the draft outlived its commit: %q", got)
	}
}

func TestEditsInTheEditor(t *testing.T) {
	sys := &fakeSystem{edited: "written in vim"}
	h := start(t, &richAPI{}, withSystem(sys))
	h.waitFor("abc1234")

	h.typeText("c")
	h.waitFor("New commit")
	h.key(tcell.KeyCtrlE)
	h.waitFor("written in vim")
	h.waitFor("14/4000")
}

func TestAttachesTheClipboardsImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clipboard.png")
	if err := os.WriteFile(path, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := start(t, &richAPI{}, withSystem(&fakeSystem{image: path}))
	h.waitFor("abc1234")

	h.typeText("c")
	h.waitFor("New commit")
	h.key(tcell.KeyCtrlV)
	h.waitFor("▣ clipboard.png")
	h.waitFor("Attached the image on the clipboard.")
}
