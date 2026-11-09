package tui

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// orgAPI has an organization, @acme, where a user's profile would be, and what it committed.
type orgAPI struct {
	mu        sync.Mutex
	following bool
	claimed   bool
}

func (f *orgAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	me := map[string]any{"id": "u0", "handle": "me", "name": "Me", "lane": 1}
	acme := map[string]any{"type": "organization", "id": "o1", "handle": "acme", "name": "Acme Corp", "lane": "#f70500"}
	page := func(data ...any) map[string]any {
		return map[string]any{"data": data, "meta": map[string]any{"next_cursor": nil}}
	}
	at := time.Now().Add(-time.Hour).Format(time.RFC3339)

	var out any
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/me":
		out = map[string]any{"data": me}
	case "GET /api/v1/log/following":
		out = page(map[string]any{"id": "c1", "short_hash": "aaa1111", "body": "we shipped", "status": "approved", "created_at": at, "author": acme})
	case "GET /api/v1/users/acme", "GET /api/v1/users/acme/commits":
		w.WriteHeader(http.StatusNotFound)
		out = map[string]any{"message": "No query results for model [App\\Models\\User] acme"}
	case "GET /api/v1/organizations/acme":
		followers := 41
		if f.following {
			followers = 42
		}
		out = map[string]any{
			"data": map[string]any{
				"handle": "acme", "name": "Acme Corp", "bio": "We make anvils.", "followers_count": followers,
				"admins": []any{map[string]any{"id": "u1", "handle": "wile", "name": "Wile E."}},
			},
			"is_following": f.following, "can_claim": !f.claimed,
		}
	case "GET /api/v1/organizations/acme/commits":
		out = page(map[string]any{"id": "c1", "short_hash": "aaa1111", "body": "we shipped", "status": "approved", "created_at": at, "author": acme})
	case "GET /api/v1/organizations/acme/mentions":
		out = page(map[string]any{"id": "c2", "short_hash": "bbb2222", "body": "thanks @acme", "status": "approved", "created_at": at, "author": me})
	case "POST /api/v1/organizations/acme/claim":
		f.claimed = true
		w.WriteHeader(http.StatusNoContent)
		return
	case "POST /api/v1/organizations/acme/follow":
		f.following = true
		w.WriteHeader(http.StatusNoContent)
		return
	default:
		out = page()
	}

	_ = json.NewEncoder(w).Encode(out)
}

func TestOpensAnOrganizationFromItsCommit(t *testing.T) {
	f := &orgAPI{}
	h := start(t, f)
	h.waitFor("we shipped")

	h.typeText("u")
	h.waitFor("We make anvils.")
	h.waitFor("⟨organization⟩")
	h.waitFor("Admins @wile")
	h.waitFor("41 followers")

	h.typeText("T")
	h.waitFor("thanks @acme")

	h.typeText("F")
	h.waitFor("Following @acme.")
	h.waitFor("42 followers")
	h.waitFor("✓ following")
}

func TestOpensAnOrganizationWhereAProfileWouldBe(t *testing.T) {
	h := start(t, &orgAPI{})
	h.waitFor("we shipped")

	h.typeText("/")
	h.waitFor("Search")
	h.typeText("@acme")
	h.key(tcell.KeyEnter)
	h.waitFor("We make anvils.")
	h.waitForGone("not found")
}

func TestClaimsAnOrganization(t *testing.T) {
	f := &orgAPI{}
	h := start(t, f)
	h.waitFor("we shipped")

	h.typeText("u")
	h.waitFor("We make anvils.")
	h.typeText("C")
	h.waitFor("Claim @acme's page?")
	h.key(tcell.KeyRight)
	h.key(tcell.KeyEnter)
	h.waitFor("You run @acme's page now.")

	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.claimed {
		t.Error("didn't claim it")
	}
}
