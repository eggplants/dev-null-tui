package tui

import (
	"strings"
	"testing"
)

// The cases agree with App\Services\Urls::characterCount() and hasOversizedUrl().
func TestBodyLength(t *testing.T) {
	long := "https://example.com/" + strings.Repeat("a", 100)

	for _, tt := range []struct {
		body      string
		want      int
		oversized bool
	}{
		{"x", 1, false},
		{"  日本語  ", 3, false},
		{long, 23, false},
		{"see " + long + ".", 4 + 23 + 1, false},
		{"see nativephp.com/docs/getting-started/installation.", 28, false},
		{"(https://en.wikipedia.org/wiki/Foo_(bar_baz_qux)).", 1 + 23 + 2, false},
		// Code counts literally.
		{"`" + long + "`", 2 + 120, false},
		{"```\n" + long + "\n```", 8 + 120, false},
		// None of these is a link.
		{"socket.io.js nativephp.community array.at(-1) bug.It user@example.com", 69, false},
		{"https://", 8, false},
		// Blank lines are collapsed first.
		{"a\n\n\n\n\nb", 4, false},
		{"https://example.com/" + strings.Repeat("a", 2030), 2050, true},
	} {
		if got, oversized := bodyLength(tt.body); got != tt.want || oversized != tt.oversized {
			t.Errorf("bodyLength(%.40q) = %d, %v, want %d, %v", tt.body, got, oversized, tt.want, tt.oversized)
		}
	}
}
