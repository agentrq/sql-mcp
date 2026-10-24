// Package config loads and saves what the CLI keeps between runs: the
// server it signed in to and its login, sealed to this machine.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Environment variables the CLI reads.
const (
	EnvConfig = "SQLMCP_CONFIG"
	EnvServer = "SQLMCP_SERVER"
)

// userConfigDir is swapped in tests to make it fail.
var userConfigDir = os.UserConfigDir

// File is the config file. Its login is sealed (machine.Sealer) with the
// server as context, so the file alone gives nobody the tokens.
type File struct {
	Server string `json:"server,omitempty"`
	Login  string `json:"login,omitempty"`
}

// Login is what a sign-in grants: tokens for Server and nothing else, and
// the databases they open.
type Login struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	Expiry       time.Time `json:"expiry"`
	Databases    []string  `json:"databases"`
}

// Path is $SQLMCP_CONFIG, or sqlmcp/config.json under the user's config
// directory.
func Path(getenv func(string) string) (string, error) {
	if p := getenv(EnvConfig); p != "" {
		return p, nil
	}
	dir, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate the config directory (or set %s): %w", EnvConfig, err)
	}
	return filepath.Join(dir, "sqlmcp", "config.json"), nil
}

// Load reads the config file; a missing one is empty.
func Load(getenv func(string) string) (File, error) {
	var f File
	p, err := Path(getenv)
	if err != nil {
		return f, err
	}
	raw, err := os.ReadFile(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return f, nil
	case err != nil:
		return f, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return f, fmt.Errorf("parse config %s: %w", p, err)
	}
	f.Server = strings.TrimRight(f.Server, "/")
	return f, nil
}

// Save writes the config file readable only by its owner.
func Save(getenv func(string) string, f File) error {
	p, err := Path(getenv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("create the config directory: %w", err)
	}
	// A struct of strings always marshals.
	raw, _ := json.MarshalIndent(f, "", "  ")
	if err := os.WriteFile(p, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	// WriteFile sets the mode of a new file only; tighten an older one.
	return os.Chmod(p, 0o600)
}
