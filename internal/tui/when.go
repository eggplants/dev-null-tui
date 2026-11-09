package tui

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	inPattern    = regexp.MustCompile(`^(?:in\s+|\+)(\d+)\s*(m|min|mins|minutes?|h|hours?|d|days?)$`)
	clockPattern = regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)
)

// parseWhen reads when to schedule a commit, after now: "+30m", "in 2h", "+1d", "18:30" (the next one), "tomorrow",
// "tomorrow 9:00", "2026-11-12 09:00" or "11-12 09:00", in the local time.
func parseWhen(text string, now time.Time) (time.Time, error) {
	text = strings.ToLower(strings.Join(strings.Fields(text), " "))
	if text == "" {
		return time.Time{}, errors.New("say when")
	}

	if m := inPattern.FindStringSubmatch(text); m != nil {
		n, _ := strconv.Atoi(m[1])
		unit := map[byte]time.Duration{'m': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour}[m[2][0]]
		return now.Add(time.Duration(n) * unit).Truncate(time.Minute), nil
	}

	day, rest, tomorrow := now, text, false
	if r, ok := strings.CutPrefix(text, "tomorrow"); ok {
		day, rest, tomorrow = now.AddDate(0, 0, 1), strings.TrimSpace(r), true
		if rest == "" {
			rest = "9:00"
		}
	} else if date, clock, ok := strings.Cut(text, " "); ok {
		layout := "2006-01-02"
		if strings.Count(date, "-") == 1 {
			date, layout = strconv.Itoa(now.Year())+"-"+date, "2006-1-2"
		}
		d, err := time.ParseInLocation(layout, date, now.Location())
		if err != nil {
			return time.Time{}, errors.New("write the day as 2026-11-12, or 11-12")
		}
		if layout == "2006-1-2" && d.Before(now.Truncate(24*time.Hour)) {
			d = d.AddDate(1, 0, 0)
		}
		day, rest, tomorrow = d, clock, true
	}

	m := clockPattern.FindStringSubmatch(rest)
	if m == nil {
		return time.Time{}, errors.New("try +2h, 18:30, tomorrow 9:00 or 2026-11-12 09:00")
	}
	hour, _ := strconv.Atoi(m[1])
	minute, _ := strconv.Atoi(m[2])
	if hour > 23 || minute > 59 {
		return time.Time{}, errors.New("that isn't a time of day")
	}

	at := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, now.Location())
	// A time of day alone is the next one.
	if !tomorrow && !at.After(now) {
		at = at.AddDate(0, 0, 1)
	}

	return at, nil
}
