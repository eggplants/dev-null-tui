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

	app := New(api.New(srv.URL, signer)).SetScreen(screen)
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

	h.waitFor("Alice @alice · 1h · abc1234")
	h.waitFor("Notifications (2)")
	h.waitFor("↳ Me @me")

	h.key(tcell.KeyEnter)
	h.waitFor("Thread abc1234")
	h.waitFor("def5678")

	h.typeText("f")
	h.waitFor("★ 3")

	h.key(tcell.KeyEscape)
	h.waitForGone("Thread abc1234")
	h.waitFor("↳ Me @me")

	h.typeText("c")
	h.waitFor("New commit")
	h.typeText("hello from the terminal")
	h.waitFor("23/4000")
	h.key(tcell.KeyCtrlS)
	h.waitFor("Committed 9999999. It's pending review")

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.bodies) != 1 || fake.bodies[0] != "hello from the terminal" {
		t.Errorf("commits made: %q", fake.bodies)
	}
}

func TestRefusesATooShortCommit(t *testing.T) {
	fake := &fakeAPI{}
	h := start(t, fake)
	h.waitFor("abc1234")

	h.typeText("c")
	h.waitFor("New commit")
	h.typeText("hi")
	h.key(tcell.KeyCtrlS)
	h.waitFor("A commit is 3 to 4000 characters.")

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.bodies) != 0 {
		t.Errorf("sent %q", fake.bodies)
	}
}
