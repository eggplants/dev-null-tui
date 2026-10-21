package tui

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func TestAvatarURLAsksGitHubForASmallOne(t *testing.T) {
	if got := avatarURL("https://avatars.githubusercontent.com/u/1?v=4"); got != "https://avatars.githubusercontent.com/u/1?s=96&v=4" {
		t.Errorf("avatarURL = %q", got)
	}
	if got := avatarURL("https://example.com/me.png"); got != "https://example.com/me.png" {
		t.Errorf("another host's avatar changed: %q", got)
	}
}

func TestRoundAvatar(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 60, 40))
	for y := range 40 {
		for x := range 60 {
			src.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 255})
		}
	}

	out := roundAvatar(src, 40, 40, "#00ff00", false)
	if c := out.NRGBAAt(20, 20); c != (color.NRGBA{255, 0, 0, 255}) {
		t.Errorf("the middle is %v", c)
	}
	if c := out.NRGBAAt(0, 0); c.A != 0 {
		t.Errorf("the corner is %v, not clear", c)
	}
	if c := out.NRGBAAt(20, 1); c.G < 200 || c.R > 60 {
		t.Errorf("the ring is %v", c)
	}

	opaque := roundAvatar(src, 40, 40, "#00ff00", true)
	if c := opaque.NRGBAAt(0, 0); c != hexColorRGBA(theme.shell) {
		t.Errorf("an opaque corner is %v, not the background", c)
	}
}

// avatarAPI is a log of one commit by someone with an avatar, and the avatar.
func avatarAPI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/avatar.png" {
		_ = png.Encode(w, image.NewGray(image.Rect(0, 0, 8, 8)))
		return
	}

	alice := map[string]any{"id": "u1", "username": "alice", "name": "Alice Liddell", "lane": 3, "avatar_url": "http://" + r.Host + "/avatar.png"}
	var out any = map[string]any{"data": []any{}, "meta": map[string]any{"next_cursor": nil}}
	switch r.URL.Path {
	case "/api/v1/me":
		out = map[string]any{"data": map[string]any{"id": "u0", "username": "me", "name": "Me"}}
	case "/api/v1/log/following":
		out = map[string]any{"data": []any{map[string]any{
			"id": "c1", "short_hash": "abc1234", "body": "hello", "status": "approved", "author": alice,
			"created_at": time.Now().Format(time.RFC3339),
		}}, "meta": map[string]any{"next_cursor": nil}}
	}
	_ = json.NewEncoder(w).Encode(out)
}

func TestShowsInitialsWithoutPixels(t *testing.T) {
	h := start(t, http.HandlerFunc(avatarAPI))
	h.waitFor("● AL Alice Liddell @alice")
}

func TestDrawsAvatarsInPixels(t *testing.T) {
	tty := &fakeTty{}
	h := startOn(t, http.HandlerFunc(avatarAPI), func(sim tcell.SimulationScreen) tcell.Screen {
		return newGraphicsScreen(ttyScreen{sim, tty}, graphicsKitty, 0, 0)
	})
	h.waitFor("Alice Liddell @alice")

	// Initials show until the picture is in, and then it's sent to the terminal.
	h.waitUntil(func(string) bool {
		tty.mu.Lock()
		defer tty.mu.Unlock()
		return strings.Contains(tty.out.String(), "\x1b_Ga=T,f=100")
	}, "send the avatar to the terminal")
}
