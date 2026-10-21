package tui

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// richAPI answers what the views beyond the logs ask for: profiles with their activity, people, notifications by
// type, and the settings.
type richAPI struct {
	mu       sync.Mutex
	types    []string
	muted    bool
	reported []string
}

func (f *richAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	yes := true
	me := map[string]any{"id": "u0", "username": "me", "name": "Me", "lane": 1}
	alice := map[string]any{"id": "u1", "username": "alice", "name": "Alice", "lane": 2, "is_followed": &yes, "bio": "builds things"}
	commit := map[string]any{
		"id": "11111111-2222-3333-4444-555555555555", "short_hash": "abc1234", "body": "hello @alice", "status": "approved",
		"created_at": time.Now().Add(-time.Hour).Format(time.RFC3339), "author": alice, "stars_count": 1,
		"html_url": "http://" + r.Host + "/@alice/commits/11111111-2222-3333-4444-555555555555",
		"mentions": []any{map[string]any{"username": "alice", "offset": 6, "length": 6}},
	}
	page := func(data ...any) map[string]any {
		return map[string]any{"data": data, "meta": map[string]any{"next_cursor": nil}}
	}

	var out any
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/me":
		out = map[string]any{"data": me}
	case "GET /api/v1/log/following", "GET /api/v1/log/latest":
		out = page(commit)
	case "GET /api/v1/commits/11111111-2222-3333-4444-555555555555":
		out = map[string]any{"data": commit, "ancestors": []any{}}
	case "GET /api/v1/users/alice":
		out = map[string]any{
			"data": alice, "is_following": true, "follows_you": true, "is_muting": f.muted,
			"streaks":      map[string]any{"current": 2, "longest": 9},
			"achievements": map[string]any{"level": 3, "rank": "operator", "xp": 420, "level_progress": 40, "badges": []any{map[string]any{"name": "init", "unlocked": true, "progress": 1, "target": 1}}},
			"activity":     map[string]any{"scale": "week", "buckets": []any{map[string]any{"date": "2026-10-20", "count": 2}}},
		}
	case "GET /api/v1/users/alice/commits":
		out = page(commit)
	case "GET /api/v1/users/alice/followers":
		out = page(me)
	case "POST /api/v1/users/alice/mute":
		f.muted = true
		w.WriteHeader(http.StatusNoContent)
		return
	case "GET /api/v1/notifications":
		f.types = append(f.types, r.URL.Query().Get("type"))
		out = page(map[string]any{"id": "n1", "type": "starred", "actor": alice, "actors_count": 3, "created_at": time.Now().Format(time.RFC3339), "commit": commit})
	case "POST /api/v1/commits":
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		w.WriteHeader(http.StatusCreated)
		out = map[string]any{"data": map[string]any{"id": "c9", "short_hash": "9999999", "body": in["body"], "status": "approved", "author": me}}
	case "GET /api/v1/notifications/n1/group":
		out = page(map[string]any{"id": "g1", "type": "starred", "actor": me, "actors_count": 1, "created_at": time.Now().Format(time.RFC3339)})
	case "POST /api/v1/commits/11111111-2222-3333-4444-555555555555/reports":
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.reported = append(f.reported, in["reason"])
		w.WriteHeader(http.StatusCreated)
		out = map[string]any{}
	default:
		out = page()
	}

	_ = json.NewEncoder(w).Encode(out)
}

func TestGoesPlacesWithG(t *testing.T) {
	h := start(t, &richAPI{})
	h.waitFor("abc1234")

	h.typeText("g")
	h.waitFor("g top")
	h.typeText("n")
	h.waitFor("Alice and 2 others starred your commit")

	h.typeText("gs")
	h.waitFor("Pastéis mode")
	h.waitFor("Notifications › Settings")
}

