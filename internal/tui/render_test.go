package tui

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
)

func TestRelativeTime(t *testing.T) {
	fixed := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return fixed }
	defer func() { now = time.Now }()

	for _, tt := range []struct {
		ago  time.Duration
		want string
	}{
		{10 * time.Second, "now"},
		{41 * time.Minute, "41m"},
		{6 * time.Hour, "6h"},
		{3 * 24 * time.Hour, "3d"},
	} {
		if got := relativeTime(fixed.Add(-tt.ago)); got != tt.want {
			t.Errorf("relativeTime(-%s) = %q, want %q", tt.ago, got, tt.want)
		}
	}

	if got := relativeTime(fixed.AddDate(-1, 0, 0)); !strings.HasSuffix(got, "2025") {
		t.Errorf("a year ago = %q, want the year", got)
	}
}

func TestLaneColor(t *testing.T) {
	for lane, want := range map[string]string{
		`1`:         theme.lanes[1],
		`"#3a88fe"`: "#3a88fe",
		// Too dark to read on the dark theme.
		`"#050505"`: theme.lanes[automaticLane("u1")],
		`9`:         theme.lanes[automaticLane("u1")],
		`"red"`:     theme.lanes[automaticLane("u1")],
		``:          theme.lanes[automaticLane("u1")],
	} {
		if got := laneColor(&api.User{ID: "u1", Lane: json.RawMessage(lane)}); got != want {
			t.Errorf("laneColor(%s) = %q, want %q", lane, got, want)
		}
	}
}

// The web hashes a user's id into one of the six lanes when they pick none: App\Models\User::automaticLane().
func TestAutomaticLaneMatchesTheWeb(t *testing.T) {
	for id, want := range map[string]int{
		"":                                     0,
		"a":                                    97 % 6,
		"019a1b2c-3d4e-7f00-8a9b-0c1d2e3f4a5b": 0,
	} {
		var hash uint64
		for _, r := range id {
			hash = (hash*31 + uint64(r)) & 0xFFFFFFFF
		}
		if id == "019a1b2c-3d4e-7f00-8a9b-0c1d2e3f4a5b" {
			want = int(hash % 6)
		}
		if got := automaticLane(id); got != want {
			t.Errorf("automaticLane(%q) = %d, want %d", id, got, want)
		}
	}
}

