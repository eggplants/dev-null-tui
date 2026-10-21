package tui

import (
	"errors"
	"image"
	"strings"
	"testing"
)

// fakeTmux answers tmux's commands from a table, by their arguments joined with spaces.
func fakeTmux(answers map[string]string) func(args ...string) (string, error) {
	return func(args ...string) (string, error) {
		if out, ok := answers[strings.Join(args, " ")]; ok {
			return out, nil
		}
		return "", errors.New("unknown command")
	}
}

func TestDetectsTmuxPassthrough(t *testing.T) {
	env := func(vars map[string]string) func(string) string { return func(k string) string { return vars[k] } }
	inTmux := env(map[string]string{"TMUX": "/tmp/tmux-501/default,1,0", "TMUX_PANE": "%3"})

	if detectTmux(env(nil), fakeTmux(nil)) != nil {
		t.Error("found tmux outside it")
	}
	if detectTmux(inTmux, fakeTmux(map[string]string{"show-options -t %3 -pAqv allow-passthrough": "off"})) != nil {
		t.Error("went through tmux with passthrough off")
	}

	tmux := detectTmux(inTmux, fakeTmux(map[string]string{
		"show-options -t %3 -pAqv allow-passthrough":                          "on",
		"display-message -t %3 -p #{pane_top} #{pane_left}":                   "5 40",
		"display-message -t %3 -p #{client_termname}":                         "xterm-ghostty",
		"display-message -t %3 -p #{client_cell_width} #{client_cell_height}": "9 18",
	}))
	if tmux == nil || tmux.top != 5 || tmux.left != 40 {
		t.Fatalf("detectTmux = %+v", tmux)
	}
	if p := tmux.outerProtocol(); p != graphicsKitty {
		t.Errorf("outerProtocol = %s", p)
	}
	if w, h := tmux.cellSize(); w != 9 || h != 18 {
		t.Errorf("cellSize = %d, %d", w, h)
	}

	if detectTmux(inTmux, fakeTmux(map[string]string{"show-options -t %3 -pAqv allow-passthrough": "all"})) == nil {
		t.Error("passthrough set to all didn't count")
	}
}

func TestWrapsSequencesForTmux(t *testing.T) {
	tmux := &tmuxPassthrough{}
	got := string(tmux.wrap([]byte("\x1b7\x1b[2;3H\x1b_Gm=1;AA\x1b\\\x1b_Gm=0;BB\x1b\\\x1b8")))
	want := "\x1bPtmux;\x1b\x1b7\x1b\x1b[2;3H\x1b\x1b_Gm=1;AA\x1b\x1b\\\x1b\\" +
		"\x1bPtmux;\x1b\x1b_Gm=0;BB\x1b\x1b\\\x1b\\" +
		"\x1bPtmux;\x1b\x1b8\x1b\\"
	if got != want {
		t.Errorf("wrap =\n%q\nwant\n%q", got, want)
	}
}

func TestGraphicsScreenDrawsThroughTmux(t *testing.T) {
	gs, _, tty := newTestGraphicsScreen(t, graphicsKitty)
	gs.tmux = &tmuxPassthrough{run: fakeTmux(map[string]string{"display-message -p #{pane_top} #{pane_left}": "3 10"})}

	g := gs.encode(image.NewRGBA(image.Rect(0, 0, 4, 4)), 20, 40)
	gs.Clear()
	gs.place(g, 2, 1, 2, 2)
	gs.Show()

	// The pane starts 3 rows down and 10 columns in, so the image's cell 2, 1 is the terminal's 13, 4.
	if out := tty.take(); !strings.HasPrefix(out, "\x1bPtmux;\x1b\x1b7\x1b\x1b[5;13H\x1b\x1b_Ga=T") {
		t.Errorf("wrote %q", out[:min(len(out), 60)])
	}

	gs.Clear()
	gs.Show()
	if out := tty.take(); !strings.Contains(out, "\x1bPtmux;\x1b\x1b_Ga=d,d=I,q=2,i=1\x1b\x1b\\\x1b\\") {
		t.Errorf("taking the image away wrote %q", out)
	}
}
