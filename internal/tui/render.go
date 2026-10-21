package tui

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
)

var hexColor = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// now is replaced in tests.
var now = time.Now

// showPasteis says whether the signed-in user is in pastéis mode: like the web, only then are pastéis counted.
var showPasteis bool

// automaticLane is the lane the web gives someone who picked none, from their id.
func automaticLane(id string) int {
	var hash uint32
	for _, r := range id {
		hash = hash*31 + uint32(r)
	}

	return int(hash % 6)
}

// laneColor is the color a user's commits are drawn in: their lane, or their own color when it can be read on the
// theme, or else the lane the web would give them.
func laneColor(u *api.User) string {
	if u == nil {
		return colorRail
	}

	var lane int
	if json.Unmarshal(u.Lane, &lane) == nil && lane >= 0 && lane < len(theme.lanes) {
		return theme.lanes[lane]
	}

	var custom string
	if json.Unmarshal(u.Lane, &custom) == nil && hexColor.MatchString(custom) && readable(custom) {
		return custom
	}

	return theme.lanes[automaticLane(u.ID)]
}

// relativeTime says how long ago t was the way the web does: 41m, 6h, 3d, then the date.
func relativeTime(t time.Time) string {
	d := now().Sub(t)
	switch {
	case t.IsZero():
		return "?"
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

// absoluteTime is when t was, as an open commit says it.
func absoluteTime(t time.Time) string {
	return t.Local().Format("15:04 · Mon 2 Jan 2006")
}

// dayLabel names the day t falls on, for the separators of a log read in time order.
func dayLabel(t time.Time) string {
	t = t.Local()
	today := now().Local()
	day := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local) }

	switch days := int(math.Round(day(today).Sub(day(t)).Hours() / 24)); {
	case days <= 0:
		return "Today"
	case days == 1:
		return "Yesterday"
	case days < 7:
		return t.Format("Monday")
	case t.Year() == today.Year():
		return t.Format("Mon, Jan 2")
	default:
		return t.Format("Mon, Jan 2, 2006")
	}
}

// count abbreviates a count the way the web does: 999, 1.2k, 12k, 1.2M.
func count(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprint(n)
	case n < 10_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", math.Floor(float64(n)/100)/10), ".0") + "k"
	case n < 1_000_000:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", math.Floor(float64(n)/100_000)/10), ".0") + "M"
	}
}

// wrap word-wraps tagged text to width, prefixing every line.
func wrap(text, prefix string, width int) []string {
	return wrapIndent(text, prefix, prefix, width)
}

// wrapIndent word-wraps tagged text to width, prefixing the first line with first and the rest with rest.
func wrapIndent(text, first, rest string, width int) []string {
	width -= tview.TaggedStringWidth(first)
	if width < 1 {
		width = 1
	}

	var lines []string
	prefix := first
	for _, paragraph := range strings.Split(text, "\n") {
		if paragraph == "" {
			lines = append(lines, prefix)
			prefix = rest
			continue
		}
		for _, line := range tview.WordWrap(paragraph, width) {
			lines = append(lines, prefix+line)
			prefix = rest
		}
	}

	return lines
}

// prefixed puts prefix before each line.
func prefixed(prefix string, lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = prefix + l
	}

	return out
}

// truncateLines keeps the first n lines, ending the last with an ellipsis when it cut any.
func truncateLines(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}

	lines = lines[:n:n]
	lines[n-1] += "[" + colorMuted + "]…[-]"

	return lines
}

// foldLines keeps the first n lines and says how many more there are, as a long commit folds in a list.
func foldLines(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}

	more := len(lines) - n
	return append(lines[:n:n], fmt.Sprintf("[%s]… %d more line%s · ⏎ to read[-]", colorPrimary, more, plural(more)))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}

	return "s"
}

// pad fills tagged text with spaces to width.
func pad(text string, width int) string {
	return text + strings.Repeat(" ", max(0, width-tview.TaggedStringWidth(text)))
}

// spread puts left and right on one line of width, right against the edge, or right after left when they don't fit.
func spread(left, right string, width int) string {
	gap := width - tview.TaggedStringWidth(left) - tview.TaggedStringWidth(right)
	if gap < 2 {
		return left + " " + right
	}

	return left + strings.Repeat(" ", gap) + right
}

// boxed draws a rounded box of width around tagged lines, which must already fit width - 4.
func boxed(lines []string, width int) []string {
	border := colorRail
	inner := width - 4

	out := []string{fmt.Sprintf("[%s]╭%s╮[-]", border, strings.Repeat("─", inner+2))}
	for _, l := range lines {
		out = append(out, fmt.Sprintf("[%s]│[-] %s [%s]│[-]", border, pad(l, inner), border))
	}

	return append(out, fmt.Sprintf("[%s]╰%s╯[-]", border, strings.Repeat("─", inner+2)))
}

