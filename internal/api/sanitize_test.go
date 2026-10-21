package api

import "testing"

func TestSanitizeStripsWhatTheTerminalWouldActOn(t *testing.T) {
	c := &Commit{
		Body:         "hi\x1b]0;pwned\x07 there\r\n\tindented ‮evil",
		Author:       &User{Name: "A\x1b[2Jlice"},
		BodySegments: []Segment{{Type: "text", Text: "x\u009by"}},
		ReplyPreview: &Page[Commit]{Data: []Commit{{Body: "\x00ok"}}},
	}
	sanitize(c)

	if c.Body != "hi]0;pwned there\n\tindented evil" {
		t.Errorf("Body = %q", c.Body)
	}
	if c.Author.Name != "A[2Jlice" || c.BodySegments[0].Text != "xy" || c.ReplyPreview.Data[0].Body != "ok" {
		t.Errorf("sanitized %q %q %q", c.Author.Name, c.BodySegments[0].Text, c.ReplyPreview.Data[0].Body)
	}
}
