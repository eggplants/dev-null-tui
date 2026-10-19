package tui

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// laneColors are the web's six lane colors in its dark theme.
var laneColors = []string{"#ff8a4c", "#51cf66", "#da77f2", "#3bc9db", "#f06595", "#fcc419"}

var hexColor = regexp.MustCompile(`^#[0-9a-f]{6}$`)

const (
	colorMuted  = "gray"
	colorCode   = "#e5c07b"
	colorLink   = "#74c0fc"
	colorStar   = "#fcc419"
	colorAccent = "#51cf66"
)

// now is replaced in tests.
var now = time.Now

func laneColor(u *api.User) string {
	if u == nil || len(u.Lane) == 0 {
		return colorAccent
	}

	var lane int
	if json.Unmarshal(u.Lane, &lane) == nil && lane >= 0 && lane < len(laneColors) {
		return laneColors[lane]
	}

	var custom string
	if json.Unmarshal(u.Lane, &custom) == nil && hexColor.MatchString(custom) {
		return custom
	}

	return colorAccent
}

// relativeTime says how long ago t was the way the web does: 41m, 6h, 3d, then the date.
func relativeTime(t time.Time) string {
	d := now().Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case t.Year() == now().Year():
		return t.Local().Format("Jan 2")
	default:
		return t.Local().Format("Jan 2, 2006")
	}
}

var htmlTag = regexp.MustCompile(`<[^>]*>`)

// segmentsText turns a commit body's segments into tagged text, with code colored.
func segmentsText(segments []api.Segment, body string) string {
	if len(segments) == 0 {
		return tview.Escape(body)
	}

	var b strings.Builder
	for _, s := range segments {
		switch s.Type {
		case "code":
			fmt.Fprintf(&b, "[%s]%s[-]", colorCode, tview.Escape(s.Code))
		case "block":
			code := html.UnescapeString(htmlTag.ReplaceAllString(s.HTML, ""))
			if !strings.HasSuffix(b.String(), "\n") && b.Len() > 0 {
				b.WriteString("\n")
			}
			if s.Language != "" {
				fmt.Fprintf(&b, "[%s]%s[-]\n", colorMuted, tview.Escape(s.Language))
			}
			fmt.Fprintf(&b, "[%s]%s[-]", colorCode, tview.Escape(strings.TrimRight(code, "\n")))
		default:
			b.WriteString(tview.Escape(s.Text))
		}
	}

	return b.String()
}

// wrap word-wraps tagged text to width, prefixing every line.
func wrap(text, prefix string, width int) []string {
	width -= tview.TaggedStringWidth(prefix)
	if width < 1 {
		width = 1
	}

	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		if paragraph == "" {
			lines = append(lines, prefix)
			continue
		}
		for _, line := range tview.WordWrap(paragraph, width) {
			lines = append(lines, prefix+line)
		}
	}

	return lines
}

// truncateLines keeps the first n lines, ending the last with an ellipsis when it cut any.
func truncateLines(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}

	lines = lines[:n]
	lines[n-1] += "[" + colorMuted + "]…[-]"

	return lines
}

func authorLine(c *api.Commit) string {
	a := c.Author
	if a == nil {
		return fmt.Sprintf("[%s]unknown[-]", colorMuted)
	}

	line := fmt.Sprintf("[%s::b]%s[-::-] [%s]@%s · %s · %s[-]",
		laneColor(a), tview.Escape(a.Name), colorMuted, tview.Escape(a.Username), relativeTime(c.CreatedAt), c.ShortHash)

	switch {
	case c.DeletedAt != nil:
		line += fmt.Sprintf(" [%s](deleted)[-]", colorMuted)
	case c.Status == "pending":
		line += " [yellow]pending review[-]"
	case c.Status == "rejected":
		line += " [red]rejected[-]"
	}
	if c.IsPinned {
		line += " [yellow]pinned[-]"
	}

	return line
}