func TestJumpsToAProfile(t *testing.T) {
	h := start(t, &richAPI{})
	h.waitFor("abc1234")

	h.typeText("/")
	h.waitFor("Go to")
	h.typeText("@alice")
	h.key(tcell.KeyEnter)
	h.waitFor("builds things")
	h.waitFor("streak 2d")
	h.waitFor("Lv. 3 · operator")
	h.waitFor("follows you")

	// The profile's tabs go round with T.
	h.typeText("TT")
	h.waitFor("Me @me")

	h.typeText("M")
	h.waitFor("Muted @alice")
	h.waitFor("muted")
}

func TestFiltersNotifications(t *testing.T) {
	api := &richAPI{}
	h := start(t, api)
	h.waitFor("abc1234")

	h.typeText("4")
	h.waitFor("starred your commit")
	h.typeText("f")
	h.waitFor("Notifications · Replies")

	h.typeText("e")
	h.waitFor("Me starred your commit")

	api.mu.Lock()
	defer api.mu.Unlock()
	if strings.Join(api.types, ",") != "all,replies" {
		t.Errorf("asked for %q", api.types)
	}
}

func TestReportsACommit(t *testing.T) {
	api := &richAPI{}
	h := start(t, api)
	h.waitFor("abc1234")

	h.typeText("!")
	h.waitFor("Report abc1234 by @alice?")
	h.key(tcell.KeyEnter)
	h.waitFor("Reported abc1234")

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.reported) != 1 || api.reported[0] != "spam" {
		t.Errorf("reported %q", api.reported)
	}
}

func TestFitsEveryScreenSize(t *testing.T) {
	for _, size := range [][2]int{{40, 12}, {60, 20}, {80, 24}, {120, 40}, {200, 60}} {
		h := start(t, &richAPI{}, func(a *App) {
			a.screen.(tcell.SimulationScreen).SetSize(size[0], size[1])
		})
		h.waitFor("abc1234")
		h.waitFor("help")

		for _, line := range strings.Split(strings.TrimRight(h.text(), "\n"), "\n") {
			if n := len([]rune(line)); n > size[0] {
				t.Errorf("at %dx%d a line is %d wide: %q", size[0], size[1], n, line)
			}
		}
	}

	h := start(t, &richAPI{}, func(a *App) { a.screen.(tcell.SimulationScreen).SetSize(30, 8) })
	h.waitFor("Make the terminal a")
}

// repliesAPI is a log with one commit that has replies, for o.
type repliesAPI struct{ richAPI }

func (f *repliesAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	alice := map[string]any{"id": "u1", "username": "alice", "name": "Alice"}
	switch r.URL.Path {
	case "/api/v1/log/following":
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{
			"id": "c1", "short_hash": "abc1234", "body": "root", "status": "approved", "author": alice, "replies_count": 2,
		}}, "meta": map[string]any{"next_cursor": nil}})
	case "/api/v1/commits/c1/replies":
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
			map[string]any{"id": "r1", "short_hash": "1111111", "body": "first reply", "author": alice},
			map[string]any{"id": "r2", "short_hash": "2222222", "body": "second reply", "author": alice},
		}, "meta": map[string]any{"next_cursor": nil}})
	default:
		f.richAPI.ServeHTTP(w, r)
	}
}

func TestShowsAndHidesRepliesWithO(t *testing.T) {
	h := start(t, &repliesAPI{})
	h.waitFor("abc1234")
	h.waitFor("o replies")

	h.typeText("o")
	h.waitFor("first reply")
	h.waitFor("second reply")
	h.waitFor("o hide replies")

	h.typeText("o")
	h.waitForGone("second reply")
}

func TestQQuotesRatherThanQuits(t *testing.T) {
	h := start(t, &richAPI{})
	h.waitFor("abc1234")

	// q quotes the commit, as on the web, rather than quitting.
	h.typeText("q")
	h.waitFor("Quote")
	h.key(tcell.KeyEscape)
	h.waitForGone("Quote")
}
