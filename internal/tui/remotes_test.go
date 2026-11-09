package tui

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// remotesAPI has a remote with @ada, a request from @eve, and an org, and takes what is pushed to them.
type remotesAPI struct {
	mu       sync.Mutex
	pushed   []api.NewMessage
	read     []int64
	stashed  bool
	merged   bool
	reacted  []string
	messages []map[string]any
	roles    []string
	// pending keeps the first message's link card pending until it's read again.
	pending bool
	// purposes are what candidates were asked for, and shared the commits sent in Remotes.
	purposes []string
	shared   []string
}

func (f *remotesAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	me := map[string]any{"id": "u0", "handle": "me", "name": "Me", "lane": 1}
	ada := map[string]any{"id": "u1", "handle": "ada", "name": "Ada", "lane": 2}
	eve := map[string]any{"id": "u2", "handle": "eve", "name": "Eve"}
	at := time.Now().Add(-time.Hour).Format(time.RFC3339)
	if f.messages == nil {
		f.messages = []map[string]any{
			{"id": 1, "user_id": "u1", "body": "hey, how's the TUI? https://example.com", "created_at": at, "link_preview": map[string]any{"url": "https://example.com", "is_pending": f.pending}},
			{"id": 2, "user_id": "u0", "body": "almost there", "created_at": at, "reactions": []any{map[string]any{"user_id": "u1", "emoji": "🎉"}}},
		}
	}
	conversation := func(id string, partner any, last string, ahead int) map[string]any {
		return map[string]any{
			"id": id, "kind": "direct", "partner": partner, "ahead": ahead, "last_message_at": at, "is_archived": f.stashed && id == "d1",
			"last_message": map[string]any{"id": 9, "user_id": "u1", "body": last, "created_at": at},
		}
	}

	var out any
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/me":
		out = map[string]any{"data": map[string]any{"id": "u0", "handle": "me", "name": "Me", "lane": 1, "remotes": map[string]any{"from": "following", "unread_count": 1, "requests": true}}}
	case "GET /api/v1/remotes":
		var data []any
		switch r.URL.Query().Get("tab") {
		case "open":
			if !f.stashed {
				data = append(data, conversation("d1", ada, "hey, how's the TUI?", 1))
			}
			data = append(data, map[string]any{
				"id": "o1", "kind": "org", "name": "Gophers", "ahead": 0, "last_message_at": at,
				"members": []any{map[string]any{"user": ada, "role": "contributor"}},
			})
			if f.merged {
				data = append(data, conversation("d2", eve, "can we talk?", 0))
			}
		case "fetched":
			if !f.merged {
				data = append(data, conversation("d2", eve, "can we talk?", 1))
			}
		case "stashed":
			if f.stashed {
				data = append(data, conversation("d1", ada, "hey, how's the TUI?", 0))
			}
		}
		out = map[string]any{"data": data, "has_more": false, "unread_count": 1, "requests_count": 1}
	case "GET /api/v1/remotes/@ada/messages":
		var messages []any
		after := r.URL.Query().Get("after")
		for _, m := range f.messages {
			if after == "" || m["id"].(int) > 2 {
				messages = append(messages, m)
			}
		}
		out = map[string]any{"data": map[string]any{
			"partner": ada, "messages": messages, "has_older": false, "access": "open", "read_through_id": 1,
			"partner_read_through_id": 2, "reactions_version": 1,
		}}
	case "POST /api/v1/remotes/@ada/messages":
		var in api.NewMessage
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.pushed = append(f.pushed, in)
		m := map[string]any{"id": 2 + len(f.pushed), "user_id": "u0", "body": in.Body, "created_at": time.Now().Format(time.RFC3339), "user": me}
		if in.ReplyToID != nil {
			m["reply_to"] = map[string]any{"id": *in.ReplyToID, "user_id": "u1", "body": "hey, how's the TUI?"}
		}
		f.messages = append(f.messages, m)
		out = map[string]any{"data": m}
	case "PUT /api/v1/remotes/@ada/read":
		var in map[string]int64
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.read = append(f.read, in["through"])
		w.WriteHeader(http.StatusNoContent)
		return
	case "POST /api/v1/remotes/@ada/archive":
		f.stashed = true
		w.WriteHeader(http.StatusNoContent)
		return
	case "POST /api/v1/remotes/@eve/merge":
		f.merged = true
		w.WriteHeader(http.StatusNoContent)
		return
	case "GET /api/v1/remotes/orgs/o1/messages":
		out = map[string]any{"data": map[string]any{
			"org": map[string]any{"id": "o1", "name": "Gophers"}, "role": "maintainer", "access": "open",
			"messages": []any{map[string]any{"id": 5, "user_id": "u1", "event": "added", "user": ada, "subject": me, "created_at": at}},
			"members":  []any{map[string]any{"user": me, "role": "maintainer"}, map[string]any{"user": ada, "role": "contributor"}},
		}}
	case "PUT /api/v1/remotes/orgs/o1/members/ada":
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.roles = append(f.roles, in["role"])
		w.WriteHeader(http.StatusNoContent)
		return
	case "GET /api/v1/remotes/messages/1/link-preview":
		out = map[string]any{"link_preview": map[string]any{"url": "https://example.com", "title": "Example Domain", "site_name": "example.com"}}
	case "GET /api/v1/log/following":
		out = map[string]any{"data": []any{map[string]any{
			"id": "c1", "short_hash": "abc1234", "body": "shipped the TUI", "status": "approved", "created_at": at,
			"html_url": "https://dev-null.sh/@ada/commits/c1", "author": ada,
		}}, "meta": map[string]any{"next_cursor": nil}}
	case "GET /api/v1/remotes/candidates":
		f.purposes = append(f.purposes, r.URL.Query().Get("for"))
		out = map[string]any{"candidates": []any{map[string]any{"user": map[string]any{"id": "u4", "handle": "bob", "name": "Bob"}, "reason": nil}}}
	case "POST /api/v1/remotes/@bob/messages":
		var in api.NewMessage
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.shared = append(f.shared, in.Body)
		out = map[string]any{"data": map[string]any{"id": 20, "user_id": "u0", "body": in.Body, "created_at": time.Now().Format(time.RFC3339)}}
	case "GET /api/v1/remotes/@bob/messages":
		var messages []any
		for _, body := range f.shared {
			messages = append(messages, map[string]any{"id": 20, "user_id": "u0", "body": body, "created_at": time.Now().Format(time.RFC3339)})
		}
		out = map[string]any{"data": map[string]any{"partner": map[string]any{"id": "u4", "handle": "bob", "name": "Bob"}, "messages": messages, "access": "open"}}
	case "GET /api/v1/remotes/reactions":
		out = map[string]any{"usage": []string{"🔥"}}
	case "POST /api/v1/remotes/messages/1/reactions":
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.reacted = append(f.reacted, in["emoji"])
		out = map[string]any{"reactions": []any{map[string]any{"user_id": "u0", "emoji": in["emoji"]}}}
	default:
		out = map[string]any{"data": []any{}, "meta": map[string]any{"next_cursor": nil}}
	}

	_ = json.NewEncoder(w).Encode(out)
}

