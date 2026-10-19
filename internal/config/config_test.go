package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSavesTheKeyPathRelativeToTheConfigDirectory(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}

	keyPath := filepath.Join(dir, "id_ed25519")
	if err := (&Config{BaseURL: DefaultBaseURL, KeyPath: keyPath}).Save(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"key_path": "id_ed25519"`) {
		t.Errorf("config.json = %s", data)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KeyPath != keyPath {
		t.Errorf("KeyPath = %q, want %q", cfg.KeyPath, keyPath)
	}
}

func TestKeepsAKeyOutsideTheConfigDirectoryAbsolute(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err := (&Config{KeyPath: keyPath}).Save(); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KeyPath != keyPath {
		t.Errorf("KeyPath = %q, want %q", cfg.KeyPath, keyPath)
	}
}

func TestLoadKeyDoesNotMakeAKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id_ed25519")

	if _, err := LoadKey(path); !os.IsNotExist(err) {
		t.Fatalf("LoadKey = %v, want a not-exist error", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("LoadKey made a key")
	}

	if _, err := LoadOrCreateKey(path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKey(path); err != nil {
		t.Errorf("LoadKey after LoadOrCreateKey: %v", err)
	}
}
