// Command server is a stand-in for the /dev/null API with made-up people and commits, for recording the demo with
// vhs (see demo.tape) without touching dev-null.sh. It listens on a free local port and writes a dn config that points
// at it into $XDG_CONFIG_HOME, so `dn` started with the same XDG_CONFIG_HOME talks to it. It doesn't check signatures.
package main

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/eggplants/dev-null-tui/internal/config"
)

type user map[string]any

type commit struct {
	ID        string
	Author    user
	Body      string
	Segments  []map[string]any
	Ago       time.Duration
	Stars     int
	Starred   bool
	Saved     bool
	ParentID  string
	Quoted    string
	Link      map[string]any
	Poll      map[string]any
	Images    int
	CreatedAt time.Time
}

var (
	you  = user{"id": "u0", "username": "you", "name": "You", "lane": 1, "bio": "Shipping things from the terminal."}
	noor = user{"id": "u1", "username": "noor", "name": "Noor Haddad", "lane": 3, "bio": "Go, terminals and too much coffee.", "location": "Lisbon"}
	sam  = user{"id": "u2", "username": "sam", "name": "Sam Rivera", "lane": 4}
	mika = user{"id": "u3", "username": "mika", "name": "Mika Tanaka", "lane": "#fcc419"}
)

type server struct {
	mu      sync.Mutex
	commits []*commit
	start   time.Time
	next    int
}

func newServer() *server {
	s := &server{start: time.Now()}
	add := func(c *commit) { s.commits = append(s.commits, c) }

	add(&commit{ID: "c1", Author: noor, Ago: 12 * time.Minute, Stars: 41,
		Body: "Shipped dn, a terminal client for /dev/null. Read your log, reply, star and commit without leaving tmux.",
		Link: map[string]any{"url": "https://github.com/eggplants/dev-null-tui", "title": "eggplants/dev-null-tui: A terminal client for /dev/null", "site_name": "GitHub"}})
	add(&commit{ID: "c2", Author: sam, Ago: 38 * time.Minute, Stars: 9,
		Body: "TIL `go test -race` catches a data race in 3 lines:",
		Segments: []map[string]any{
			{"type": "text", "text": "TIL "}, {"type": "code", "code": "go test -race"}, {"type": "text", "text": " catches a data race in 3 lines:\n"},
			{"type": "block", "language": "go", "html": "<pre>go func() { n++ }()\nn++\nwg.Wait()</pre>"},
		}})
	add(&commit{ID: "c3", Author: mika, Ago: 2 * time.Hour, Stars: 17,
		Body: "Settle it once and for all:",
		Poll: map[string]any{"id": "p1", "is_closed": false, "has_voted": true, "total_votes": 128, "closes_at": time.Now().Add(20 * time.Hour).Format(time.RFC3339),
			"options": []any{
				map[string]any{"id": "o1", "label": "Tabs", "voted": true, "votes_count": 71, "percentage": 55},
				map[string]any{"id": "o2", "label": "Spaces", "voted": false, "votes_count": 57, "percentage": 45},
			}}})
	add(&commit{ID: "c4", Author: sam, Ago: 3 * time.Hour, Stars: 6, Quoted: "c1",
		Body: "Finally, a reason to keep a terminal open on the second monitor."})
	add(&commit{ID: "c5", Author: mika, Ago: 5 * time.Hour, Stars: 23, Images: 2,
		Body: "Pairing on the new onboarding flow today. Whiteboards everywhere."})

	add(&commit{ID: "r1", Author: mika, Ago: 9 * time.Minute, ParentID: "c1", Stars: 3, Body: "The thread view feels just like the web."})
	add(&commit{ID: "r2", Author: sam, Ago: 6 * time.Minute, ParentID: "c1", Stars: 1, Body: "Installing it right now."})

	return s
}

func (s *server) find(id string) *commit {
	for _, c := range s.commits {
		if c.ID == id {
			return c
		}
	}

	return nil
}

func (s *server) json(c *commit, withPreview bool) map[string]any {
	created := c.CreatedAt
	if created.IsZero() {
		created = s.start.Add(-c.Ago)
	}

	var replies []*commit
	for _, r := range s.commits {
		if r.ParentID == c.ID {
			replies = append(replies, r)
		}
	}

	out := map[string]any{
		"id": c.ID, "short_hash": shortHash(c.ID),
		"body": c.Body, "body_segments": c.Segments, "status": "approved",
		"html_url":   "https://dev-null.sh/@" + c.Author["username"].(string) + "/commits/" + c.ID,
		"created_at": created.UTC().Format(time.RFC3339), "author": c.Author,
		"stars_count": c.Stars, "starred": c.Starred, "is_saved": c.Saved, "replies_count": len(replies),
		"link_preview": c.Link, "poll": c.Poll, "images": make([]any, c.Images),
	}
	if c.ParentID != "" {
		out["parent_id"] = c.ParentID
	}
	if q := s.find(c.Quoted); q != nil {
		out["quoted_commit_id"] = q.ID
		out["quoted_commit"] = s.json(q, false)
	}
	if withPreview && len(replies) > 0 {
		out["reply_preview"] = map[string]any{"data": []any{s.json(replies[len(replies)-1], false)}, "next_cursor": nil}
	}

	return out
}

