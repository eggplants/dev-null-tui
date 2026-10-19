// Package config keeps the signing key and the signed-in account under the user's config directory.
package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

const DefaultBaseURL = "https://dev-null.sh"

type Config struct {
	BaseURL string `json:"base_url"`
	// KeyPath is the private key's path, relative to the config directory when the key is in it, so the directory
	// can be mounted elsewhere, as in a container.
	KeyPath  string `json:"key_path"`
	Username string `json:"username,omitempty"`
}

func Dir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "devnull-tui"), nil
}

func path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "config.json"), nil
}

// Load returns the saved config, or os.ErrNotExist when nobody signed in yet.
func Load() (*Config, error) {
	p, err := path()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(cfg.KeyPath) {
		cfg.KeyPath = filepath.Join(filepath.Dir(p), cfg.KeyPath)
	}

	return &cfg, nil
}

func (c *Config) Save() error {
	p, err := path()
	if err != nil {
		return err
	}

	saved := *c
	if rel, err := filepath.Rel(filepath.Dir(p), c.KeyPath); err == nil && filepath.IsLocal(rel) {
		saved.KeyPath = rel
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(p, append(data, '\n'), 0o600)
}

// LoadKey reads the private key at path.
func LoadKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return ssh.ParsePrivateKey(data)
}

// LoadOrCreateKey reads the private key at path, or makes a new ed25519 key there when there is none.
func LoadOrCreateKey(path string) (ssh.Signer, error) {
	signer, err := LoadKey(path)
	if !errors.Is(err, os.ErrNotExist) {
		return signer, err
	}

	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	block, err := ssh.MarshalPrivateKey(key, "devnull-tui")
	if err != nil {
		return nil, err
	}

	signer, err = ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path+".pub", ssh.MarshalAuthorizedKey(signer.PublicKey()), 0o644); err != nil {
		return nil, err
	}

	return signer, nil
}
