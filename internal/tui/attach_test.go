package tui

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"

	"github.com/eggplants/dev-null-tui/internal/commit"
)

func TestCompletePath(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"shot.png", "shot.webp", "notes.txt", ".hidden.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "shots"), 0o700); err != nil {
		t.Fatal(err)
	}

	got := completePath(dir + "/sh")
	want := []string{dir + "/shot.png", dir + "/shot.webp", dir + "/shots/"}
	if !slices.Equal(got, want) {
		t.Errorf("completePath = %q, want %q", got, want)
	}

	if got := completePath(dir + "/.h"); !slices.Equal(got, []string{dir + "/.hidden.png"}) {
		t.Errorf("hidden files = %q", got)
	}
	if got := completePath(dir + "/"); slices.Contains(got, dir+"/.hidden.png") {
		t.Errorf("hidden files listed without a dot: %q", got)
	}
}

func TestImagePathTakesPastedFiles(t *testing.T) {
	want := "/home/me/Pictures/Screenshots/Screenshot From 2026-10-20 04-04-48.png"

	pastes := []string{
		want,
		"  " + want + " ",
		"'" + want + "' ",
		`"` + want + `"`,
		"file:///home/me/Pictures/Screenshots/Screenshot%20From%202026-10-20%2004-04-48.png",
	}
	// A backslash is a path separator on Windows, not an escape.
	if runtime.GOOS != "windows" {
		pastes = append(pastes, `/home/me/Pictures/Screenshots/Screenshot\ From\ 2026-10-20\ 04-04-48.png`)
	}

	for _, pasted := range pastes {
		if got := imagePath(pasted); got != want {
			t.Errorf("imagePath(%q) = %q", pasted, got)
		}
	}
}

func TestCheckImage(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.png")
	big := filepath.Join(dir, "big.png")
	if err := os.WriteFile(small, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(big, make([]byte, commit.MaxImageSize+1), 0o600); err != nil {
		t.Fatal(err)
	}

	if img, err := checkImage("'" + small + "' "); err != nil || img.path != small || img.size != 3 {
		t.Errorf("checkImage(small) = %+v, %v", img, err)
	}
	for _, path := range []string{big, dir, filepath.Join(dir, "missing.png"), ""} {
		if _, err := checkImage(path); err == nil {
			t.Errorf("checkImage(%q) took it", path)
		}
	}
}

// recorder stands in for sending a commit with images, so nothing leaves the test.
type recorder struct {
	mu   sync.Mutex
	sent []commit.Options
	pass []byte
}

func (r *recorder) commit(_ context.Context, opts commit.Options) (string, error) {
	// Ask for a passphrase the way an encrypted key does, to drive the dialog.
	pass, err := opts.Passphrase("/home/me/.ssh/id_ed25519")
	if err != nil {
		return "", err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, opts)
	r.pass = pass

	return "Committed: https://dev-null.sh/@me/commits/c9 (pending review, only you can see it until it is approved)\n", nil
}

func TestAttachesImagesToAReply(t *testing.T) {
	image := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(image, make([]byte, 2048), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := &recorder{}
	h := start(t, &fakeAPI{}, func(a *App) { a.commitWithImages = rec.commit })
	h.waitFor("abc1234")

	h.typeText("r")
	h.waitFor("Reply")
	h.key(tcell.KeyCtrlO)
	h.waitFor("Attach an image")
	h.typeText(image)
	h.key(tcell.KeyEnter)
	h.waitFor("shot.png (2 KB)")

	h.typeText("with a screenshot")
	h.key(tcell.KeyCtrlS)
	h.waitFor("Unlock your SSH key")
	h.typeText("secret")
	h.key(tcell.KeyEnter)
	h.waitFor("Committed: https://dev-null.sh/@me/commits/c9 (pending review")

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.sent) != 1 {
		t.Fatalf("sent %d commits", len(rec.sent))
	}
	got := rec.sent[0]
	if got.ReplyTo != "c1" || got.Message != "with a screenshot" || !slices.Equal(got.Images, []string{image}) {
		t.Errorf("sent %+v", got)
	}
	if string(rec.pass) != "secret" {
		t.Errorf("passphrase = %q", rec.pass)
	}
}

func TestRemovesTheLastImage(t *testing.T) {
	image := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(image, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := start(t, &fakeAPI{})
	h.waitFor("abc1234")

	h.typeText("c")
	h.waitFor("New commit")
	h.key(tcell.KeyCtrlO)
	h.waitFor("Attach an image")
	h.typeText(image)
	h.key(tcell.KeyEnter)
	h.waitFor("shot.png (3 B)")

	h.key(tcell.KeyCtrlR)
	h.waitForGone("shot.png (3 B)")
}

func TestShowsEveryImagePastALongName(t *testing.T) {
	dir := t.TempDir()
	long := filepath.Join(dir, strings.Repeat("a-very-long-screenshot-name-", 5)+"end.png")
	images := []string{long, filepath.Join(dir, "second.png"), filepath.Join(dir, "third.png")}
	for _, image := range images {
		if err := os.WriteFile(image, []byte("png"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	h := start(t, &fakeAPI{})
	h.waitFor("abc1234")

	h.typeText("c")
	h.waitFor("New commit")
	for _, image := range images {
		h.key(tcell.KeyCtrlO)
		h.waitFor("Attach an image")
		h.typeText(image)
		h.key(tcell.KeyEnter)
		h.waitForGone("Attach an image")
	}
	h.waitFor("end.png (3 B)")
	h.waitFor("second.png (3 B)")
	h.waitFor("third.png (3 B)  ^r remove")
}

func TestShortenName(t *testing.T) {
	if got := shortenName("shot.png", 8); got != "shot.png" {
		t.Errorf("a name that fits = %q", got)
	}
	if got := shortenName("a-long-screenshot.png", 12); got != "a-long…t.png" {
		t.Errorf("shortenName = %q", got)
	}
	if got := shortenName("スクリーンショット.png", 12); uniseg.StringWidth(got) > 12 || !strings.HasSuffix(got, ".png") {
		t.Errorf("wide name = %q", got)
	}
}

func TestAQuoteCannotCarryImages(t *testing.T) {
	h := start(t, &fakeAPI{})
	h.waitFor("abc1234")

	h.typeText("q")
	h.waitFor("Quote")
	h.key(tcell.KeyCtrlO)
	h.waitFor("A quote can't carry images.")
}