func TestBodyLinesHighlightsCode(t *testing.T) {
	lines := bodyLines([]api.Segment{
		{Type: "text", Text: "run [this] with @alice "},
		{Type: "code", Code: "go test"},
		{Type: "block", Language: "go", HTML: `<span class="hl-keyword">func</span> main() { <span class="hl-comment">// &quot;hi&quot;</span> }`},
	}, "", []api.Mention{{Username: "alice"}}, 60)

	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"run [this[] with ",
		"[" + colorPrimary + "]@alice[-:-:-]",
		"[" + colorInk + ":" + colorWash + "] go test [-:-:-]",
		"[" + colorFaint + "]go[-:-:-]",
		"[" + colorPrimary + ":" + colorWash + ":]func[-:-:-]",
		"[" + colorMuted + ":" + colorWash + ":i]// \"hi\"[-:-:-]",
		`// "hi"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("bodyLines = %q, want it to contain %q", got, want)
		}
	}
}

func TestProseSpansLinksAndMentions(t *testing.T) {
	got := tagged(proseSpans("see https://example.com/a, ask @bob or mail me@example.com", map[string]bool{"bob": true}))
	for _, want := range []string{"[" + colorPrimary + "::u]example.com/a[-:-:-][::U][" + colorInk + "], ask ", "[" + colorPrimary + "]@bob[-:-:-]"} {
		if !strings.Contains(got, want) {
			t.Errorf("proseSpans = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "]@example.com") {
		t.Errorf("an email address was taken for a mention: %q", got)
	}
}

func TestWrapSpansKeepsInlineCodeTogether(t *testing.T) {
	lines := wrapSpans([]span{{text: "aaaa bbbb "}, {text: " cc dd ", glue: true}, {text: " eeee"}}, 12)
	for _, l := range lines {
		if w := spansWidth(l); w > 12 {
			t.Errorf("%q is %d wide", tagged(l), w)
		}
	}
	found := false
	for _, l := range lines {
		for _, s := range l {
			if s.text == " cc dd " {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("the inline code was split: %v", lines)
	}

	// A word wider than the line goes in pieces.
	for _, l := range wrapSpans([]span{{text: strings.Repeat("x", 25)}}, 10) {
		if w := spansWidth(l); w > 10 {
			t.Errorf("%d wide", w)
		}
	}
}

func TestCount(t *testing.T) {
	for n, want := range map[int]string{999: "999", 1000: "1k", 1250: "1.2k", 12_345: "12k", 1_250_000: "1.2M"} {
		if got := count(n); got != want {
			t.Errorf("count(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestParseBackground(t *testing.T) {
	for answer, light := range map[string]bool{
		"\x1b]11;rgb:ffff/ffff/ffff\x1b\\": true,
		"\x1b]11;rgb:1e1e/1e1e/1e1e\x07":   false,
		"\x1b]11;rgb:f6/f7/f8\x1b\\":       true,
	} {
		r, g, b, ok := parseBackground([]byte(answer))
		if !ok || (luminance(r, g, b) > 0.5) != light {
			t.Errorf("parseBackground(%q) = %v %v %v %v", answer, r, g, b, ok)
		}
	}
	if pickTheme("auto", []byte("\x1b]11;rgb:ffff/ffff/ffff\x07")).dark {
		t.Error("a white terminal got the dark theme")
	}
	if !pickTheme("dark", []byte("\x1b]11;rgb:ffff/ffff/ffff\x07")).dark {
		t.Error("the dark theme asked for wasn't picked")
	}
}

func TestPreviewSegments(t *testing.T) {
	got := previewSegments("look:\n```go\nfmt.Println(1 < 2)\n```\nand `x`")
	want := []api.Segment{
		{Type: "text", Text: "look:"},
		{Type: "block", Language: "go", HTML: "fmt.Println(1 &lt; 2)"},
		{Type: "text", Text: "and "},
		{Type: "code", Code: "x"},
	}
	if len(got) != len(want) {
		t.Fatalf("previewSegments = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("segment %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestActivityLinesFitTheWidth(t *testing.T) {
	start := time.Date(2025, 10, 19, 0, 0, 0, 0, time.UTC)
	var buckets []api.ActivityBucket
	for d := range 368 {
		buckets = append(buckets, api.ActivityBucket{Date: start.AddDate(0, 0, d).Format("2006-01-02"), Count: d % 5})
	}

	for _, width := range []int{30, 53, 80} {
		for _, l := range activityLines(&api.Activity{Scale: "year", Buckets: buckets}, theme.lanes[0], width) {
			if w := tview.TaggedStringWidth(l); w > width {
				t.Errorf("at %d wide, %q is %d wide", width, l, w)
			}
		}
	}

	bars := activityLines(&api.Activity{Scale: "week", Buckets: buckets[:7]}, theme.lanes[0], 40)
	if !strings.Contains(stripTags(bars[len(bars)-1]), "commits in the last 7 days") {
		t.Errorf("week: %q", bars)
	}
}

func TestWrapKeepsLinesWithinWidth(t *testing.T) {
	lines := wrap(strings.Repeat("意味ない ", 10)+"\n\nend", "[red]│[-] ", 20)

	for _, line := range lines {
		if w := tview.TaggedStringWidth(line); w > 20 {
			t.Errorf("%q is %d wide", line, w)
		}
	}
	if lines[len(lines)-2] != "[red]│[-] " || !strings.HasSuffix(lines[len(lines)-1], "end") {
		t.Errorf("blank line or last paragraph lost: %q", lines)
	}
}

func TestCommitLines(t *testing.T) {
	parent := "p"
	c := &api.Commit{
		ID: "c", ShortHash: "abc1234", Body: "hello", Status: "pending", StarsCount: 2, Starred: true, RepliesCount: 1,
		ParentID:        &parent,
		Author:          &api.User{Name: "Alice", Username: "alice"},
		PreviewSegments: []api.Segment{{Type: "text", Text: "hel"}},
		BodySegments:    []api.Segment{{Type: "text", Text: "hello"}},
		ReplyPreview:    &api.Page[api.Commit]{Data: []api.Commit{{Body: "hi back", Author: &api.User{Name: "Bob", Username: "bob"}}}},
	}

	excerpt := stripTags(strings.Join(commitLines(c, false, false, 60), "\n"))
	for _, want := range []string{"Alice @alice", "abc1234", "pending review", "hel\n", "Show more", "★ 2", "↩ 1", "Bob @bob", "hi back", "◌"} {
		if !strings.Contains(excerpt, want) {
			t.Errorf("excerpt is missing %q:\n%s", want, excerpt)
		}
	}

	full := stripTags(strings.Join(commitLines(c, true, true, 60), "\n"))
	if !strings.Contains(full, "hello") || strings.Contains(full, "Show more") {
		t.Errorf("full commit:\n%s", full)
	}
}

func TestNotificationLines(t *testing.T) {
	me := &api.User{Name: "Alice", Username: "alice"}
	for typ, want := range map[string]string{
		"starred":    "Alice starred your commit",
		"leveled_up": "You reached a new level",
	} {
		got := stripTags(strings.Join(notificationLines(&api.Notification{Type: typ, Actor: me}, false, 60), "\n"))
		if !strings.Contains(got, want) || typ == "leveled_up" && strings.Contains(got, "Alice") {
			t.Errorf("%s notification:\n%s", typ, got)
		}
	}
}

var styleTag = regexp.MustCompile(`\[[a-zA-Z0-9#:\-]*\]`)

func TestCommitLinesFitTheWidth(t *testing.T) {
	pct := 55
	c := &api.Commit{
		ID: "c", ShortHash: "abc1234", Status: "approved", Author: &api.User{Name: "A very long name indeed", Username: "someone-with-a-long-name"},
		BodySegments: []api.Segment{{Type: "text", Text: strings.Repeat("word ", 40)}, {Type: "block", Language: "go", HTML: strings.Repeat("x", 120)}},
		Poll:         &api.Poll{Options: []api.PollOption{{Label: "Tabs", Voted: true, Percentage: &pct}}},
		LinkPreview:  &api.LinkPreview{URL: "https://example.com", Title: strings.Repeat("title ", 20)},
		QuotedCommit: &api.Commit{ShortHash: "def5678", Body: strings.Repeat("quoted ", 30), Author: &api.User{Name: "Q", Username: "q"}},
	}

	for _, width := range []int{30, 60, 97} {
		for _, selected := range []bool{false, true} {
			for _, l := range commitLines(c, selected, true, width) {
				if w := tview.TaggedStringWidth(l); w > width {
					t.Errorf("at %d wide, %q is %d wide", width, l, w)
				}
			}
		}
	}
}

func stripTags(s string) string {
	return styleTag.ReplaceAllString(s, "")
}
