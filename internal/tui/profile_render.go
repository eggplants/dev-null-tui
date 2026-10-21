package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/rivo/tview"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// initials are what a profile's avatar shows: the first letters of the name's first two words, or of the username.
func initials(u *api.User) string {
	words := strings.Fields(u.Name)
	if len(words) == 0 {
		words = []string{u.Username}
	}

	var out []rune
	for _, w := range words[:min(2, len(words))] {
		for _, r := range w {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				out = append(out, unicode.ToUpper(r))
				break
			}
		}
	}
	if len(out) == 0 {
		return "?"
	}

	return string(out)
}

// profileAvatar is where a profile's avatar goes in its head: under the blank first line, at the left, four columns
// by two rows.
const profileAvatarCol, profileAvatarLine, profileAvatarCols, profileAvatarRows = 1, 1, 4, 2

// pill is a small label next to a profile's name.
func pill(text, color string) string {
	return fmt.Sprintf("[%s]⟨%s⟩[-]", color, text)
}

// profileLines renders a profile's head. pasteisLeft is how many pastéis the user has left to give today, shown on
// their own profile in pastéis mode, or -1.
func profileLines(p *api.Profile, me string, pasteisLeft, width int) []string {
	u := &p.Data
	const indent = "      "
	inner := max(width-len(indent)-1, 10)

	name := u.Name
	if name == "" {
		name = u.Username
	}
	head := fmt.Sprintf("%s  [%s::b]%s[-::-] [%s]@%s[-]", strings.Repeat(" ", profileAvatarCol+profileAvatarCols-1), colorInk, tview.Escape(name), colorMuted, tview.Escape(u.Username))

	var pills []string
	switch {
	case u.Username == me:
		pills = append(pills, pill("you", colorMuted))
	default:
		if p.FollowsYou {
			pills = append(pills, pill("follows you", colorMuted))
		}
		if p.IsMuting {
			pills = append(pills, pill("muted", colorWarning))
		}
		if p.IsFollowing {
			pills = append(pills, pill("✓ following", colorPrimary))
		} else {
			pills = append(pills, pill("+ F to follow", colorMuted))
		}
	}

	lines := []string{"", spread(head, strings.Join(pills, " ")+" ", width)}
	if u.Bio != "" {
		lines = append(lines, wrap(fmt.Sprintf("[%s]%s[-]", colorInk, tview.Escape(u.Bio)), indent, inner+len(indent))...)
	}

	var facts []string
	for _, f := range []string{u.Company, u.Location, strings.TrimPrefix(strings.TrimPrefix(u.Website, "https://"), "http://")} {
		if f != "" {
			facts = append(facts, tview.Escape(f))
		}
	}
	if !u.CreatedAt.IsZero() {
		facts = append(facts, "joined "+u.CreatedAt.Local().Format("Jan 2006"))
	}
	if len(facts) > 0 {
		lines = append(lines, wrap(fmt.Sprintf("[%s]%s[-]", colorMuted, strings.Join(facts, " · ")), indent, inner+len(indent))...)
	}

	var counts []string
	for _, c := range []struct {
		n     *int
		label string
	}{{u.CommitsCount, "commits"}, {u.FollowersCount, "followers"}, {u.FollowingCount, "following"}} {
		if c.n != nil {
			counts = append(counts, fmt.Sprintf("[%s::b]%s[-::-] [%s]%s[-]", colorInk, count(*c.n), colorMuted, c.label))
		}
	}
	if showPasteis && p.PasteisReceived > 0 {
		counts = append(counts, fmt.Sprintf("[%s::b]%s[-::-] [%s]pastéis[-]", theme.lanes[5], count(p.PasteisReceived), colorMuted))
	}
	if len(counts) > 0 {
		lines = append(lines, wrap(strings.Join(counts, "   "), indent, inner+len(indent))...)
	}
	if showPasteis && pasteisLeft >= 0 && u.Username == me {
		lines = append(lines, indent+fmt.Sprintf("[%s]◍[-] [%s]%d pastéis left to give today[-]", theme.lanes[5], colorMuted, pasteisLeft))
	}

	if p.Activity != nil && len(p.Activity.Buckets) > 0 {
		lines = append(lines, "")
		lines = append(lines, prefixed(indent, activityLines(p.Activity, laneColor(u), inner))...)
	}
	lines = append(lines, prefixed(indent, standingLines(p, inner))...)

	return lines
}

