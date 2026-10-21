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

func TestSetSettings(t *testing.T) {
	s := &Settings{}
	for name, value := range map[string]string{"theme": "light", "graphics": "none", "poll_interval": "1m", "mouse": "false", "inline_images": "false"} {
		if err := s.Set(name, value); err != nil {
			t.Errorf("Set(%s, %s): %v", name, value, err)
		}
	}
	e, err := s.Resolve(func(string) string { return "" })
	if err != nil || e.Theme != "light" || e.Graphics != "none" || e.PollInterval != time.Minute || e.Mouse || e.InlineImages {
		t.Errorf("resolved %+v, %v", e, err)
	}

	for name, value := range map[string]string{"theme": "blue", "poll_interval": "1s", "mouse": "maybe", "colour": "red"} {
		if err := s.Set(name, value); err == nil {
			t.Errorf("Set(%s, %s) was taken", name, value)
		}
	}
	if s.Theme != "light" {
		t.Errorf("a refused value changed the settings: %+v", s)
	}
}
