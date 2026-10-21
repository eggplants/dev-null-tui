package tui

import (
	"html"
	"regexp"
	"strings"

	"github.com/rivo/tview"
	"github.com/rivo/uniseg"

	"github.com/eggplants/dev-null-tui/internal/api"
)

// span is a run of text in one style. style is what goes between a tview style tag's brackets, like "#ececec::b";
// empty is the default style.
type span struct {
	text  string
	style string
	// glue keeps the span on one line when it fits, as the web never breaks inline code.
	glue bool
}

// tagged turns spans into text with tview style tags, each span ending back on the default style. The background
// resets to the cell's, so a selected commit's wash shows through.
func tagged(spans []span) string {
	var b strings.Builder
	style := ""
	for _, s := range spans {
		if s.text == "" {
			continue
		}
		// Spans in the same style share their tags.
		if s.style != style {
			if style != "" {
				b.WriteString(closeTag(style))
			}
			if s.style != "" {
				b.WriteString("[" + s.style + "]")
			}
			style = s.style
		}
		b.WriteString(tview.Escape(s.text))
	}
	if style != "" {
		b.WriteString(closeTag(style))
	}

	return b.String()
}

// closeTag goes back to the default style after style. Underlining is apart from the other attributes in tcell, so
// "-" doesn't take it off and "U" has to.
func closeTag(style string) string {
	fields := strings.SplitN(style, ":", 3)
	if len(fields) == 3 && strings.Contains(fields[2], "u") {
		return "[-:-:-][::U]"
	}

	return "[-:-:-]"
}

func spansWidth(spans []span) int {
	n := 0
	for _, s := range spans {
		n += uniseg.StringWidth(s.text)
	}

	return n
}

// token is a word, or the spaces between words, in one style.
type token struct {
	span
	space bool
	width int
}

func tokenize(spans []span) []token {
	var tokens []token
	for _, s := range spans {
		if s.glue {
			tokens = append(tokens, token{span: s, width: uniseg.StringWidth(s.text)})
			continue
		}

		rest := s.text
		for rest != "" {
			space := rest[0] == ' '
			end := strings.IndexFunc(rest, func(r rune) bool { return (r == ' ') != space })
			if end < 0 {
				end = len(rest)
			}
			part := span{text: rest[:end], style: s.style}
			tokens = append(tokens, token{span: part, space: space, width: uniseg.StringWidth(part.text)})
			rest = rest[end:]
		}
	}

	return tokens
}

// wrapSpans word-wraps spans to width, breaking paragraphs at newlines and words wider than a line wherever they
// must. Spaces at the start of a wrapped line are dropped; a paragraph's own indentation is kept.
func wrapSpans(spans []span, width int) [][]span {
	width = max(width, 1)

	var paragraphs [][]span
	current := []span{}
	for _, s := range spans {
		parts := strings.Split(s.text, "\n")
		for i, part := range parts {
			if i > 0 {
				paragraphs = append(paragraphs, current)
				current = []span{}
			}
			if part != "" {
				current = append(current, span{text: part, style: s.style, glue: s.glue})
			}
		}
	}
	paragraphs = append(paragraphs, current)

	var lines [][]span
	for _, p := range paragraphs {
		lines = append(lines, wrapParagraph(p, width)...)
	}

	return lines
}

func wrapParagraph(spans []span, width int) [][]span {
	var lines [][]span
	var line []span
	used := 0
	wrapped := false

	flush := func() {
		lines = append(lines, line)
		line, used, wrapped = nil, 0, true
	}

	for _, t := range tokenize(spans) {
		if t.space && used == 0 && wrapped {
			continue
		}
		if used+t.width <= width {
			line = append(line, t.span)
			used += t.width
			continue
		}
		if t.space {
			flush()
			continue
		}
		if used > 0 && t.width <= width {
			flush()
			line, used = append(line, t.span), t.width
			continue
		}

		// The word is wider than a line, so it goes in pieces.
		g := uniseg.NewGraphemes(t.text)
		var piece strings.Builder
		for g.Next() {
			w := g.Width()
			if used+w > width && used > 0 {
				line = append(line, span{text: piece.String(), style: t.style})
				piece.Reset()
				flush()
			}
			piece.WriteString(g.Str())
			used += w
		}
		line = append(line, span{text: piece.String(), style: t.style})
	}

	return append(lines, line)
}

// Styles of a commit body's parts.
func styleText() string       { return colorInk }
func styleMention() string    { return colorPrimary }
func styleLink() string       { return colorPrimary + "::u" }
func styleInlineCode() string { return colorInk + ":" + colorWash }

// highlightColors maps the classes tempest/highlight puts on code to the colors the web's app.css gives them.
func highlightStyle(class string) string {
	switch class {
	case "hl-keyword":
		return colorPrimary
	case "hl-type", "hl-generic":
		return theme.brand
	case "hl-property":
		return theme.lanes[3]
	case "hl-value", "hl-addition":
		return theme.lanes[1]
	case "hl-number", "hl-literal":
		return theme.lanes[0]
	case "hl-variable":
		return theme.lanes[4]
	case "hl-comment":
		return colorMuted + "::i"
	case "hl-attribute":
		return colorInk + "::i"
	case "hl-deletion":
		return colorDanger
	}

	return colorInk
}

var (
	htmlTagPattern = regexp.MustCompile(`<(/?)([a-zA-Z]+)([^>]*)>`)
	classPattern   = regexp.MustCompile(`class="([^"]*)"`)
)

