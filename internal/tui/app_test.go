package tui

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"golang.org/x/crypto/ssh"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// fakeAPI answers the endpoints the TUI calls with a small log, and records the commits made.
type fakeAPI struct {
	mu      sync.Mutex
	starred bool
	bodies  []string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	me := map[string]any{"id": "u0", "username": "me", "name": "Me", "lane": 1}
	alice := map[string]any{"id": "u1", "username": "alice", "name": "Alice", "lane": "#3a88fe"}
	commit := func() map[string]any {
		stars := 2
		if f.starred {
			stars = 3
		}
		return map[string]any{
			"id": "c1", "short_hash": "abc1234", "body": "shipping a TUI", "status": "approved",
			"created_at": time.Now().Add(-time.Hour).Format(time.RFC3339), "author": alice,
			"stars_count": stars, "starred": f.starred, "replies_count": 1,
			"reply_preview": map[string]any{"data": []any{map[string]any{"id": "r1", "body": "nice one", "author": me}}},
		}
	}
	reply := map[string]any{"id": "r1", "short_hash": "def5678", "body": "nice one", "status": "approved", "author": me}

	var out any
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/me":
		out = map[string]any{"data": me, "unread_notifications_count": 2}
	case "GET /api/v1/log/following":
		out = map[string]any{"data": []any{commit()}, "meta": map[string]any{"next_cursor": nil}}
	case "GET /api/v1/commits/c1":
		out = map[string]any{"data": commit(), "ancestors": []any{}}
	case "GET /api/v1/commits/c1/replies":
		out = map[string]any{"data": []any{reply}, "meta": map[string]any{"next_cursor": nil}}
	case "POST /api/v1/commits/c1/star":
		f.starred = true
		out = map[string]any{"data": commit()}
	case "POST /api/v1/commits":
		var in api.NewCommit
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &in)
		f.bodies = append(f.bodies, in.Body)
		w.WriteHeader(http.StatusCreated)
		out = map[string]any{"data": map[string]any{"id": "c2", "short_hash": "9999999", "body": in.Body, "status": "pending", "author": me}}
	default:
		out = map[string]any{"data": []any{}, "meta": map[string]any{"next_cursor": nil}}
	}

	_ = json.NewEncoder(w).Encode(out)
}

type harness struct {
	t      *testing.T
	screen tcell.SimulationScreen
	app    *App
}

// start runs the TUI against handler, after configure (if any) has set the app up.
func start(t *testing.T, handler http.Handler, configure ...func(*App)) *harness {
	t.Helper()

	return startOn(t, handler, nil, configure...)
}

// startOn is start with the app drawing on what wrap makes of the simulation screen, as a graphicsScreen over it.
func startOn(t *testing.T, handler http.Handler, wrap func(tcell.SimulationScreen) tcell.Screen, configure ...func(*App)) *harness {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	_, key, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(80, 30)

	var drawn tcell.Screen = screen
	if wrap != nil {
		drawn = wrap(screen)
	}
	app := New(api.New(srv.URL, signer)).SetScreen(drawn)
	for _, f := range configure {
		f(app)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		app.tv.Stop()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})

	return &harness{t: t, screen: screen, app: app}
}

// text reads the screen on the UI goroutine, which is the one that draws on it.
func (h *harness) text() string {
	read := make(chan string, 1)
	h.app.tv.QueueUpdate(func() { read <- h.screenText() })

	select {
	case s := <-read:
		return s
	case <-time.After(5 * time.Second):
		h.t.Fatal("the UI goroutine is stuck")
		return ""
	}
}

func (h *harness) screenText() string {
	cells, width, height := h.screen.GetContents()

	var b strings.Builder
	for y := range height {
		for x := range width {
			if runes := cells[y*width+x].Runes; len(runes) > 0 {
				b.WriteString(string(runes))
			} else {
				b.WriteByte(' ')
			}
		}
		b.WriteByte('\n')
	}

	return b.String()
}

// waitFor waits until the screen shows want.
func (h *harness) waitFor(want string) {
	h.t.Helper()
	h.waitUntil(func(s string) bool { return strings.Contains(s, want) }, "show "+want)
}

// waitForGone waits until the screen no longer shows text.
func (h *harness) waitForGone(text string) {
	h.t.Helper()
	h.waitUntil(func(s string) bool { return !strings.Contains(s, text) }, "stop showing "+text)
}

func (h *harness) waitUntil(ok func(screen string) bool, what string) {
	h.t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok(h.text()) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	h.t.Fatalf("the screen didn't %s:\n%s", what, h.text())
}

