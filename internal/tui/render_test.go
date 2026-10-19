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
		`1`:         laneColors[1],
		`"#3a88fe"`: "#3a88fe",
		`9`:         colorAccent,
		`"red"`:     colorAccent,
		``:          colorAccent,
	} {
		if got := laneColor(&api.User{Lane: json.RawMessage(lane)}); got != want {
			t.Errorf("laneColor(%s) = %q, want %q", lane, got, want)
		}
	}
}

func TestSegmentsText(t *testing.T) {
	got := segmentsText([]api.Segment{
		{Type: "text", Text: "run [this] "},
		{Type: "code", Code: "go test"},
		{Type: "block", Language: "go", HTML: `<pre><span class="k">fmt</span>.Println(&quot;hi&quot;)</pre>`},
	}, "")

	for _, want := range []string{"run [this[] ", "[" + colorCode + "]go test[-]", "go[-]\n", `fmt.Println("hi")`} {
		if !strings.Contains(got, want) {
			t.Errorf("segmentsText = %q, want it to contain %q", got, want)
		}
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
	for _, want := range []string{"Alice @alice", "abc1234", "pending review", "hel\n", "Show more", "★ 2", "↩ 1", "Bob @bob", "hi back"} {
		if !strings.Contains(excerpt, want) {
			t.Errorf("excerpt is missing %q:\n%s", want, excerpt)
		}
	}

	full := stripTags(strings.Join(commitLines(c, true, true, 60), "\n"))
	if !strings.Contains(full, "hello") || strings.Contains(full, "Show more") {
		t.Errorf("full commit:\n%s", full)
	}
}

var styleTag = regexp.MustCompile(`\[[a-zA-Z0-9#:\-]*\]`)

func stripTags(s string) string {
	return styleTag.ReplaceAllString(s, "")
}
