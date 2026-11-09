package tui

import (
	"testing"
	"time"
)

func TestParseWhen(t *testing.T) {
	now := time.Date(2026, 11, 9, 21, 15, 30, 0, time.Local)
	for text, want := range map[string]time.Time{
		"+30m":             time.Date(2026, 11, 9, 21, 45, 0, 0, time.Local),
		"in 2h":            time.Date(2026, 11, 9, 23, 15, 0, 0, time.Local),
		"+1d":              time.Date(2026, 11, 10, 21, 15, 0, 0, time.Local),
		"22:00":            time.Date(2026, 11, 9, 22, 0, 0, 0, time.Local),
		"9:00":             time.Date(2026, 11, 10, 9, 0, 0, 0, time.Local),
		"tomorrow":         time.Date(2026, 11, 10, 9, 0, 0, 0, time.Local),
		"Tomorrow 18:30":   time.Date(2026, 11, 10, 18, 30, 0, 0, time.Local),
		"2026-11-12 09:00": time.Date(2026, 11, 12, 9, 0, 0, 0, time.Local),
		"11-12 9:05":       time.Date(2026, 11, 12, 9, 5, 0, 0, time.Local),
		"1-3 10:00":        time.Date(2027, 1, 3, 10, 0, 0, 0, time.Local),
	} {
		got, err := parseWhen(text, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("parseWhen(%q) = %s, %v; want %s", text, got, err, want)
		}
	}

	for _, text := range []string{"", "soon", "25:00", "2026-13-01 10:00", "+5y"} {
		if _, err := parseWhen(text, now); err == nil {
			t.Errorf("parseWhen(%q) took it", text)
		}
	}
}
