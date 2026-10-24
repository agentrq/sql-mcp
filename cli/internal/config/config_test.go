package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestPath(t *testing.T) {
	p, err := Path(env(map[string]string{EnvConfig: "/x/c.json"}))
	require.NoError(t, err)
	assert.Equal(t, "/x/c.json", p)

	t.Setenv("XDG_CONFIG_HOME", "/home/ann/.config")
	t.Setenv("HOME", "/home/ann")
	t.Setenv("AppData", `C:\Users\ann\AppData\Roaming`)
	p, err = Path(env(nil))
	require.NoError(t, err)
	assert.Equal(t, "config.json", filepath.Base(p))
	assert.Equal(t, "sqlmcp", filepath.Base(filepath.Dir(p)))

	old := userConfigDir
	t.Cleanup(func() { userConfigDir = old })
	userConfigDir = func() (string, error) { return "", errors.New("no home") }
	_, err = Path(env(nil))
	assert.ErrorContains(t, err, EnvConfig)
	_, err = Load(env(nil))
	assert.Error(t, err)
	assert.Error(t, Save(env(nil), File{}))
}

func TestLoadSave(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "nested", "config.json")
	getenv := env(map[string]string{EnvConfig: p})

	f, err := Load(getenv)
	require.NoError(t, err)
	assert.Equal(t, File{}, f, "a missing file is empty")

	require.NoError(t, Save(getenv, File{Server: "https://sql.example.com/", Login: "v1.sealed"}))
	info, err := os.Stat(p)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	f, err = Load(getenv)
	require.NoError(t, err)
	assert.Equal(t, File{Server: "https://sql.example.com", Login: "v1.sealed"}, f)

	// An older, wider file is tightened.
	require.NoError(t, os.Chmod(p, 0o644))
	require.NoError(t, Save(getenv, f))
	info, _ = os.Stat(p)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.NoError(t, os.WriteFile(p, []byte("{"), 0o600))
	_, err = Load(getenv)
	assert.ErrorContains(t, err, "parse config")

	// A directory where the file should be.
	_, err = Load(env(map[string]string{EnvConfig: dir}))
	assert.ErrorContains(t, err, "read config")
	assert.ErrorContains(t, Save(env(map[string]string{EnvConfig: dir}), f), "write config")
	// A file where the directory should be.
	assert.ErrorContains(t, Save(env(map[string]string{EnvConfig: filepath.Join(p, "c.json")}), f), "config directory")
}