// commitLines renders a commit for a list. full shows the whole body even when the API sent an excerpt.
func commitLines(c *api.Commit, selected, full bool, width int) []string {
	color := laneColor(c.Author)
	gutter := fmt.Sprintf("[%s]│[-] ", color)
	if selected {
		gutter = fmt.Sprintf("[%s::b]┃[-::-] ", color)
	}

	lines := wrap(authorLine(c), gutter, width)

	segments := c.BodySegments
	if !full && c.PreviewSegments != nil {
		segments = c.PreviewSegments
	}
	if c.DeletedAt == nil {
		lines = append(lines, wrap(segmentsText(segments, c.Body), gutter, width)...)
		if !full && c.PreviewSegments != nil {
			lines = append(lines, gutter+fmt.Sprintf("[%s]Show more ⏎[-]", colorLink))
		}
	}

	lines = append(lines, attachmentLines(c, gutter, width)...)
	lines = append(lines, gutter+countsLine(c))

	if c.ReplyPreview != nil {
		for _, reply := range c.ReplyPreview.Data {
			lines = append(lines, replyPreviewLines(&reply, gutter, width)...)
		}
	}

	return append(lines, "")
}

func attachmentLines(c *api.Commit, gutter string, width int) []string {
	var lines []string

	if p := c.Poll; p != nil {
		for _, o := range p.Options {
			mark := "○"
			if o.Voted {
				mark = "●"
			}
			line := fmt.Sprintf("%s %s", mark, tview.Escape(o.Label))
			if o.Percentage != nil {
				line += fmt.Sprintf(" [%s]%d%%[-]", colorMuted, *o.Percentage)
			}
			lines = append(lines, wrap(line, gutter+"  ", width)...)
		}
		state := "open"
		if p.IsClosed {
			state = "closed"
		} else if p.ClosesAt != nil {
			state = "closes " + p.ClosesAt.Local().Format("Jan 2 15:04")
		}
		votes := ""
		if p.TotalVotes != nil {
			votes = fmt.Sprintf("%d votes · ", *p.TotalVotes)
		}
		lines = append(lines, gutter+fmt.Sprintf("  [%s]%s%s[-]", colorMuted, votes, state))
	}

	if n := len(c.Images); n > 0 {
		label := "image"
		if n > 1 {
			label = fmt.Sprintf("%d images", n)
		}
		lines = append(lines, gutter+fmt.Sprintf("[%s][%s][-]", colorMuted, label))
	}

	if lp := c.LinkPreview; lp != nil {
		title := lp.Title
		if title == "" {
			title = lp.URL
		}
		line := fmt.Sprintf("[%s]↗ %s[-]", colorLink, tview.Escape(title))
		if lp.SiteName != "" {
			line += fmt.Sprintf(" [%s]%s[-]", colorMuted, tview.Escape(lp.SiteName))
		}
		lines = append(lines, truncateLines(wrap(line, gutter, width), 2)...)
	}

	if q := c.QuotedCommit; q != nil {
		quote := gutter + fmt.Sprintf("[%s]▏[-] ", colorMuted)
		lines = append(lines, wrap(authorLine(q), quote, width)...)
		lines = append(lines, truncateLines(wrap(segmentsText(q.BodySegments, q.Body), quote, width), 4)...)
	} else if c.QuotedCommitID != nil {
		lines = append(lines, gutter+fmt.Sprintf("[%s]▏ This commit is unavailable.[-]", colorMuted))
	}

	return lines
}

func countsLine(c *api.Commit) string {
	star := fmt.Sprintf("[%s]☆ %d[-]", colorMuted, c.StarsCount)
	if c.Starred {
		star = fmt.Sprintf("[%s]★ %d[-]", colorStar, c.StarsCount)
	}

	line := fmt.Sprintf("[%s]↩ %d[-]  %s", colorMuted, c.RepliesCount, star)
	if c.PasteisCount > 0 {
		line += fmt.Sprintf("  [%s]pastéis %d[-]", colorMuted, c.PasteisCount)
	}
	if c.IsSaved {
		line += fmt.Sprintf("  [%s]saved[-]", colorLink)
	}

	return line
}

