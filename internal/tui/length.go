package tui

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The server's limits on a commit's body: App\Models\Commit, App\Rules\CommitBodyLength and App\Services\Urls.
const (
	minBodyLength    = 1
	maxBodyLength    = 4000
	maxRawBodyLength = 40000
	maxURLLength     = 2048
	// urlWeight is the most characters a link in the prose counts as.
	urlWeight = 23
)

// bodyLength measures a body the way the server does: once normalized, with each link in the prose counting as at
// most urlWeight characters. Code counts literally. oversized reports a link longer than the server takes.
// Keep in sync with App\Services\Urls::characterCount() and hasOversizedUrl().
func bodyLength(body string) (n int, oversized bool) {
	body = normalizeBody(body)
	n = utf8.RuneCountInString(body)

	for _, match := range urlMatches(withoutCode(body)) {
		link := withoutTrailingPunctuation(match)
		url := link
		if !hasScheme(url) {
			url = "https://" + url
		}

		switch size := utf8.RuneCountInString(url); {
		case size > maxURLLength:
			oversized = true
		case !strings.HasSuffix(url, "://"):
			n -= max(0, utf8.RuneCountInString(link)-urlWeight)
		}
	}

	return n, oversized
}

var blankLines = regexp.MustCompile(`(?:[ \t]*\n){3,}`)

// normalizeBody is the body as the server saves it: trimmed, with runs of blank lines in the prose collapsed into one.
// Keep in sync with App\Services\CommitBodies::normalize().
func normalizeBody(body string) string {
	parts := splitCode(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(strings.TrimSpace(body)))
	for i := 0; i < len(parts); i += 2 {
		parts[i] = blankLines.ReplaceAllString(parts[i], "\n\n")
	}

	return strings.Join(parts, "")
}

// withoutCode removes the code, leaving a line break in its place so that the prose on either side stays apart.
func withoutCode(text string) string {
	parts := splitCode(text)
	prose := make([]string, 0, len(parts)/2+1)
	for i := 0; i < len(parts); i += 2 {
		prose = append(prose, parts[i])
	}

	return strings.Join(prose, "\n")
}

// splitCode splits the text into prose and code, alternating: even indexes are prose (maybe empty), odd ones are a
// fenced code block or inline code. Keep in sync with App\Services\CodeSpans::PATTERN.
func splitCode(text string) []string {
	var parts []string
	last := 0
	for i := 0; i < len(text); {
		if text[i] != '`' {
			i++
			continue
		}

		end := fenceEnd(text, i)
		if end < 0 {
			end = inlineCodeEnd(text, i)
		}
		if end < 0 {
			i++
			continue
		}

		parts = append(parts, text[last:i], text[i:end])
		last, i = end, end
	}

	return append(parts, text[last:])
}

// fenceEnd is where a fenced code block that opens at i ends, or -1: a line starting with ``` or more (and maybe a
// language) up to the first line that is only ``` or more.
func fenceEnd(text string, i int) int {
	if i > 0 && text[i-1] != '\n' {
		return -1
	}

	j := i + backticks(text, i)
	if j-i < 3 {
		return -1
	}
	for j < len(text) && !strings.ContainsRune("`\r\n", rune(text[j])) {
		j++
	}
	if strings.HasPrefix(text[j:], "\r\n") {
		j++
	}
	if j >= len(text) || text[j] != '\n' {
		return -1
	}

	for line := j + 1; ; {
		if k := line + backticks(text, line); k-line >= 3 {
			for k < len(text) && (text[k] == ' ' || text[k] == '\t') {
				k++
			}
			if k == len(text) || text[k] == '\r' || text[k] == '\n' {
				return k
			}
		}

		next := strings.IndexByte(text[line:], '\n')
		if next < 0 {
			return -1
		}
		line += next + 1
	}
}

// inlineCodeEnd is where inline code that opens at i ends, or -1: some text between two backticks on one line.
func inlineCodeEnd(text string, i int) int {
	end := strings.IndexAny(text[i+1:], "`\r\n")
	if end < 1 || text[i+1+end] != '`' {
		return -1
	}

	return i + end + 2
}

func backticks(text string, i int) int {
	n := 0
	for i+n < len(text) && text[i+n] == '`' {
		n++
	}

	return n
}

// topLevelDomains are the ones a bare domain may end with. Keep in sync with App\Services\Urls::TOP_LEVEL_DOMAINS.
var topLevelDomains = func() []string {
	domains := strings.Fields(`com org net edu gov mil info biz io dev app co ai me xyz tech gg tv fm ly to is blog
		cloud site online design studio tools software codes wiki news website club ninja rocks academy guide social
		digital agency fyi lol live
		uk de fr nl es it pt br ca au jp ch se no dk fi be at ie nz us eu cz sk hu ro bg gr hr si ee lv lt ru ua tr il
		za mx ar kr cn tw hk sg vn th`)
	upper := make([]string, len(domains))
	for i, d := range domains {
		upper[i] = strings.ToUpper(d)
	}

	return append(domains, upper...)
}()

