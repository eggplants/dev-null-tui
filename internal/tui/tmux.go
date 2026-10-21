package tui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// tmuxPassthrough gets images to the terminal tmux runs in, which tmux would otherwise keep them from: each sequence
// goes wrapped in tmux's passthrough, which its allow-passthrough option lets through, and is placed by where the
// pane sits in that terminal, since tmux's cursor isn't the terminal's.
type tmuxPassthrough struct {
	// run runs tmux with args and returns what it printed.
	run  func(args ...string) (string, error)
	pane string
	// top and left are where the pane starts in the terminal, in cells.
	top, left int
}

// runTmux runs the tmux command, giving up after a moment.
func runTmux(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "tmux", args...).Output()

	return strings.TrimSpace(string(out)), err
}

// detectTmux tells whether dn runs in tmux with passthrough allowed, and if so how to get images through it.
func detectTmux(getenv func(string) string, run func(args ...string) (string, error)) *tmuxPassthrough {
	if getenv("TMUX") == "" {
		return nil
	}

	t := &tmuxPassthrough{run: run, pane: getenv("TMUX_PANE")}
	// allow-passthrough is a pane option, which -A shows as the pane gets it, from its window or set -g too.
	allowed, _ := t.query("show-options", "-pAqv", "allow-passthrough")
	if allowed != "on" && allowed != "all" {
		return nil
	}
	t.refresh()

	return t
}

// query runs a tmux command about dn's pane.
func (t *tmuxPassthrough) query(command string, args ...string) (string, error) {
	full := []string{command}
	if t.pane != "" {
		full = append(full, "-t", t.pane)
	}

	return t.run(append(full, args...)...)
}

// outerProtocol guesses how the terminal tmux runs in draws images from its name, since tmux keeps the terminal's
// answer to the kitty query to itself. Kitty, Ghostty and WezTerm speak the kitty protocol.
func (t *tmuxPassthrough) outerProtocol() graphicsProtocol {
	name, err := t.query("display-message", "-p", "#{client_termname}")
	if err != nil {
		return graphicsNone
	}

	for _, kitty := range []string{"kitty", "ghostty", "wezterm"} {
		if strings.Contains(strings.ToLower(name), kitty) {
			return graphicsKitty
		}
	}

	return graphicsNone
}

// cellSize is the size of the terminal's cells in pixels, which tmux knows from 3.4, or 0, 0.
func (t *tmuxPassthrough) cellSize() (int, int) {
	out, err := t.query("display-message", "-p", "#{client_cell_width} #{client_cell_height}")
	var width, height int
	if err != nil || len(strings.Fields(out)) != 2 {
		return 0, 0
	}
	fmt.Sscanf(out, "%d %d", &width, &height)

	return width, height
}

// refresh finds where the pane starts in the terminal again, as it moves when panes are split or resized.
func (t *tmuxPassthrough) refresh() {
	out, err := t.query("display-message", "-p", "#{pane_top} #{pane_left}")
	if err != nil {
		return
	}
	fmt.Sscanf(out, "%d %d", &t.top, &t.left)
}

// wrap puts escape sequences in tmux's passthrough, one passthrough each, doubling the ESCs inside as tmux wants.
func (t *tmuxPassthrough) wrap(sequences []byte) []byte {
	var out bytes.Buffer
	for _, seq := range splitSequences(sequences) {
		out.WriteString("\x1bPtmux;")
		out.Write(bytes.ReplaceAll(seq, []byte("\x1b"), []byte("\x1b\x1b")))
		out.WriteString("\x1b\\")
	}

	return out.Bytes()
}

// splitSequences splits escape sequences apart at each string terminator, ESC \, so no passthrough gets too long for
// tmux. What comes before the first ESC is left with it.
func splitSequences(data []byte) [][]byte {
	var seqs [][]byte
	for len(data) > 0 {
		end := bytes.Index(data, []byte("\x1b\\"))
		if end < 0 {
			seqs = append(seqs, data)
			break
		}
		seqs = append(seqs, data[:end+2])
		data = data[end+2:]
	}

	return seqs
}

// inTmux is detectTmux for the environment dn runs in.
func inTmux() *tmuxPassthrough {
	return detectTmux(os.Getenv, runTmux)
}
