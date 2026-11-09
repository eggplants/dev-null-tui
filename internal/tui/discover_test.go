package tui

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
)

// discoverAPI suggests people to follow, round by round, and has one the user follows live.
type discoverAPI struct {
	mu        sync.Mutex
	rounds    []string
	dismissed []string
	followed  []string
}

func (f *discoverAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	two := 2
	var out any
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/me":
		out = map[string]any{"data": map[string]any{"id": "u0", "handle": "me", "name": "Me", "lane": 1}}
	case "GET /api/v1/me/suggested-users":
		round := r.URL.Query().Get("round")
		f.rounds = append(f.rounds, round)
		name := "ada"
		if round != "0" {
			name = "grace"
		}
		out = map[string]any{"data": []any{
			map[string]any{"id": "u1", "handle": name, "name": name, "followed_by_following_count": &two},
			map[string]any{"id": "u2", "handle": "linus", "name": "Linus"},
		}}
	case "GET /api/v1/me/live-following":
		out = map[string]any{"data": []any{map[string]any{"id": "u3", "handle": "streamer", "name": "Streamer", "live": map[string]any{"platform": "twitch"}}}}
	case "POST /api/v1/users/linus/dismiss":
		f.dismissed = append(f.dismissed, "linus")
		w.WriteHeader(http.StatusNoContent)
		return
	case "POST /api/v1/users/ada/follow":
		f.followed = append(f.followed, "ada")
		w.WriteHeader(http.StatusNoContent)
		return
	default:
		out = map[string]any{"data": []any{}, "meta": map[string]any{"next_cursor": nil}}
	}

	_ = json.NewEncoder(w).Encode(out)
}

func TestSuggestsWhoToFollow(t *testing.T) {
	f := &discoverAPI{}
	h := start(t, f)
	h.waitFor("Following")

	h.typeText("gw")
	h.waitFor("ada @ada")
	h.waitFor("followed by 2 people you follow")

	h.typeText("f")
	h.waitFor("Following @ada.")

	h.typeText("j")
	h.typeText("x")
	h.waitFor("You won't see @linus suggested again.")
	h.waitForGone("Linus @linus")

	h.typeText("R")
	h.waitFor("grace @grace")

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.followed) != 1 || len(f.dismissed) != 1 {
		t.Errorf("followed %v, dismissed %v", f.followed, f.dismissed)
	}
	// The aside asks for round 0, the list for round 0 and then round 1.
	if last := f.rounds[len(f.rounds)-1]; last != "1" {
		t.Errorf("asked for rounds %v", f.rounds)
	}
}

func TestShowsWhoIsLiveInTheAside(t *testing.T) {
	h := start(t, &discoverAPI{})
	h.screen.SetSize(160, 40)
	h.waitFor("Live now")
	h.waitFor("@streamer")
	h.waitFor("$ who --should-follow")
}