func TestReadsAndPushesInARemote(t *testing.T) {
	f := &remotesAPI{}
	h := start(t, f)
	h.waitFor("Following")

	h.typeText("gm")
	h.waitFor("hey, how's the TUI?")
	h.waitFor("Gophers")

	h.key(tcell.KeyEnter)
	h.waitFor("almost there")
	h.waitFor("🎉")
	h.waitFor("✓ pulled")

	h.typeText("c")
	h.waitFor("Push to @ada")
	h.typeText("shipping tonight")
	h.key(tcell.KeyCtrlS)
	h.waitForGone("Push to @ada")
	h.waitFor("shipping tonight")

	// A reply to the first message.
	h.typeText("gg")
	h.typeText("r")
	h.waitFor("replying to @ada")
	h.typeText("good, thanks")
	h.key(tcell.KeyCtrlS)
	h.waitFor("↪ @ada: hey, how's the TUI?")

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pushed) != 2 || f.pushed[0].Body != "shipping tonight" || f.pushed[1].ReplyToID == nil || *f.pushed[1].ReplyToID != 1 {
		t.Fatalf("pushed %+v", f.pushed)
	}
	if f.pushed[0].ClientID == "" || f.pushed[0].ClientID == f.pushed[1].ClientID {
		t.Errorf("client ids %q %q", f.pushed[0].ClientID, f.pushed[1].ClientID)
	}
	if len(f.read) == 0 || f.read[0] != 2 {
		t.Errorf("read through %v", f.read)
	}
}

