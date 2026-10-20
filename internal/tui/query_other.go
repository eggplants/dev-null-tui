//go:build !unix

package tui

// queryTerminal can't read the terminal's answers here, so no graphics protocol is found; DN_GRAPHICS can still pick
// one.
func queryTerminal(string) []byte { return nil }