// The commit graph is drawn in three columns left of each line: the selection bar, the rail, and a space.
const gutterWidth = 3

// gutter is what goes left of a commit's lines: the selection bar when it's selected, and its rail glyph.
func gutter(selected bool, rail, color string) string {
	bar := " "
	if selected {
		bar = fmt.Sprintf("[%s]▌[-]", colorPrimary)
	}

	return fmt.Sprintf("%s[%s]%s[-] ", bar, color, rail)
}

// separator is the line under every item of a list, where a commit's rail runs on to the next one. listView leaves
// it off a selected item's wash.
func separator(color string) string {
	if color == "" {
		return ""
	}

	return fmt.Sprintf(" [%s]│[-]", color)
}

func authorName(u *api.User) string {
	if u == nil {
		return fmt.Sprintf("[%s]unknown[-]", colorMuted)
	}

	name := u.Name
	if name == "" {
		name = u.Username
	}

	// Lanes are identity on the web: they color the graph, never the text.
	return fmt.Sprintf("[%s::b]%s[-::-] [%s]@%s[-]", colorInk, tview.Escape(name), colorMuted, tview.Escape(u.Username))
}

// authorLine is a commit's head: who and when, and its review status.
func authorLine(c *api.Commit) string {
	line := authorName(c.Author)
	if !c.CreatedAt.IsZero() {
		line += fmt.Sprintf(" [%s]· %s[-]", colorMuted, relativeTime(c.CreatedAt))
	}

	switch c.Status {
	case "pending":
		line += fmt.Sprintf(" [%s]· pending review[-]", colorWarning)
	case "rejected":
		line += fmt.Sprintf(" [%s]· rejected[-]", colorDanger)
	}
	if c.IsPinned {
		line += fmt.Sprintf(" [%s]· ◇ pinned[-]", colorMuted)
	}

	return line
}

func hash(c *api.Commit) string {
	return fmt.Sprintf("[%s]%s[-]", colorFaint, c.ShortHash)
}

// commitLines renders a commit for a list as a node on its author's rail. full shows the whole body and when it was
// made, as the commit a thread is about does.
func commitLines(c *api.Commit, selected, full bool, width int) []string {
	color := laneColor(c.Author)
	inner := max(width-gutterWidth, 1)
	rail := gutter(selected, "│", color)

	if c.DeletedAt != nil || c.Author == nil && c.Body == "" {
		lines := []string{gutter(selected, "○", colorRail) + fmt.Sprintf("[%s::i]This commit was deleted.[-::-]", colorFaint)}
		if c.RepliesCount > 0 {
			lines = append(lines, gutter(selected, " ", colorRail)+fmt.Sprintf("[%s]↩ %s[-]", colorMuted, count(c.RepliesCount)))
		}
		return append(lines, separator(colorRail))
	}

	node := "●"
	if c.Status == "pending" || c.Status == "rejected" {
		node = "◌"
	}
	lines := wrapIndent(spread(authorLine(c), hash(c), inner), gutter(selected, node, color), rail, width)

	segments := c.BodySegments
	excerpt := !full && c.PreviewSegments != nil
	if excerpt {
		segments = c.PreviewSegments
	}
	body := bodyLines(segments, c.Body, c.Mentions, inner)
	switch {
	case excerpt:
		body = append(body, fmt.Sprintf("[%s]Show more ⏎[-]", colorPrimary))
	case !full:
		body = foldLines(body, 10)
	}
	lines = append(lines, prefixed(rail, body)...)
	lines = append(lines, prefixed(rail, attachmentLines(c, inner))...)
	lines = append(lines, rail+countsLine(c))
	if full && !c.CreatedAt.IsZero() {
		lines = append(lines, rail+fmt.Sprintf("[%s]%s[-]", colorFaint, absoluteTime(c.CreatedAt)))
	}

	if c.ReplyPreview != nil {
		for i := range c.ReplyPreview.Data {
			lines = append(lines, replyPreviewLines(&c.ReplyPreview.Data[i], selected, color, width)...)
		}
		if more := c.RepliesCount - len(c.ReplyPreview.Data); more > 0 && len(c.ReplyPreview.Data) > 0 {
			label := fmt.Sprintf("%s more replies", count(more))
			if more == 1 {
				label = "1 more reply"
			}
			lines = append(lines, rail+fmt.Sprintf("[%s]  %s · ⏎[-]", colorMuted, label))
		}
	}

	return append(lines, separator(color))
}