func replyPreviewLines(r *api.Commit, gutter string, width int) []string {
	name := "unknown"
	if r.Author != nil {
		name = fmt.Sprintf("[%s::b]%s[-::-] [%s]@%s · %s[-]",
			laneColor(r.Author), tview.Escape(r.Author.Name), colorMuted, tview.Escape(r.Author.Username), relativeTime(r.CreatedAt))
	}

	indent := gutter + fmt.Sprintf("[%s]↳[-] ", colorMuted)
	lines := wrap(name, indent, width)
	body := strings.ReplaceAll(segmentsText(r.BodySegments, r.Body), "\n", " ")

	return append(lines, truncateLines(wrap(body, gutter+"  ", width), 2)...)
}

var notificationVerbs = map[string]string{
	"followed":  "followed you",
	"replied":   "replied to your commit",
	"mentioned": "mentioned you",
	"quoted":    "quoted your commit",
	"starred":   "starred your commit",
	"approved":  "Your commit was approved",
	"rejected":  "Your commit was rejected",
}

func notificationLines(n *api.Notification, selected bool, width int) []string {
	color := laneColor(n.Actor)
	gutter := fmt.Sprintf("[%s]│[-] ", color)
	if selected {
		gutter = fmt.Sprintf("[%s::b]┃[-::-] ", color)
	}

	verb := notificationVerbs[n.Type]
	if verb == "" {
		verb = n.Type
	}

	var head string
	switch n.Type {
	case "approved", "rejected":
		head = verb
		if n.Reason != nil && *n.Reason != "" {
			head += ": " + tview.Escape(*n.Reason)
		}
	default:
		actor := "someone"
		if n.Actor != nil {
			actor = fmt.Sprintf("[%s::b]%s[-::-]", color, tview.Escape(n.Actor.Name))
		}
		if others := n.ActorsCount - 1; others > 0 {
			actor += fmt.Sprintf(" and %d other", others)
			if others > 1 {
				actor += "s"
			}
		}
		head = actor + " " + verb
	}

	head += fmt.Sprintf(" [%s]· %s[-]", colorMuted, relativeTime(n.CreatedAt))
	if n.UnreadCount > 0 {
		head = fmt.Sprintf("[%s]●[-] ", colorAccent) + head
	}

	lines := wrap(head, gutter, width)
	if c := n.Commit; c != nil {
		body := strings.ReplaceAll(segmentsText(c.BodySegments, c.Body), "\n", " ")
		lines = append(lines, truncateLines(wrap(fmt.Sprintf("[%s]%s[-]", colorMuted, body), gutter, width), 3)...)
	}

	return append(lines, "")
}

func profileLines(p *api.Profile, width int) []string {
	u := &p.Data
	color := laneColor(u)
	gutter := fmt.Sprintf("[%s]█[-] ", color)

	lines := wrap(fmt.Sprintf("[%s::b]%s[-::-] [%s]@%s[-]", color, tview.Escape(u.Name), colorMuted, tview.Escape(u.Username)), gutter, width)
	if u.Bio != "" {
		lines = append(lines, wrap(tview.Escape(u.Bio), gutter, width)...)
	}

	var facts []string
	for _, f := range []string{u.Company, u.Location, u.Website} {
		if f != "" {
			facts = append(facts, tview.Escape(f))
		}
	}
	if len(facts) > 0 {
		lines = append(lines, wrap(fmt.Sprintf("[%s]%s[-]", colorMuted, strings.Join(facts, " · ")), gutter, width)...)
	}

	var counts []string
	for _, c := range []struct {
		n     *int
		label string
	}{{u.CommitsCount, "commits"}, {u.FollowersCount, "followers"}, {u.FollowingCount, "following"}} {
		if c.n != nil {
			counts = append(counts, fmt.Sprintf("[::b]%d[::-] %s", *c.n, c.label))
		}
	}
	if p.PasteisReceived > 0 {
		counts = append(counts, fmt.Sprintf("[::b]%d[::-] pastéis", p.PasteisReceived))
	}
	if p.FollowsYou {
		counts = append(counts, fmt.Sprintf("[%s]follows you[-]", colorMuted))
	}
	if p.IsFollowing {
		counts = append(counts, fmt.Sprintf("[%s]following[-]", colorAccent))
	}
	if len(counts) > 0 {
		lines = append(lines, wrap(strings.Join(counts, "  "), gutter, width)...)
	}

	return append(lines, strings.Repeat("─", width), "")
}
