package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/eggplants/dev-null-tui/internal/commit"
)

func TestParseCommitArgs(t *testing.T) {
	for _, tt := range []struct {
		args    []string
		reply   string
		images  []string
		rest    []string
		help    bool
		wantErr string
	}{
		{args: []string{"Hello", "world!"}, rest: []string{"Hello", "world!"}},
		{args: []string{"-r", "0be0130", "Agreed!"}, reply: "0be0130", rest: []string{"Agreed!"}},
		{args: []string{"--reply=0be0130", "Agreed!"}, reply: "0be0130", rest: []string{"Agreed!"}},
		{args: []string{"-r=0be0130", "Agreed!"}, reply: "0be0130", rest: []string{"Agreed!"}},
		{args: []string{"-i", "a.png", "--image", "b.png", "-i=c.png", "Shipped!"}, images: []string{"a.png", "b.png", "c.png"}, rest: []string{"Shipped!"}},
		{args: []string{"-i", "a.png", "-r", "0be0130", "both"}, reply: "0be0130", images: []string{"a.png"}, rest: []string{"both"}},
		{args: []string{"--", "-r", "is a flag"}, rest: []string{"-r", "is a flag"}},
		{args: []string{"-it", "works!"}, rest: []string{"-it", "works!"}},
		{args: []string{"say", "-r", "x"}, rest: []string{"say", "-r", "x"}},
		{args: []string{"-h"}, help: true},
		{args: []string{"-r"}, wantErr: "-r needs a value"},
		{args: []string{"-i", ""}, wantErr: "-i needs a value"},
		{args: []string{"-r", "a", "--reply", "b", "x"}, wantErr: "only be given once"},
		{args: nil},
	} {
		var opts commit.Options
		rest, help, err := parseCommitArgs(tt.args, &opts)

		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%q: err = %v, want %q", tt.args, err, tt.wantErr)
			}
			continue
		}
		if err != nil || help != tt.help || opts.ReplyTo != tt.reply || !slices.Equal(opts.Images, tt.images) || !slices.Equal(rest, tt.rest) {
			t.Errorf("%q: rest %q, help %v, reply %q, images %q, err %v", tt.args, rest, help, opts.ReplyTo, opts.Images, err)
		}
	}
}
