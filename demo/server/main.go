// Command server is a stand-in for the /dev/null API with made-up people and commits, for recording the demo with
// vhs (see demo.tape) without touching dev-null.sh. It listens on a free local port and writes a dn config that points
// at it into $XDG_CONFIG_HOME, so `dn` started with the same XDG_CONFIG_HOME talks to it. It doesn't check signatures.
package main

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"math"
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
	ParentID  string
	Quoted    string
	Link      map[string]any
	Poll      map[string]any
	Images    int
	Pasteis   int
	Mentions  []map[string]any
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
			{"type": "block", "language": "go", "html": `<span class="hl-keyword">go</span> <span class="hl-keyword">func</span>() { n++ }() <span class="hl-comment">// races</span>` + "\nn++\nwg.<span class=\"hl-property\">Wait</span>()"},
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
	add(&commit{ID: "c5", Author: mika, Ago: 5 * time.Hour, Stars: 23, Images: 3, Pasteis: 4,
		Body: "Pairing on the new onboarding flow today. Whiteboards everywhere."})
	add(&commit{ID: "c6", Author: noor, Ago: 27 * time.Hour, Stars: 1280,
		Body:     "Thanks @mika for the review on https://github.com/eggplants/dev-null-tui/pull/42, it's in!",
		Mentions: []map[string]any{{"username": "mika", "offset": 7, "length": 5}}})
	add(&commit{ID: "c7", Author: you, Ago: 50 * time.Hour, Stars: 3,
		Body: "First commit from the terminal. Hello, /dev/null!"})

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
		"stars_count": c.Stars, "starred": c.Starred, "replies_count": len(replies),
		"link_preview": c.Link, "poll": c.Poll, "images": images(c), "pasteis_count": c.Pasteis, "mentions": c.Mentions,
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

// base is the demo server's own address, which its images are served from.
var base string

func images(c *commit) []any {
	out := []any{}
	for i := range c.Images {
		out = append(out, map[string]any{"id": fmt.Sprintf("%s-%d", c.ID, i), "url": fmt.Sprintf("%s/img/%s-%d.png", base, c.ID, i), "width": 1600, "height": 900})
	}

	return out
}

// picture draws a made-up photo: a sky fading into the ground, with a sun, in colors that differ by name.
func picture(w http.ResponseWriter, name string) {
	h := fnv.New32a()
	h.Write([]byte(name))
	hue := float64(h.Sum32()%360) / 360

	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for y := range 180 {
		for x := range 320 {
			t := float64(y) / 180
			r, g, b := hsv(hue+t*0.15, 0.55, 1-t*0.6)
			if dx, dy := float64(x-220), float64(y-70); dx*dx+dy*dy < 900 {
				r, g, b = 255, 210, 90
			}
			if y > 130 {
				r, g, b = r/3, g/3, b/3
			}
			img.Set(x, y, color.RGBA{r, g, b, 255})
		}
	}
	w.Header().Set("Content-Type", "image/png")
	_ = png.Encode(w, img)
}

func hsv(h, s, v float64) (uint8, uint8, uint8) {
	h -= math.Floor(h)
	i := int(h * 6)
	f := h*6 - float64(i)
	p, q, t := v*(1-s), v*(1-f*s), v*(1-(1-f)*s)
	rgb := [6][3]float64{{v, t, p}, {q, v, p}, {p, v, t}, {p, q, v}, {t, p, v}, {v, p, q}}[i%6]

	return uint8(rgb[0] * 255), uint8(rgb[1] * 255), uint8(rgb[2] * 255)
}

// activity is a year of made-up commits, a few a week with busier spells.
func activity(now time.Time) map[string]any {
	start := now.AddDate(0, 0, -7*52)
	start = start.AddDate(0, 0, -int(start.Weekday()))
	var buckets []any
	for d := start; !d.After(now); d = d.AddDate(0, 0, 1) {
		h := fnv.New32a()
		h.Write([]byte(d.Format("2006-01-02")))
		n := int(h.Sum32() % 9)
		if n > 5 || d.Month()%3 == 0 && n > 2 {
			n = 0
		}
		buckets = append(buckets, map[string]any{"date": d.Format("2006-01-02"), "count": n})
	}

	return map[string]any{"scale": "year", "buckets": buckets}
}

