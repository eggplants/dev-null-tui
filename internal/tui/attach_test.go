package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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

func TestAttachesImagesToAReply(t *testing.T) {
	image := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(image, make([]byte, 2048), 0o600); err != nil {
		t.Fatal(err)
	}

	f := &fakeAPI{}
	h := start(t, f)
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
	h.waitFor("[log 9999999] with a screenshot")

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.drafts) != 1 || f.drafts[0].ParentID != "c1" || f.drafts[0].Body != "with a screenshot" {
		t.Fatalf("sent %+v", f.drafts)
	}
	if len(f.images) != 1 || !slices.Equal(f.images[0], []string{"shot.png 2048"}) {
		t.Errorf("sent images %v", f.images)
	}
}

func TestAttachesImagesToAQuote(t *testing.T) {
	image := filepath.Join(t.TempDir(), "shot.webp")
	if err := os.WriteFile(image, []byte("webp"), 0o600); err != nil {
		t.Fatal(err)
	}

	f := &fakeAPI{}
	h := start(t, f)
	h.waitFor("abc1234")

	h.typeText("q")
	h.waitFor("Quote")
	h.key(tcell.KeyCtrlO)
	h.waitFor("Attach an image")
	h.typeText(image)
	h.key(tcell.KeyEnter)
	h.waitFor("shot.webp (4 B)")

	h.typeText("look at this")
	h.key(tcell.KeyCtrlS)
	h.waitFor("[log 9999999] look at this")

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.drafts) != 1 || f.drafts[0].QuotedCommitID != "c1" || len(f.images) != 1 || len(f.images[0]) != 1 {
		t.Fatalf("sent %+v with images %v", f.drafts, f.images)
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
