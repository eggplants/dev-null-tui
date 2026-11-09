package tui

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// searchAPI answers searches for commits and profiles, and records what was searched for.
type searchAPI struct {
	mu      sync.Mutex
	queries []string
}

func (f *searchAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	me := map[string]any{"id": "u0", "handle": "me", "name": "Me", "lane": 1}
	page := func(data ...any) map[string]any {
		return map[string]any{"data": data, "meta": map[string]any{"next_cursor": nil}}
	}

	var out any
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/me":
		out = map[string]any{"data": me}
	case "GET /api/v1/search/commits":
		f.queries = append(f.queries, "commits:"+r.URL.Query().Get("q"))
		out = page(map[string]any{
			"id": "c1", "short_hash": "abc1234", "body": "octane in production", "status": "approved",
			"created_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
			"author":     map[string]any{"type": "user", "id": "u1", "handle": "alice", "name": "Alice"},
		})
	case "GET /api/v1/search/mentions":
		f.queries = append(f.queries, "mentions:"+r.URL.Query().Get("q"))
		out = page(map[string]any{"id": "u2", "handle": "octavia", "name": "Octavia"})
	case "GET /api/v1/search/issues":
		if r.URL.Query().Get("q") == "1363" {
			out = map[string]any{"issue": map[string]any{"number": 1363, "title": "[RFC] Gists", "url": "https://github.com/dev-null-sh/dev-null.sh/issues/1363"}}
		} else {
			out = map[string]any{"issue": nil}
		}
	case "GET /api/v1/search/profiles":
		f.queries = append(f.queries, "profiles:"+r.URL.Query().Get("q"))
		out = map[string]any{
			"users":         []any{map[string]any{"id": "u2", "handle": "octavia", "name": "Octavia"}},
			"organizations": []any{map[string]any{"handle": "octane-org", "name": "Octane Org"}},
		}
	default:
		out = page()
	}

	_ = json.NewEncoder(w).Encode(out)
}

func TestSearchesCommitsAndPeople(t *testing.T) {
	f := &searchAPI{}
	h := start(t, f)
	h.waitFor("Following")

	h.typeText("/")
	h.waitFor("Search")
	h.typeText("octane")
	h.key(tcell.KeyEnter)
	h.waitFor("$ grep octane")
	h.waitFor("octane in production")

	h.typeText("T")
	h.waitFor("Octavia @octavia")
	h.waitFor("Octane Org @octane-org")

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queries) != 2 || f.queries[0] != "commits:octane" || f.queries[1] != "profiles:octane" {
		t.Errorf("searched %v", f.queries)
	}
}

func TestSearchesOnlyPeopleForAShortTerm(t *testing.T) {
	f := &searchAPI{}
	h := start(t, f)
	h.waitFor("Following")

	h.typeText("/")
	h.waitFor("Search")
	h.typeText("oc")
	h.key(tcell.KeyEnter)
	h.waitFor("Octavia @octavia")

	f.mu.Lock()
	defer f.mu.Unlock()
	for _, q := range f.queries {
		if q == "commits:oc" {
			t.Error("searched commits for a term the API turns down")
		}
	}
}

func TestCompletesMentionsTheAPISuggests(t *testing.T) {
	f := &searchAPI{}
	h := start(t, f)
	h.waitFor("Following")

	h.typeText("c")
	h.waitFor("New commit")
	h.typeText("thanks @oc")
	h.waitFor("tab @octavia")
	h.key(tcell.KeyTab)
	h.waitFor("thanks @octavia")

	f.mu.Lock()
	defer f.mu.Unlock()
	// Only the last of the keys typed quickly asks.
	asked := 0
	for _, q := range f.queries {
		if q == "mentions:o" || q == "mentions:" {
			t.Errorf("asked for %s while still typing", q)
		}
		if q == "mentions:oc" {
			asked++
		}
	}
	if asked != 1 {
		t.Errorf("asked %v", f.queries)
	}
}

func TestCompletesAnIssueToItsLink(t *testing.T) {
	h := start(t, &searchAPI{})
	h.waitFor("Following")

	h.typeText("c")
	h.waitFor("New commit")
	h.typeText("see #1363")
	h.waitFor("#1363 [RFC] Gists")
	h.key(tcell.KeyTab)
	h.waitFor("see https://github.com/dev-null-sh/dev-null.sh/issues/1363")

	h.typeText("and #99")
	h.waitFor("/dev/null has no issue #99")
}