// highlighted turns a code block's HTML, as tempest/highlight writes it, into spans colored by the innermost class.
func highlighted(source string) []span {
	var spans []span
	var classes []string

	add := func(text string) {
		if text == "" {
			return
		}
		style := colorInk
		for i := len(classes) - 1; i >= 0; i-- {
			if classes[i] != "" {
				style = highlightStyle(classes[i])
				break
			}
		}
		spans = append(spans, span{text: html.UnescapeString(text), style: style})
	}

	last := 0
	for _, m := range htmlTagPattern.FindAllStringSubmatchIndex(source, -1) {
		add(source[last:m[0]])
		last = m[1]

		if !strings.EqualFold(source[m[4]:m[5]], "span") {
			continue
		}
		if source[m[2]:m[3]] == "/" {
			if len(classes) > 0 {
				classes = classes[:len(classes)-1]
			}
			continue
		}
		class := ""
		if c := classPattern.FindStringSubmatch(source[m[6]:m[7]]); c != nil {
			for _, name := range strings.Fields(c[1]) {
				if strings.HasPrefix(name, "hl-") {
					class = name
				}
			}
		}
		classes = append(classes, class)
	}
	add(source[last:])

	return spans
}

var (
	urlPattern     = regexp.MustCompile(`https?://[^\s<>"']+[^\s<>"'.,;:!?)\]]`)
	mentionPattern = regexp.MustCompile(`(^|[^A-Za-z0-9_.@-])@([A-Za-z0-9](?:[A-Za-z0-9-]{0,38}))`)
)

// proseSpans styles prose: links in the link style without their scheme, and mentions of the people the server says
// it mentions, or of anyone when it didn't say.
func proseSpans(text string, mentions map[string]bool) []span {
	var spans []span

	plain := func(s string) {
		last := 0
		for _, m := range mentionPattern.FindAllStringSubmatchIndex(s, -1) {
			name := s[m[4]:m[5]]
			if mentions != nil && !mentions[strings.ToLower(name)] {
				continue
			}
			at := m[4] - 1
			spans = append(spans, span{text: s[last:at], style: styleText()})
			spans = append(spans, span{text: s[at:m[5]], style: styleMention()})
			last = m[5]
		}
		spans = append(spans, span{text: s[last:], style: styleText()})
	}

	last := 0
	for _, m := range urlPattern.FindAllStringIndex(text, -1) {
		plain(text[last:m[0]])
		link := text[m[0]:m[1]]
		link = strings.TrimPrefix(strings.TrimPrefix(link, "https://"), "http://")
		spans = append(spans, span{text: link, style: styleLink()})
		last = m[1]
	}
	plain(text[last:])

	return spans
}

// mentionNames are the lowercased usernames a commit mentions, or nil when the API didn't say.
func mentionNames(mentions []api.Mention) map[string]bool {
	if mentions == nil {
		return nil
	}

	names := map[string]bool{}
	for _, m := range mentions {
		names[strings.ToLower(m.Username)] = true
	}

	return names
}

// bodyLines lays a commit body's segments out at width as tagged lines: prose with its links and mentions, inline
// code on the wash, and code blocks under their language with a bar down their side. Without segments, as a
// notification's commit may come, the plain body is prose.
func bodyLines(segments []api.Segment, body string, mentions []api.Mention, width int) []string {
	names := mentionNames(mentions)
	if len(segments) == 0 {
		segments = []api.Segment{{Type: "text", Text: body}}
	}

	var lines []string
	var prose []span
	flush := func() {
		if len(prose) == 0 {
			return
		}
		for _, l := range wrapSpans(prose, width) {
			lines = append(lines, tagged(l))
		}
		prose = nil
	}

	for _, s := range segments {
		switch s.Type {
		case "code":
			prose = append(prose, span{text: " " + s.Code + " ", style: styleInlineCode(), glue: true})
		case "block":
			flush()
			lines = append(lines, codeBlockLines(s, width)...)
		default:
			prose = append(prose, proseSpans(s.Text, names)...)
		}
	}
	flush()

	return lines
}

// codeBlockLines draws a code block on the wash, under its language, with a bar down its side.
func codeBlockLines(s api.Segment, width int) []string {
	var lines []string
	if s.Language != "" {
		lines = append(lines, tagged([]span{{text: s.Language, style: colorFaint}}))
	}

	code := []span{{text: html.UnescapeString(htmlTagPattern.ReplaceAllString(s.HTML, "")), style: colorInk}}
	if strings.Contains(s.HTML, "hl-") {
		code = highlighted(s.HTML)
	}
	code = trimTrailingNewlines(code)

	bar := tagged([]span{{text: "▏", style: colorRail}})
	inner := max(width-2, 1)
	for _, l := range wrapSpans(code, inner) {
		fill := strings.Repeat(" ", max(0, inner-spansWidth(l)))
		lines = append(lines, bar+" "+tagged(withBackground(append(l, span{text: fill}), colorWash)))
	}

	return lines
}

func trimTrailingNewlines(spans []span) []span {
	for len(spans) > 0 {
		last := &spans[len(spans)-1]
		last.text = strings.TrimRight(last.text, "\n")
		if last.text != "" {
			break
		}
		spans = spans[:len(spans)-1]
	}

	return spans
}

// withBackground gives every span a background, keeping their colors and attributes.
func withBackground(spans []span, bg string) []span {
	out := make([]span, len(spans))
	for i, s := range spans {
		fields := strings.SplitN(s.style, ":", 3)
		for len(fields) < 3 {
			fields = append(fields, "")
		}
		if fields[0] == "" {
			fields[0] = colorInk
		}
		fields[1] = bg
		out[i] = span{text: s.text, style: strings.Join(fields, ":"), glue: s.glue}
	}

	return out
}