// activityShade is the color of a day with count commits, out of the most on any day: the lane, mixed into the
// background the less there were, as the web's graph does.
func activityShade(n, most int, lane string) string {
	if n == 0 || most == 0 {
		return colorWash
	}

	weights := []float64{0.32, 0.56, 0.80, 1}
	level := min((n*len(weights)-1)/most, len(weights)-1)

	return mix(lane, theme.shell, weights[level])
}

// activityLines draws the commits a user made: a year as a grid of weeks, each cell two days in half blocks, or a
// shorter window as bars.
func activityLines(a *api.Activity, lane string, width int) []string {
	most, total := 0, 0
	for _, b := range a.Buckets {
		most = max(most, b.Count)
		total += b.Count
	}

	window := map[string]string{"day": "the last 24 hours", "week": "the last 7 days", "month": "the last 30 days", "year": "the last year"}[a.Scale]
	if window == "" {
		window = "a while"
	}
	summary := fmt.Sprintf("[%s]%s commit%s in %s[-]", colorMuted, count(total), plural(total), window)

	if a.Scale != "year" {
		return append(activityBars(a.Buckets, most, lane, width), summary)
	}

	weeks := (len(a.Buckets) + 6) / 7
	shown := min(weeks, width)
	first := weeks - shown

	var months strings.Builder
	used := 0
	lastMonth := -1
	for w := first; w < weeks; w++ {
		col := w - first
		day, err := time.Parse("2006-01-02", a.Buckets[w*7].Date)
		if err != nil {
			continue
		}
		if m := int(day.Month()); m != lastMonth {
			lastMonth = m
			label := day.Format("Jan")
			if col >= used && col+len(label) <= shown && (w > first || day.Day() <= 7) {
				months.WriteString(strings.Repeat(" ", col-used) + label)
				used = col + len(label)
			}
		}
	}
	lines := []string{fmt.Sprintf("[%s]%s[-]", colorFaint, months.String())}

	shade := func(i int) string {
		if i >= len(a.Buckets) {
			return ""
		}
		return activityShade(a.Buckets[i].Count, most, lane)
	}
	for row := range 4 {
		var b strings.Builder
		for w := first; w < weeks; w++ {
			top, bottom := shade(w*7+row*2), ""
			if row*2+1 < 7 {
				bottom = shade(w*7 + row*2 + 1)
			}
			switch {
			case top == "" && bottom == "":
				b.WriteString(" ")
			case bottom == "":
				fmt.Fprintf(&b, "[%s]▀[-]", top)
			case top == "":
				fmt.Fprintf(&b, "[%s]▄[-]", bottom)
			default:
				fmt.Fprintf(&b, "[%s:%s]▀[-:-]", top, bottom)
			}
		}
		lines = append(lines, b.String())
	}

	legend := fmt.Sprintf("[%s]less[-] ", colorFaint)
	for _, n := range []int{0, 1, 2, 3, 4} {
		legend += fmt.Sprintf("[%s]■[-]", activityShade(n, 4, lane))
	}
	legend += fmt.Sprintf(" [%s]more[-]", colorFaint)

	if tview.TaggedStringWidth(summary)+tview.TaggedStringWidth(legend)+2 > shown {
		return append(lines, summary)
	}

	return append(lines, spread(summary, legend, shown))
}