func (s *server) list(keep func(*commit) bool, withPreview bool) map[string]any {
	data := []any{}
	for _, c := range s.commits {
		if keep(c) {
			data = append(data, s.json(c, withPreview))
		}
	}

	return map[string]any{"data": data, "meta": map[string]any{"next_cursor": nil}}
}

func shortHash(id string) string {
	h := fnv.New32a()
	h.Write([]byte(id))

	return fmt.Sprintf("%07x", h.Sum32()&0xfffffff)
}

func topLevel(c *commit) bool { return c.ParentID == "" }

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	parts := strings.Split(path, "/")
	route := r.Method + " " + path

	var out any
	switch {
	case route == "GET me":
		out = map[string]any{"data": you, "unread_notifications_count": 3}
	case r.Method == "GET" && parts[0] == "log":
		out = s.list(topLevel, true)
	case route == "GET saved":
		out = s.list(func(c *commit) bool { return c.Saved }, true)
	case route == "GET notifications":
		out = map[string]any{"data": []any{
			map[string]any{"id": "n1", "type": "starred", "created_at": s.start.Add(-4 * time.Minute).Format(time.RFC3339), "actor": sam, "actors_count": 3, "unread_count": 3, "commit": s.json(s.find("c5"), false)},
			map[string]any{"id": "n2", "type": "followed", "created_at": s.start.Add(-time.Hour).Format(time.RFC3339), "actor": mika, "actors_count": 1, "unread_count": 0},
			map[string]any{"id": "n3", "type": "replied", "created_at": s.start.Add(-3 * time.Hour).Format(time.RFC3339), "actor": noor, "actors_count": 1, "unread_count": 0, "commit": s.json(s.find("r1"), false)},
		}, "meta": map[string]any{"next_cursor": nil}}
	case r.Method == "GET" && parts[0] == "users" && len(parts) == 2:
		u := map[string]user{"you": you, "noor": noor, "sam": sam, "mika": mika}[parts[1]]
		if u == nil {
			http.NotFound(w, r)
			return
		}
		counted := user{}
		for k, v := range u {
			counted[k] = v
		}
		counted["commits_count"], counted["followers_count"], counted["following_count"] = 128, 2048, 64
		out = map[string]any{"data": counted, "is_following": u["username"] != "you", "follows_you": u["username"] == "noor"}
	case r.Method == "GET" && parts[0] == "users" && len(parts) == 3 && parts[2] == "commits":
		out = s.list(func(c *commit) bool { return topLevel(c) && c.Author["username"] == parts[1] }, true)
	case r.Method == "GET" && parts[0] == "commits" && len(parts) == 2:
		c := s.find(parts[1])
		if c == nil {
			http.NotFound(w, r)
			return
		}
		ancestors := []any{}
		for p := s.find(c.ParentID); p != nil; p = s.find(p.ParentID) {
			ancestors = append([]any{s.json(p, false)}, ancestors...)
		}
		out = map[string]any{"data": s.json(c, false), "ancestors": ancestors}
	case r.Method == "GET" && parts[0] == "commits" && len(parts) == 3 && parts[2] == "replies":
		out = s.list(func(c *commit) bool { return c.ParentID == parts[1] }, false)
	case parts[0] == "commits" && len(parts) == 3 && (parts[2] == "star" || parts[2] == "bookmark" || parts[2] == "pasteis"):
		c := s.find(parts[1])
		if c == nil {
			http.NotFound(w, r)
			return
		}
		switch {
		case parts[2] == "star" && r.Method == "POST" && !c.Starred:
			c.Starred, c.Stars = true, c.Stars+1
		case parts[2] == "star" && r.Method == "DELETE" && c.Starred:
			c.Starred, c.Stars = false, c.Stars-1
		case parts[2] == "bookmark":
			c.Saved = r.Method == "PUT"
		}
		out = map[string]any{"data": s.json(c, false)}
	case route == "POST commits":
		var in struct {
			Body     string `json:"body"`
			ParentID string `json:"parent_id"`
		}
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &in)
		s.next++
		c := &commit{ID: fmt.Sprintf("n%d", s.next), Author: you, Body: in.Body, ParentID: in.ParentID, CreatedAt: time.Now()}
		s.commits = append(s.commits, c)
		w.WriteHeader(http.StatusCreated)
		out = map[string]any{"data": s.json(c, false)}
	case parts[0] == "users" && len(parts) == 3 && parts[2] == "follow":
		w.WriteHeader(http.StatusNoContent)
		return
	default:
		out = map[string]any{"data": []any{}, "meta": map[string]any{"next_cursor": nil}}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func main() {
	if os.Getenv("XDG_CONFIG_HOME") == "" {
		log.Fatal("set XDG_CONFIG_HOME to a scratch directory: the demo writes a dn config there")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}

	dir, err := config.Dir()
	if err != nil {
		log.Fatal(err)
	}
	keyPath := filepath.Join(dir, "id_ed25519")
	if _, err := config.LoadOrCreateKey(keyPath); err != nil {
		log.Fatal(err)
	}
	cfg := &config.Config{BaseURL: "http://" + ln.Addr().String(), KeyPath: keyPath, Username: "you"}
	if err := cfg.Save(); err != nil {
		log.Fatal(err)
	}

	log.Fatal(http.Serve(ln, newServer()))
}