func TestReactsToAMessage(t *testing.T) {
	f := &remotesAPI{}
	h := start(t, f)
	h.waitFor("Following")

	h.typeText("gm")
	h.waitFor("hey, how's the TUI?")
	h.key(tcell.KeyEnter)
	h.waitFor("almost there")

	h.typeText("gg")
	h.typeText("e")
	h.waitFor("React with")
	h.key(tcell.KeyEnter)
	h.waitForGone("React with")
	h.waitFor("🔥")

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reacted) != 1 || f.reacted[0] != "🔥" {
		t.Errorf("reacted %v", f.reacted)
	}
}

func TestStashesAndAcceptsConversations(t *testing.T) {
	f := &remotesAPI{}
	h := start(t, f)
	h.waitFor("Following")

	h.typeText("gm")
	h.waitFor("hey, how's the TUI?")
	h.typeText("S")
	h.waitFor("Stashed it")
	h.waitForGone("hey, how's the TUI?")

	h.typeText("T")
	h.waitFor("can we talk?")
	h.typeText("a")
	h.waitFor("Accepted @eve")

	h.typeText("T")
	h.waitFor("hey, how's the TUI?")

	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.stashed || !f.merged {
		t.Errorf("stashed %v, merged %v", f.stashed, f.merged)
	}
}

func TestShowsUnreadRemotesOnTheTab(t *testing.T) {
	h := start(t, &remotesAPI{})
	h.waitUntil(func(s string) bool { return strings.Contains(s, "Remotes 1") || strings.Contains(s, "Rmt 1") }, "show the unread count")
}

func TestManagesAnOrg(t *testing.T) {
	f := &remotesAPI{}
	h := start(t, f)
	h.waitFor("Following")

	h.typeText("gm")
	h.waitFor("Gophers")
	h.typeText("j")
	h.key(tcell.KeyEnter)
	h.waitFor("@ada added @me")

	h.typeText("M")
	h.waitFor("⟨maintainer⟩")
	h.typeText("j")
	h.typeText("m")
	h.waitFor("@ada is a maintainer now.")

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.roles) != 1 || f.roles[0] != "maintainer" {
		t.Errorf("set roles %v", f.roles)
	}
}

func TestReadsALinkCardAgainWhileItIsMade(t *testing.T) {
	every := remoteEvery
	remoteEvery = 50 * time.Millisecond
	t.Cleanup(func() { remoteEvery = every })

	h := start(t, &remotesAPI{pending: true})
	h.waitFor("Following")

	h.typeText("gm")
	h.waitFor("hey, how's the TUI?")
	h.key(tcell.KeyEnter)
	h.waitFor("reading the link")
	h.waitFor("Example Domain")
}

func TestSendsACommitInRemotes(t *testing.T) {
	f := &remotesAPI{}
	h := start(t, f)
	h.waitFor("shipped the TUI")

	h.typeText("m")
	h.waitFor("Send abc1234 in Remotes")
	h.typeText("bo")
	h.waitFor("@bob (Bob)")
	h.key(tcell.KeyEnter)
	h.waitFor("Send abc1234 to @bob?")
	h.key(tcell.KeyRight)
	h.key(tcell.KeyEnter)
	h.waitFor("Sent abc1234 to @bob.")
	h.waitFor("Following › @bob")
	h.waitFor("dev-null.sh/@ada/commits/c1")

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.shared) != 1 || f.shared[0] != "https://dev-null.sh/@ada/commits/c1" {
		t.Errorf("sent %v", f.shared)
	}
	for _, p := range f.purposes {
		if p != "share" {
			t.Errorf("asked for candidates for %q", p)
		}
	}
}