// activityBars draws a short window's buckets as bars, as wide as they fit.
func activityBars(buckets []api.ActivityBucket, most int, lane string, width int) []string {
	levels := []rune(" ▁▂▃▄▅▆▇█")
	each := max(1, min(3, width/max(1, len(buckets))))

	var top, bottom strings.Builder
	for _, b := range buckets {
		// Two rows give 16 steps.
		steps := 0
		if most > 0 && b.Count > 0 {
			steps = max(1, b.Count*16/most)
		}
		low, high := min(steps, 8), max(steps-8, 0)
		color := activityShade(b.Count, most, lane)
		if b.Count == 0 {
			fmt.Fprintf(&bottom, "[%s]%s[-]", colorRail, strings.Repeat("▁", each))
			top.WriteString(strings.Repeat(" ", each))
			continue
		}
		fmt.Fprintf(&top, "[%s]%s[-]", color, strings.Repeat(string(levels[high]), each))
		fmt.Fprintf(&bottom, "[%s]%s[-]", color, strings.Repeat(string(levels[low]), each))
	}

	return []string{top.String(), bottom.String()}
}

// standingLines are a profile's streaks, Unix rank and badges.
func standingLines(p *api.Profile, width int) []string {
	var lines []string

	var parts []string
	if s := p.Streaks; s != nil {
		parts = append(parts, fmt.Sprintf("[%s]streak[-] [%s::b]%dd[-::-] [%s]· longest[-] [%s::b]%dd[-::-]", colorMuted, colorInk, s.Current, colorMuted, colorInk, s.Longest))
	}
	if a := p.Achievements; a != nil {
		rank := fmt.Sprintf("[%s::b]Lv. %d · %s[-::-] [%s]%s XP[-]", colorInk, a.Level, tview.Escape(a.Rank), colorMuted, count(a.XP))
		const barWidth = 10
		filled := min(barWidth, a.LevelProgress*barWidth/100)
		rank += fmt.Sprintf(" [%s]%s[%s]%s[-]", colorPrimary, strings.Repeat("▰", filled), colorRail, strings.Repeat("▱", barWidth-filled))
		if a.NextLevelXP != nil {
			rank += fmt.Sprintf(" [%s]%s XP to the next[-]", colorFaint, count(max(0, *a.NextLevelXP-a.XP)))
		}
		parts = append(parts, rank)
	}
	if len(parts) > 0 {
		lines = append(lines, wrap(strings.Join(parts, "   "), "", width)...)
	}

	if a := p.Achievements; a != nil && len(a.Badges) > 0 {
		var badges []string
		for _, b := range a.Badges {
			if b.Unlocked {
				badges = append(badges, fmt.Sprintf("[%s]✓[-] [%s]%s[-]", colorSuccess, colorInk, tview.Escape(b.Name)))
			}
		}
		for _, b := range a.Badges {
			if !b.Unlocked {
				badges = append(badges, fmt.Sprintf("[%s]○ %s %d/%d[-]", colorFaint, tview.Escape(b.Name), b.Progress, b.Target))
			}
		}
		lines = append(lines, wrap(strings.Join(badges, "  "), "", width)...)
	}

	return lines
}

// profileTabs is the row of a profile's tabs, the current one underlined.
func profileTabs(names []string, current, width int) string {
	var b strings.Builder
	b.WriteString(" ")
	for i, name := range names {
		if i == current {
			fmt.Fprintf(&b, "[%s::bu]%s[-::-][::U]", colorInk, name)
		} else {
			fmt.Fprintf(&b, "[%s]%s[-]", colorMuted, name)
		}
		b.WriteString("   ")
	}
	fmt.Fprintf(&b, "[%s](T next)[-]", colorFaint)

	return b.String()
}

// personLines renders someone in a list of people, with how they and the user follow each other.
func personLines(u *api.User, selected bool, width int) []string {
	bar := " "
	if selected {
		bar = fmt.Sprintf("[%s]▌[-]", colorPrimary)
	}

	var pills []string
	if u.FollowsYou != nil && *u.FollowsYou {
		pills = append(pills, pill("follows you", colorMuted))
	}
	if u.IsFollowed != nil && *u.IsFollowed {
		pills = append(pills, pill("✓ following", colorPrimary))
	}

	head := bar + avatarRoom + authorName(u)
	lines := []string{spread(head, strings.Join(pills, " "), width)}
	if u.Bio != "" {
		lines = append(lines, truncateLines(wrap(fmt.Sprintf("[%s]%s[-]", colorMuted, tview.Escape(u.Bio)), bar+avatarRoom, width), 2)...)
	}

	return append(lines, "")
}