// attachmentLines renders what a commit carries besides its body: a poll, images, a link card and a quote.
func attachmentLines(c *api.Commit, width int) []string {
	var lines []string

	if p := c.Poll; p != nil {
		lines = append(lines, boxed(pollLines(p, width-4), width)...)
	}

	if n := len(c.Images); n > 0 {
		label := "image"
		if n > 1 {
			label = fmt.Sprintf("%d images", n)
		}
		if img := c.Images[0]; img.Width > 0 && img.Height > 0 {
			label += fmt.Sprintf(" · %d×%d", img.Width, img.Height)
		}
		lines = append(lines, fmt.Sprintf("[%s]▣ %s · i to view[-]", colorMuted, label))
	}

	if lp := c.LinkPreview; lp != nil {
		lines = append(lines, boxed(linkPreviewLines(lp, width-4), width)...)
	}

	if q := c.QuotedCommit; q != nil {
		inner := width - 4
		quote := wrap(spread(authorLine(q), hash(q), inner), "", inner)
		quote = append(quote, truncateLines(bodyLines(q.BodySegments, q.Body, q.Mentions, inner), 4)...)
		if n := len(q.Images); n > 0 {
			quote = append(quote, fmt.Sprintf("[%s]▣ %d image%s[-]", colorMuted, n, plural(n)))
		}
		lines = append(lines, boxed(quote, width)...)
	} else if c.QuotedCommitID != nil {
		lines = append(lines, boxed([]string{fmt.Sprintf("[%s::i]This commit is unavailable.[-::-]", colorFaint)}, width)...)
	}

	return lines
}

func pollLines(p *api.Poll, width int) []string {
	var lines []string

	barWidth := min(20, max(width/3, 5))
	for _, o := range p.Options {
		mark := fmt.Sprintf("[%s]○[-]", colorMuted)
		if o.Voted {
			mark = fmt.Sprintf("[%s]✓[-]", colorPrimary)
		}
		label := mark + " " + tview.Escape(o.Label)
		if o.Percentage == nil {
			lines = append(lines, wrap(label, "", width)...)
			continue
		}

		filled := *o.Percentage * barWidth / 100
		barColor := colorRail
		if o.Voted {
			barColor = colorPrimary
		}
		bar := fmt.Sprintf("[%s]%s[%s]%s[-] [%s]%3d%%[-]", barColor, strings.Repeat("█", filled), colorWash, strings.Repeat("█", barWidth-filled), colorMuted, *o.Percentage)
		lines = append(lines, spread(label, bar, width))
	}

	var facts []string
	if p.TotalVotes != nil {
		facts = append(facts, fmt.Sprintf("%s vote%s", count(*p.TotalVotes), plural(*p.TotalVotes)))
	}
	switch {
	case p.IsClosed:
		facts = append(facts, "closed")
	case p.ClosesAt != nil:
		facts = append(facts, "closes "+p.ClosesAt.Local().Format("Jan 2 15:04"))
	}
	if !p.IsClosed && !p.HasVoted {
		facts = append(facts, "vote on the web")
	}

	return append(lines, wrap(fmt.Sprintf("[%s]%s[-]", colorMuted, strings.Join(facts, " · ")), "", width)...)
}

func linkPreviewLines(lp *api.LinkPreview, width int) []string {
	title := lp.Title
	if title == "" {
		title = lp.URL
	}
	lines := truncateLines(wrap(fmt.Sprintf("[%s::b]↗ %s[-::-]", colorInk, tview.Escape(title)), "", width), 2)

	if lp.Description != "" {
		lines = append(lines, truncateLines(wrap(fmt.Sprintf("[%s]%s[-]", colorMuted, tview.Escape(lp.Description)), "", width), 2)...)
	}

	site := lp.SiteName
	if u, err := url.Parse(lp.URL); err == nil && u.Host != "" {
		host := strings.TrimPrefix(u.Host, "www.")
		if site == "" {
			site = host
		} else if !strings.EqualFold(site, host) {
			site += " · " + host
		}
	}
	if site != "" {
		lines = append(lines, truncateLines(wrap(fmt.Sprintf("[%s]%s[-]", colorFaint, tview.Escape(site)), "", width), 1)...)
	}

	return lines
}

// countsLine is the row under a commit: its replies, its stars, starred in Commit Blue, and its pastéis in pastéis
// mode.
func countsLine(c *api.Commit) string {
	star := fmt.Sprintf("[%s]☆ %s[-]", colorMuted, count(c.StarsCount))
	if c.Starred {
		star = fmt.Sprintf("[%s]★ %s[-]", colorPrimary, count(c.StarsCount))
	}

	line := fmt.Sprintf("[%s]↩ %s[-]   %s", colorMuted, count(c.RepliesCount), star)
	if showPasteis && c.PasteisCount > 0 {
		line += fmt.Sprintf("   [%s]◍ %s[-]", theme.lanes[5], count(c.PasteisCount))
	}

	return line
}

