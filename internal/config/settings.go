package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Settings are how the TUI looks and behaves, kept in settings.json next to the config. A field left out takes its
// default, and an environment variable overrides the file.
type Settings struct {
	// Theme is "auto", "dark" or "light". DN_THEME overrides it.
	Theme string `json:"theme,omitempty"`
	// Graphics is "auto", "kitty", "sixel" or "none". DN_GRAPHICS overrides it.
	Graphics string `json:"graphics,omitempty"`
	// PollInterval is how often new commits and notifications are checked for, like "30s". DN_POLL_INTERVAL
	// overrides it.
	PollInterval string `json:"poll_interval,omitempty"`
	// Mouse turns the mouse on. DN_MOUSE overrides it.
	Mouse *bool `json:"mouse,omitempty"`
	// InlineImages shows commits' images in the lists, not only behind i. DN_INLINE_IMAGES overrides it.
	InlineImages *bool `json:"inline_images,omitempty"`
}

// Setting is a setting's effective value and where it came from: "default", "settings.json" or an environment
// variable.
type Setting struct {
	Name, Value, Source string
}

// Effective are the settings after the defaults, the file and the environment, in that order.
type Effective struct {
	Theme        string
	Graphics     string
	PollInterval time.Duration
	Mouse        bool
	InlineImages bool
	Sources      []Setting
}

func SettingsPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "settings.json"), nil
}

// DraftPath is where the TUI keeps a new commit's draft.
func DraftPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "draft.txt"), nil
}

// LoadSettings reads settings.json, which may be missing. Unknown fields are an error, so a typo doesn't go unseen.
func LoadSettings() (*Settings, error) {
	p, err := SettingsPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &Settings{}, nil
	}
	if err != nil {
		return nil, err
	}

	var s Settings
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}

	return &s, nil
}

// Save writes the settings to settings.json.
func (s *Settings) Save() error {
	p, err := SettingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(p, append(data, '\n'), 0o600)
}

// Resolve applies the file and then the environment over the defaults, checking each value.
func (s *Settings) Resolve(getenv func(string) string) (*Effective, error) {
	e := &Effective{Theme: "auto", Graphics: "auto", PollInterval: 30 * time.Second, Mouse: true, InlineImages: true}

	pick := func(name, env, file, def string, valid func(string) error) (string, error) {
		value, source := def, "default"
		if file != "" {
			value, source = file, "settings.json"
		}
		if v := getenv(env); v != "" {
			value, source = v, "$"+env
		}
		if err := valid(value); err != nil {
			return "", fmt.Errorf("%s (from %s): %w", name, source, err)
		}
		e.Sources = append(e.Sources, Setting{Name: name, Value: value, Source: source})

		return value, nil
	}
	oneOf := func(options ...string) func(string) error {
		return func(v string) error {
			for _, o := range options {
				if v == o {
					return nil
				}
			}
			return fmt.Errorf("%q isn't one of %q", v, options)
		}
	}

	var err error
	if e.Theme, err = pick("theme", "DN_THEME", s.Theme, "auto", oneOf("auto", "dark", "light")); err != nil {
		return nil, err
	}
	if e.Graphics, err = pick("graphics", "DN_GRAPHICS", s.Graphics, "auto", oneOf("auto", "kitty", "sixel", "none")); err != nil {
		return nil, err
	}

	interval, err := pick("poll_interval", "DN_POLL_INTERVAL", s.PollInterval, "30s", func(v string) error {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		if d < 10*time.Second || d > 10*time.Minute {
			return errors.New("it must be from 10s to 10m")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	e.PollInterval, _ = time.ParseDuration(interval)

	boolean := func(name, env string, file *bool) (bool, error) {
		value := ""
		if file != nil {
			value = strconv.FormatBool(*file)
		}
		v, err := pick(name, env, value, "true", func(v string) error {
			_, err := strconv.ParseBool(v)
			return err
		})
		if err != nil {
			return false, err
		}
		b, _ := strconv.ParseBool(v)
		return b, nil
	}
	if e.Mouse, err = boolean("mouse", "DN_MOUSE", s.Mouse); err != nil {
		return nil, err
	}
	if e.InlineImages, err = boolean("inline_images", "DN_INLINE_IMAGES", s.InlineImages); err != nil {
		return nil, err
	}

	return e, nil
}
