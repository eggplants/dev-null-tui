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

// actionsAPI answers a log of someone else's commit with a poll, in French, and one of the user's own, and the
// actions the web's commit cards have on them.
type actionsAPI struct {
	mu       sync.Mutex
	pinned   bool
	voted    string
	deleted  bool
	language string
}

func (f *actionsAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	me := map[string]any{"id": "u0", "handle": "me", "name": "Me", "lane": 1}
	alice := map[string]any{"id": "u1", "handle": "alice", "name": "Alice", "lane": 2}
	at := time.Now().Add(-time.Hour).Format(time.RFC3339)
	poll := func() map[string]any {
		option := func(id, label string) map[string]any {
			o := map[string]any{"id": id, "label": label, "voted": f.voted == id}
			if f.voted != "" {
				pct := 0
				if f.voted == id {
					pct = 100
				}
				o["percentage"] = pct
			}
			return o
		}
		total := 0
		if f.voted != "" {
			total = 1
		}
		return map[string]any{"id": "p1", "has_voted": f.voted != "", "total_votes": total, "options": []any{option("o1", "Tabs"), option("o2", "Spaces")}}
	}
	theirs := func() map[string]any {
		return map[string]any{
			"id": "c1", "short_hash": "aaa1111", "body": "bonjour le monde", "status": "approved", "created_at": at,
			"author": alice, "stars_count": 2, "poll": poll(),
		}
	}
	mine := func() map[string]any {
		return map[string]any{
			"id": "c2", "short_hash": "bbb2222", "body": "shipping it", "status": "approved", "created_at": at,
			"author": me, "is_pinned": f.pinned,
		}
	}
	page := func(data ...any) map[string]any {
		return map[string]any{"data": data, "meta": map[string]any{"next_cursor": nil}}
	}

	var out any
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/me":
		out = map[string]any{"data": me}
	case "GET /api/v1/log/following":
		if f.deleted {
			out = page(theirs())
		} else {
			out = page(theirs(), mine())
		}
	case "PUT /api/v1/commits/c2/pin":
		f.pinned = true
		out = map[string]any{"data": mine()}
	case "DELETE /api/v1/commits/c2/pin":
		f.pinned = false
		out = map[string]any{"data": mine()}
	case "POST /api/v1/commits/c1/poll/votes":
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.voted = in["option"]
		out = map[string]any{"data": theirs()}
	case "GET /api/v1/me/settings":
		out = map[string]any{"data": map[string]any{"translation_language": "en"}}
	case "POST /api/v1/commits/c1/translation":
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.language = in["language"]
		out = map[string]any{"translation": map[string]any{
			"language": in["language"], "source_language": "fr", "source_name": "French", "available": true,
			"translated": map[string]any{"body_segments": []any{map[string]any{"type": "text", "text": "hello world"}}},
		}}
	case "GET /api/v1/commits/c1/stars":
		out = page(map[string]any{"id": "u2", "handle": "bob", "name": "Bob"})
	case "DELETE /api/v1/commits/c2":
		f.deleted = true
		w.WriteHeader(http.StatusNoContent)
		return
	case "POST /api/v1/commits/c2/restoration":
		f.deleted = false
		out = map[string]any{"data": mine()}
	default:
		out = page()
	}

	_ = json.NewEncoder(w).Encode(out)
}

func TestPinsAndUnpinsTheUsersCommit(t *testing.T) {
	f := &actionsAPI{}
	h := start(t, f)
	h.waitFor("bbb2222")

	h.typeText("j")
	h.typeText("p")
	h.waitFor("Pinned bbb2222")
	h.waitFor("◇ pinned")

	h.typeText("p")
	h.waitFor("Unpinned bbb2222")
	h.waitForGone("◇ pinned")
}

func TestOnlyPinsTheUsersOwnCommits(t *testing.T) {
	h := start(t, &actionsAPI{})
	h.waitFor("aaa1111")

	h.typeText("p")
	h.waitFor("You can only pin your own commits.")
}

func TestVotesOnAPoll(t *testing.T) {
	f := &actionsAPI{}
	h := start(t, f)
	h.waitFor("v to vote")

	h.typeText("v")
	h.waitFor("Vote for")
	h.key(tcell.KeyRight)
	h.key(tcell.KeyEnter)
	h.waitFor("Voted for Spaces")
	h.waitFor("100%")
	h.waitForGone("v to vote")

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.voted != "o2" {
		t.Errorf("voted for %q, want o2", f.voted)
	}
}

func TestTranslatesACommitAndShowsItAsWrittenAgain(t *testing.T) {
	f := &actionsAPI{}
	h := start(t, f)
	h.waitFor("bonjour le monde")

	h.typeText("t")
	h.waitFor("hello world")
	h.waitFor("translated from French")
	h.waitForGone("bonjour le monde")

	h.typeText("t")
	h.waitFor("bonjour le monde")
	h.waitForGone("translated from French")

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.language != "en" {
		t.Errorf("translated into %q, want the language from the settings", f.language)
	}
}

func TestListsWhoStarredACommit(t *testing.T) {
	h := start(t, &actionsAPI{})
	h.waitFor("aaa1111")

	h.typeText("S")
	h.waitFor("Bob @bob")
	h.waitFor("Stars on aaa1111")
}

func TestUndoesDeletingACommit(t *testing.T) {
	h := start(t, &actionsAPI{})
	h.waitFor("bbb2222")

	h.typeText("j")
	h.typeText("D")
	h.waitFor("Delete commit bbb2222?")
	h.key(tcell.KeyRight)
	h.key(tcell.KeyEnter)
	h.waitFor("U to undo")
	h.waitForGone("shipping it")

	h.typeText("U")
	h.waitFor("Restored bbb2222")
	h.waitFor("shipping it")
}

func TestSaysWhenThereIsNothingToUndo(t *testing.T) {
	h := start(t, &actionsAPI{})
	h.waitFor("aaa1111")

	h.typeText("U")
	h.waitUntil(func(s string) bool { return strings.Contains(s, "Nothing to undo") }, "say there is nothing to undo")
}