// replyPreviewLines draws a reply under its commit, branching off the commit's rail.
func replyPreviewLines(r *api.Commit, selected bool, color string, width int) []string {
	bar := " "
	if selected {
		bar = fmt.Sprintf("[%s]▌[-]", colorPrimary)
	}
	branch := fmt.Sprintf("%s[%s]├─[%s]●[-] ", bar, color, laneColor(r.Author))
	rest := fmt.Sprintf("%s[%s]│[-]   ", bar, color)
	inner := max(width-5, 1)

	if r.DeletedAt != nil {
		return []string{branch + fmt.Sprintf("[%s::i]This reply was deleted.[-::-]", colorFaint)}
	}

	head := authorName(r.Author) + fmt.Sprintf(" [%s]· %s[-]", colorMuted, relativeTime(r.CreatedAt))
	lines := wrapIndent(head, branch, rest, width)

	body := bodyLines(r.BodySegments, r.Body, r.Mentions, inner)
	return append(lines, prefixed(rest, truncateLines(body, 2))...)
}

var notificationVerbs = map[string]string{
	"followed":  "followed you",
	"replied":   "replied to your commit",
	"mentioned": "mentioned you",
	"quoted":    "quoted your commit",
	"starred":   "starred your commit",
	"approved":  "Your commit was approved",
	"rejected":  "Your commit was rejected",
	// The API doesn't say which level, unlike the web.
	"leveled_up": "You reached a new level",
}

// notificationIcons mark each kind of notification, colored as what they're about.
func notificationIcon(typ string) string {
	switch typ {
	case "starred":
		return fmt.Sprintf("[%s]★[-]", colorPrimary)
	case "replied":
		return fmt.Sprintf("[%s]↩[-]", colorInk)
	case "quoted":
		return fmt.Sprintf("[%s]❝[-]", colorInk)
	case "mentioned":
		return fmt.Sprintf("[%s]@[-]", colorPrimary)
	case "followed":
		return fmt.Sprintf("[%s]+[-]", colorSuccess)
	case "approved":
		return fmt.Sprintf("[%s]✓[-]", colorSuccess)
	case "rejected":
		return fmt.Sprintf("[%s]✕[-]", colorDanger)
	case "leveled_up":
		return fmt.Sprintf("[%s]▲[-]", colorWarning)
	}

	return fmt.Sprintf("[%s]•[-]", colorMuted)
}

func notificationLines(n *api.Notification, selected bool, width int) []string {
	bar := " "
	if selected {
		bar = fmt.Sprintf("[%s]▌[-]", colorPrimary)
	}
	first := bar + notificationIcon(n.Type) + " "
	rest := bar + "  "

	verb := notificationVerbs[n.Type]
	if verb == "" {
		verb = n.Type
	}

	var head string
	switch n.Type {
	case "approved", "rejected", "leveled_up":
		head = fmt.Sprintf("[%s]%s[-]", colorInk, verb)
		if n.Reason != nil && *n.Reason != "" {
			head += fmt.Sprintf("[%s]: %s[-]", colorInk, tview.Escape(*n.Reason))
		}
	default:
		actor := "someone"
		if n.Actor != nil {
			name := n.Actor.Name
			if name == "" {
				name = n.Actor.Username
			}
			actor = fmt.Sprintf("[%s::b]%s[-::-]", colorInk, tview.Escape(name))
		}
		if others := n.ActorsCount - 1; others > 0 {
			actor += fmt.Sprintf("[%s] and %d other%s[-]", colorInk, others, plural(others))
		}
		head = actor + fmt.Sprintf(" [%s]%s[-]", colorInk, verb)
	}

	head += fmt.Sprintf(" [%s]· %s[-]", colorMuted, relativeTime(n.CreatedAt))
	if n.UnreadCount > 0 {
		head += fmt.Sprintf(" [%s]●[-]", colorPrimary)
	}

	lines := wrapIndent(head, first, rest, width)
	if c := n.Commit; c != nil {
		inner := max(width-gutterWidth, 1)
		body := bodyLines(c.BodySegments, c.Body, c.Mentions, inner)
		for i, l := range body {
			body[i] = fmt.Sprintf("[%s]", colorMuted) + l
		}
		lines = append(lines, prefixed(rest, truncateLines(body, 3))...)
	}

	return append(lines, "")
}