// urlMatches finds what the server's URL pattern matches in the text, in order: a URL with a scheme, or a bare domain
// like "nativephp.com". Go's regexp has no lookarounds, so the pattern in App\Services\Urls::pattern() is spelled out.
func urlMatches(text string) []string {
	var matches []string
	for i := 0; i < len(text); {
		prev := rune(-1)
		if i > 0 {
			prev, _ = utf8.DecodeLastRuneInString(text[:i])
		}

		end := schemeURLEnd(text, i, prev)
		if end < 0 {
			end = bareDomainEnd(text, i, prev)
		}
		if end > i {
			matches = append(matches, text[i:end])
			i = end
			continue
		}

		_, size := utf8.DecodeRuneInString(text[i:])
		i += size
	}

	return matches
}

// schemeURLEnd is where an http(s) URL starting at i ends, or -1. It can't follow a letter, number, mark or
// connector punctuation.
func schemeURLEnd(text string, i int, prev rune) int {
	if unicode.In(prev, unicode.L, unicode.N, unicode.M, unicode.Pc) {
		return -1
	}

	rest := text[i:]
	if len(rest) < 4 || !strings.EqualFold(rest[:4], "http") {
		return -1
	}
	j := 4
	if j < len(rest) && (rest[j] == 's' || rest[j] == 'S') {
		j++
	}
	if !strings.HasPrefix(rest[j:], "://") {
		return -1
	}

	end := i + j + 3 + unbrokenLength(rest[j+3:])
	if end == i+j+3 {
		return -1
	}

	return end
}

// bareDomainEnd is where a bare domain starting at i ends, path included, or -1: ASCII labels ending in an allowed
// TLD. It can't follow a letter, number, "_", ".", "/", "@" or "-", and its TLD must end the word without a "(" or
// another label after it. As the server's regex backtracks, the most labels win, then the first TLD that fits.
func bareDomainEnd(text string, i int, prev rune) int {
	if unicode.In(prev, unicode.L, unicode.N) || strings.ContainsRune("_./@-", prev) {
		return -1
	}

	// Where each label followed by a "." ends.
	var ends []int
	for j := i; ; {
		k := j
		for k < len(text) && (isAlnum(text[k]) || text[k] == '-') {
			k++
		}
		if k == j || k >= len(text) || text[k] != '.' || text[j] == '-' || text[k-1] == '-' {
			break
		}
		ends = append(ends, k+1)
		j = k + 1
	}

	for l := len(ends) - 1; l >= 0; l-- {
		p := ends[l]
		for _, tld := range topLevelDomains {
			if !strings.HasPrefix(text[p:], tld) || !endsWord(text[p+len(tld):]) {
				continue
			}

			end := p + len(tld)
			if end < len(text) && strings.ContainsRune("/?#", rune(text[end])) {
				end += 1 + unbrokenLength(text[end+1:])
			}

			return end
		}
	}

	return -1
}

// endsWord is whether a TLD followed by rest ends the word: no letter, number, "_", "(" or "-", nor another label.
func endsWord(rest string) bool {
	if rest == "" {
		return true
	}

	r, _ := utf8.DecodeRuneInString(rest)
	if unicode.In(r, unicode.L, unicode.N) || strings.ContainsRune("_(-", r) {
		return false
	}

	return !(r == '.' && len(rest) > 1 && isAlnum(rest[1]))
}

// unbrokenLength is how many bytes at the start of s are neither whitespace, "<" nor ">".
func unbrokenLength(s string) int {
	end := strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '<' || r == '>' })
	if end < 0 {
		return len(s)
	}

	return end
}

func isAlnum(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func hasScheme(url string) bool {
	return len(url) >= 7 && strings.EqualFold(url[:7], "http://") || len(url) >= 8 && strings.EqualFold(url[:8], "https://")
}

// withoutTrailingPunctuation strips punctuation that ends a sentence rather than the URL, keeping closing brackets
// that pair with one inside it. Keep in sync with App\Services\Urls::withoutTrailingPunctuation().
func withoutTrailingPunctuation(url string) string {
	for {
		previous := url
		url = strings.TrimRight(url, ".,;:!?'\"…”’")
		for _, pair := range []string{"()", "[]"} {
			open, close := pair[:1], pair[1:]
			if strings.HasSuffix(url, close) && strings.Count(url, open) < strings.Count(url, close) {
				url = url[:len(url)-1]
			}
		}
		if url == previous {
			return url
		}
	}
}