func achievements() map[string]any {
	badge := func(id, name string, progress, target int) map[string]any {
		return map[string]any{"id": id, "name": name, "unlocked": progress >= target, "progress": progress, "target": target}
	}

	return map[string]any{
		"level": 4, "rank": "maintainer", "xp": 791, "next_level_xp": 1000, "level_progress": 46,
		"badges": []any{
			badge("init", "init", 1, 1), badge("syntax", "syntax", 1, 1), badge("uptime", "uptime", 7, 7),
			badge("daemon", "daemon", 3, 7), badge("stdout", "stdout", 40, 100), badge("pipe", "pipe", 2, 10),
		},
	}
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
	if name, ok := strings.CutPrefix(r.URL.Path, "/img/"); ok {
		picture(w, name)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	parts := strings.Split(path, "/")
	route := r.Method + " " + path

	var out any
	switch {
	case route == "GET me":
		me := user{"pasteis_mode": true, "pasteis_remaining": 7, "preferred_lane": 1, "automatic_lane": 4}
		for k, v := range you {
			me[k] = v
		}
		out = map[string]any{"data": me, "unread_notifications_count": 3}
	case r.Method == "GET" && parts[0] == "log":
		out = s.list(topLevel, true)
	case route == "GET starred":
		out = s.list(func(c *commit) bool { return c.Starred }, true)
	case route == "GET notifications":
		all := []any{
			map[string]any{"id": "n1", "type": "starred", "created_at": s.start.Add(-4 * time.Minute).Format(time.RFC3339), "actor": sam, "actors_count": 3, "unread_count": 3, "commit": s.json(s.find("c5"), false)},
			map[string]any{"id": "n2", "type": "followed", "created_at": s.start.Add(-time.Hour).Format(time.RFC3339), "actor": mika, "actors_count": 1, "unread_count": 0},
			map[string]any{"id": "n3", "type": "replied", "created_at": s.start.Add(-3 * time.Hour).Format(time.RFC3339), "actor": noor, "actors_count": 1, "unread_count": 0, "commit": s.json(s.find("r1"), false)},
			map[string]any{"id": "n4", "type": "mentioned", "created_at": s.start.Add(-27 * time.Hour).Format(time.RFC3339), "actor": noor, "actors_count": 1, "unread_count": 0, "commit": s.json(s.find("c6"), false)},
			map[string]any{"id": "n5", "type": "approved", "created_at": s.start.Add(-50 * time.Hour).Format(time.RFC3339), "actors_count": 0, "unread_count": 0, "commit": s.json(s.find("c7"), false)},
		}
		kinds := map[string]string{"starred": "stars", "followed": "follows", "replied": "replies", "mentioned": "mentions", "approved": "moderation"}
		data := []any{}
		for _, n := range all {
			if t := r.URL.Query().Get("type"); t == "" || t == "all" || kinds[n.(map[string]any)["type"].(string)] == t {
				data = append(data, n)
			}
		}
		out = map[string]any{"data": data, "meta": map[string]any{"next_cursor": nil}}
	case r.Method == "GET" && parts[0] == "notifications" && len(parts) == 3:
		var data []any
		for _, u := range []user{sam, noor, mika} {
			data = append(data, map[string]any{"id": "g-" + u["username"].(string), "type": "starred", "created_at": s.start.Add(-4 * time.Minute).Format(time.RFC3339), "actor": u, "actors_count": 1, "commit": s.json(s.find("c5"), false)})
		}
		out = map[string]any{"data": data, "meta": map[string]any{"next_cursor": nil}}
	case r.Method == "DELETE" && parts[0] == "commits" && len(parts) == 2:
		for i, c := range s.commits {
			if c.ID == parts[1] {
				s.commits = append(s.commits[:i], s.commits[i+1:]...)
				break
			}
		}
		w.WriteHeader(http.StatusNoContent)
		return
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
		counted["created_at"] = s.start.AddDate(-1, 0, 0).Format(time.RFC3339)
		out = map[string]any{
			"data": counted, "is_following": u["username"] != "you", "follows_you": u["username"] == "noor",
			"pasteis_received": 12, "activity": activity(s.start), "streaks": map[string]any{"current": 3, "longest": 12},
			"achievements": achievements(),
		}
	case r.Method == "GET" && parts[0] == "users" && len(parts) == 3 && (parts[2] == "followers" || parts[2] == "following"):
		yes, no := true, false
		var people []any
		for _, u := range []user{noor, sam, mika} {
			p := user{"is_followed": &yes, "follows_you": &no}
			if u["username"] == "noor" {
				p["follows_you"] = &yes
			}
			if u["username"] == "sam" {
				p["is_followed"] = &no
			}
			for k, v := range u {
				p[k] = v
			}
			people = append(people, p)
		}
		out = map[string]any{"data": people, "meta": map[string]any{"next_cursor": nil}}
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
	case parts[0] == "commits" && len(parts) == 3 && (parts[2] == "star" || parts[2] == "pasteis"):
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
		case parts[2] == "pasteis":
			c.Pasteis++
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
	base = "http://" + ln.Addr().String()
	cfg := &config.Config{BaseURL: base, KeyPath: keyPath, Username: "you"}
	if err := cfg.Save(); err != nil {
		log.Fatal(err)
	}

	log.Fatal(http.Serve(ln, newServer()))
}
