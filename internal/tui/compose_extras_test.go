package tui

import (
	"encoding/json"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// queueAPI takes commits with polls and scheduled commits, and lists the queue.
type queueAPI struct {
	mu        sync.Mutex
	commits   []api.NewCommit
	scheduled []api.NewScheduledCommit
	canceled  []string
	submitted []string
}

func (f *queueAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	me := map[string]any{"id": "u0", "handle": "me", "name": "Me", "lane": 1}
	var out any
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/me":
		out = map[string]any{"data": me}
	case "POST /api/v1/commits":
		var in api.NewCommit
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.commits = append(f.commits, in)
		w.WriteHeader(http.StatusCreated)
		out = map[string]any{"data": map[string]any{"id": "c9", "short_hash": "9999999", "body": in.Body, "status": "approved", "author": me}}
	case "POST /api/v1/scheduled-commits":
		var in api.NewScheduledCommit
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.scheduled = append(f.scheduled, in)
		w.WriteHeader(http.StatusCreated)
		out = map[string]any{"data": map[string]any{"id": "s1", "body": in.Body, "scheduled_at": in.ScheduledAt, "status": "scheduled"}}
	case "GET /api/v1/scheduled-commits":
		var data []any
		for _, id := range []string{"s1", "s2"} {
			gone := false
			for _, c := range f.canceled {
				gone = gone || c == id
			}
			if gone {
				continue
			}
			body := "queued " + id
			data = append(data, map[string]any{
				"id": id, "body": body, "scheduled_at": time.Now().Add(2 * time.Hour).Format(time.RFC3339), "status": "scheduled",
				"poll_options": []string{"yes", "no"},
			})
		}
		out = map[string]any{"data": data, "meta": map[string]any{"current_page": 1, "last_page": 1}}
	case "DELETE /api/v1/scheduled-commits/s2":
		f.canceled = append(f.canceled, "s2")
		w.WriteHeader(http.StatusNoContent)
		return
	case "POST /api/v1/scheduled-commits/s1/submission":
		f.submitted = append(f.submitted, "s1")
		out = map[string]any{"data": map[string]any{"id": "s1", "scheduled_at": time.Now().Format(time.RFC3339), "status": "pending"}}
	default:
		out = map[string]any{"data": []any{}, "meta": map[string]any{"next_cursor": nil}}
	}

	_ = json.NewEncoder(w).Encode(out)
}

func TestCommitsWithAPoll(t *testing.T) {
	f := &queueAPI{}
	h := start(t, f)
	h.waitFor("Following")

	h.typeText("c")
	h.waitFor("New commit")
	h.typeText("tabs or spaces?")
	h.key(tcell.KeyCtrlL)
	h.waitFor("Option 1")
	h.typeText("Tabs")
	h.key(tcell.KeyTab)
	h.typeText("Spaces")
	// Past the empty options and the duration, to Save.
	for range 4 {
		h.key(tcell.KeyTab)
	}
	h.key(tcell.KeyEnter)
	h.waitFor("▤ poll: Tabs / Spaces · 1 day")

	h.key(tcell.KeyCtrlS)
	h.waitFor("[log 9999999] tabs or spaces?")

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.commits) != 1 || f.commits[0].Poll == nil || len(f.commits[0].Poll.Options) != 2 || f.commits[0].Poll.Duration != 1440 {
		t.Errorf("sent %+v", f.commits)
	}
}

func TestSchedulesACommit(t *testing.T) {
	f := &queueAPI{}
	h := start(t, f)
	h.waitFor("Following")

	h.typeText("c")
	h.waitFor("New commit")
	h.typeText("good morning")
	h.key(tcell.KeyCtrlT)
	h.waitFor("Commit it later")
	h.typeText("+2h")
	h.key(tcell.KeyEnter)
	h.waitFor("◷ goes out")

	h.key(tcell.KeyCtrlS)
	h.waitFor("Scheduled for")

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.commits) != 0 || len(f.scheduled) != 1 || f.scheduled[0].Body != "good morning" {
		t.Fatalf("committed %+v, scheduled %+v", f.commits, f.scheduled)
	}
	at, err := time.Parse(time.RFC3339, f.scheduled[0].ScheduledAt)
	if err != nil || at.Sub(time.Now()) < 110*time.Minute || at.Sub(time.Now()) > 2*time.Hour {
		t.Errorf("scheduled at %q", f.scheduled[0].ScheduledAt)
	}
}

func TestARepliesCannotBeScheduled(t *testing.T) {
	h := start(t, &fakeAPI{})
	h.waitFor("abc1234")

	h.typeText("r")
	h.waitFor("Reply")
	h.key(tcell.KeyCtrlT)
	h.waitFor("Only a new commit can be scheduled")
}

func TestCancelsAndCommitsScheduledCommits(t *testing.T) {
	f := &queueAPI{}
	h := start(t, f)
	h.waitFor("Following")

	h.typeText("gq")
	h.waitFor("queued s1")
	h.waitFor("▤ poll: [yes no]")

	h.typeText("j")
	h.typeText("D")
	h.waitFor("never goes out")
	h.key(tcell.KeyRight)
	h.key(tcell.KeyEnter)
	h.waitFor("Canceled it.")
	h.waitForGone("queued s2")

	h.typeText("k")
	h.key(tcell.KeyEnter)
	h.waitFor("Commit it now")
	h.key(tcell.KeyRight)
	h.key(tcell.KeyEnter)
	h.waitFor("Committed it now.")

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.canceled) != 1 || len(f.submitted) != 1 {
		t.Errorf("canceled %v, submitted %v", f.canceled, f.submitted)
	}
}

func TestRepliesToWhomTheParentWasAddressed(t *testing.T) {
	f := &fakeAPI{}
	h := start(t, f)
	h.waitFor("abc1234")

	h.typeText("r")
	h.waitFor("replying to @alice @bob · ^n choose")
	h.typeText("first")
	h.key(tcell.KeyCtrlS)
	h.waitFor("[log 9999999] first")

	h.typeText("r")
	h.waitFor("replying to @alice @bob")
	h.key(tcell.KeyCtrlN)
	h.waitFor("Who else does the reply")
	h.key(tcell.KeyEnter)
	h.waitFor("· @bob")
	h.key(tcell.KeyRight)
	h.key(tcell.KeyEnter)
	h.waitFor("replying to @alice · ^n choose")
	h.typeText("second")
	h.key(tcell.KeyCtrlS)
	h.waitFor("[log 9999999] second")

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.drafts) != 2 {
		t.Fatalf("sent %+v", f.drafts)
	}
	if !slices.Equal(f.drafts[0].ReplyRecipients, []string{"u5"}) || len(f.drafts[1].ReplyRecipients) != 0 {
		t.Errorf("sent to %v, then %v", f.drafts[0].ReplyRecipients, f.drafts[1].ReplyRecipients)
	}
}