func (h *harness) key(key tcell.Key) {
	h.screen.InjectKey(key, 0, tcell.ModNone)
}

func (h *harness) typeText(s string) {
	for _, r := range s {
		h.screen.InjectKey(tcell.KeyRune, r, tcell.ModNone)
	}
}

func TestReadsTheLogAndActsOnACommit(t *testing.T) {
	fake := &fakeAPI{}
	h := start(t, fake)

	h.waitFor("Alice @alice · 1h")
	h.waitFor("abc1234")
	h.waitFor("Notifications 2")
	h.waitFor("├─● Me @me")

	h.key(tcell.KeyEnter)
	h.waitFor("Thread abc1234")
	h.waitFor("def5678")

	h.typeText("s")
	h.waitFor("★ 3")

	h.key(tcell.KeyEscape)
	h.waitForGone("Thread abc1234")
	h.waitFor("├─● Me @me")

	h.typeText("c")
	h.waitFor("New commit")
	h.typeText("see https://example.com/" + strings.Repeat("a", 100))
	// A link counts as 23 characters, as on the server.
	h.waitFor("27/4000")
	h.key(tcell.KeyCtrlU)
	h.typeText("hello from the terminal")
	h.waitFor("23/4000")
	h.key(tcell.KeyCtrlS)
	h.waitFor("[log 9999999] hello from the terminal · pending review")

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.bodies) != 1 || fake.bodies[0] != "hello from the terminal" {
		t.Errorf("commits made: %q", fake.bodies)
	}
}

func TestRefusesABlankCommit(t *testing.T) {
	fake := &fakeAPI{}
	h := start(t, fake)
	h.waitFor("abc1234")

	h.typeText("c")
	h.waitFor("New commit")
	h.typeText("   ")
	h.key(tcell.KeyCtrlS)
	h.waitFor("A commit is 1 to 4000 characters")

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.bodies) != 0 {
		t.Errorf("sent %q", fake.bodies)
	}
}

// growingLog is a Following log that commits can be added on top of, which it counts for ?since= as the API does.
type growingLog struct {
	mu     sync.Mutex
	bodies []string
}

func (g *growingLog) add(body string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.bodies = append([]string{body}, g.bodies...)
}

func (g *growingLog) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()

	var out map[string]any
	switch r.URL.Path {
	case "/api/v1/me":
		out = map[string]any{"data": map[string]any{"id": "u0", "username": "me", "name": "Me"}}
	case "/api/v1/log/following":
		commits := make([]any, len(g.bodies))
		newer := -1
		for i, body := range g.bodies {
			commits[i] = map[string]any{"id": body, "short_hash": body, "body": body, "status": "approved", "author": map[string]any{"id": "u1", "username": "alice", "name": "Alice"}}
			if body == r.URL.Query().Get("since") {
				newer = i
			}
		}
		out = map[string]any{"data": commits, "meta": map[string]any{"next_cursor": nil}}
		if newer >= 0 {
			out["new_commits_count"] = newer
		}
	default:
		out = map[string]any{"data": []any{}, "meta": map[string]any{"next_cursor": nil}}
	}

	_ = json.NewEncoder(w).Encode(out)
}

func TestTakesInNewCommitsAtTheTop(t *testing.T) {
	log := &growingLog{bodies: []string{"first-commit"}}
	h := start(t, log, func(a *App) { a.opts.PollInterval = 50 * time.Millisecond })
	h.waitFor("first-commit")

	log.add("second-commit")
	h.waitFor("second-commit")
	if s := h.text(); strings.Contains(s, "↑") {
		t.Errorf("the feed took the commit in, but still counts it:\n%s", s)
	}
}

func TestCountsNewCommitsWhenScrolledDown(t *testing.T) {
	log := &growingLog{bodies: []string{"newer-commit", "older-commit"}}
	h := start(t, log, func(a *App) { a.opts.PollInterval = 50 * time.Millisecond })
	h.waitFor("older-commit")

	h.typeText("j")
	h.waitUntil(func(s string) bool { return strings.Contains(s, "▌│ older-commit") }, "select the older commit")
	log.add("brand-new-1")
	log.add("brand-new-2")
	h.waitFor("↑2")
	if s := h.text(); strings.Contains(s, "brand-new") {
		t.Errorf("the feed moved under the reader:\n%s", s)
	}

	h.typeText("R")
	h.waitFor("brand-new-2")
	h.waitForGone("↑2")
}
