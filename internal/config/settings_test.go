package config

import (
	"testing"
	"time"
)

func TestResolveSettings(t *testing.T) {
	off := false
	s := &Settings{Theme: "light", PollInterval: "1m", Mouse: &off}
	env := map[string]string{"DN_THEME": "dark"}

	e, err := s.Resolve(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if e.Theme != "dark" || e.Graphics != "auto" || e.PollInterval != time.Minute || e.Mouse {
		t.Errorf("resolved %+v", e)
	}

	sources := map[string]string{}
	for _, s := range e.Sources {
		sources[s.Name] = s.Source
	}
	if sources["theme"] != "$DN_THEME" || sources["poll_interval"] != "settings.json" || sources["graphics"] != "default" {
		t.Errorf("sources %v", sources)
	}
}

func TestResolveRefusesBadSettings(t *testing.T) {
	for _, s := range []*Settings{{Theme: "blue"}, {PollInterval: "1s"}, {Graphics: "iterm"}} {
		if _, err := s.Resolve(func(string) string { return "" }); err == nil {
			t.Errorf("%+v was taken", s)
		}
	}
}
