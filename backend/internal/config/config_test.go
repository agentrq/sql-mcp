package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	hexKey    = strings.Repeat("ab", 32)
	base64Key = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	secret    = strings.Repeat("s", 32)
)

// getenv serves vars on top of the minimum a valid configuration needs.
func getenv(vars map[string]string) func(string) string {
	all := map[string]string{"JWT_SECRET": secret, "ENCRYPTION_KEY": hexKey}
	for k, v := range vars {
		all[k] = v
	}
	return func(k string) string { return all[k] }
}

func TestFromEnvDefaults(t *testing.T) {
	cfg, err := FromEnv(getenv(nil))
	require.NoError(t, err)

	assert.Equal(t, "http://localhost:3000", cfg.BaseURL)
	assert.Equal(t, 3000, cfg.HTTPPort)
	assert.False(t, cfg.SSLEnabled)
	assert.Equal(t, 3443, cfg.HTTPSPort)
	assert.Equal(t, "./_data/certs", cfg.SSLCacheDir)
	assert.Equal(t, "https://acme-v02.api.letsencrypt.org/directory", cfg.ACMEDirectoryURL)
	assert.Nil(t, cfg.AllowedEmails)
	assert.Equal(t, "root@sqlmcp.local", cfg.RootEmail)
	assert.Equal(t, "sqlite", cfg.AppDBDriver)
	assert.Equal(t, "./_data/sqlmcp.db", cfg.AppDBDSN)
	assert.Equal(t, "./public", cfg.PublicDir)
	assert.Equal(t, "https://app.tmphost.io", cfg.TmphostAPIURL)
	assert.Equal(t, 1000, cfg.QueryMaxRows)
	assert.Equal(t, 25, cfg.QueryInlineRows)
	assert.Equal(t, 30*time.Second, cfg.QueryTimeout)
	assert.Len(t, cfg.EncryptionKey, 32)
	assert.False(t, cfg.CookieSecure())
}

func TestFromEnvOverrides(t *testing.T) {
	cfg, err := FromEnv(getenv(map[string]string{
		"APP_BASE_URL":         "https://sql.example.com/",
		"HTTP_PORT":            "8080",
		"SSL_ENABLED":          "true",
		"HTTPS_PORT":           "8443",
		"APP_DOMAIN":           "SQL.Example.com",
		"CLOUDFLARE_API_TOKEN": "cf",
		"ALLOWED_EMAILS":       " a@x.com, ,*@y.com ",
		"ROOT_LOGIN_ENABLED":   "1",
		"ROOT_TOKEN":           strings.Repeat("r", 16),
		"ROOT_EMAIL":           "Root@X.com",
		"APP_DB_DRIVER":        "postgres",
		"TMPHOST_API_URL":      "https://tmp.example.com/",
		"QUERY_MAX_ROWS":       "5",
		"QUERY_INLINE_ROWS":    "2",
		"QUERY_TIMEOUT":        "1m",
		"ENCRYPTION_KEY":       base64Key,
	}))
	require.NoError(t, err)

	assert.Equal(t, "https://sql.example.com", cfg.BaseURL)
	assert.True(t, cfg.CookieSecure())
	assert.Equal(t, 8080, cfg.HTTPPort)
	assert.True(t, cfg.SSLEnabled)
	assert.Equal(t, 8443, cfg.HTTPSPort)
	assert.Equal(t, "sql.example.com", cfg.Domain)
	assert.Equal(t, []string{"a@x.com", "*@y.com"}, cfg.AllowedEmails)
	assert.True(t, cfg.RootLoginEnabled)
	assert.Equal(t, "root@x.com", cfg.RootEmail)
	assert.Equal(t, "postgres", cfg.AppDBDriver)
	assert.Equal(t, "https://tmp.example.com", cfg.TmphostAPIURL)
	assert.Equal(t, 5, cfg.QueryMaxRows)
	assert.Equal(t, 2, cfg.QueryInlineRows)
	assert.Equal(t, time.Minute, cfg.QueryTimeout)
	assert.Equal(t, []byte(strings.Repeat("k", 32)), cfg.EncryptionKey)
}

func TestFromEnvRefuses(t *testing.T) {
	for _, tc := range []struct {
		name string
		vars map[string]string
		want string
	}{
		{"bad int", map[string]string{"HTTP_PORT": "x"}, "HTTP_PORT must be an integer"},
		{"first error wins", map[string]string{"HTTP_PORT": "x", "HTTPS_PORT": "y"}, "HTTP_PORT must be an integer"},
		{"bad bool", map[string]string{"SSL_ENABLED": "maybe"}, "SSL_ENABLED must be true or false"},
		{"bad duration", map[string]string{"QUERY_TIMEOUT": "30"}, "QUERY_TIMEOUT must be a duration"},
		{"no key", map[string]string{"ENCRYPTION_KEY": ""}, "ENCRYPTION_KEY is required"},
		{"short hex key", map[string]string{"ENCRYPTION_KEY": "abcd"}, "exactly 32 bytes"},
		{"short base64 key", map[string]string{"ENCRYPTION_KEY": base64.StdEncoding.EncodeToString([]byte("short"))}, "exactly 32 bytes"},
		{"not a key", map[string]string{"ENCRYPTION_KEY": "!!!"}, "exactly 32 bytes"},
		{"short secret", map[string]string{"JWT_SECRET": "short"}, "JWT_SECRET must be at least 32"},
		{"relative base url", map[string]string{"APP_BASE_URL": "/app"}, "APP_BASE_URL"},
		{"ftp base url", map[string]string{"APP_BASE_URL": "ftp://x.com"}, "APP_BASE_URL"},
		{"unparsable base url", map[string]string{"APP_BASE_URL": "http://[::1"}, "APP_BASE_URL"},
		{"ssl without domain", map[string]string{"SSL_ENABLED": "true", "CLOUDFLARE_API_TOKEN": "cf"}, "APP_DOMAIN is required"},
		{"ssl without token", map[string]string{"SSL_ENABLED": "true", "APP_DOMAIN": "x.com"}, "CLOUDFLARE_API_TOKEN is required"},
		{"short root token", map[string]string{"ROOT_LOGIN_ENABLED": "true", "ROOT_TOKEN": "short"}, "ROOT_TOKEN must be at least 16"},
		{"unknown driver", map[string]string{"APP_DB_DRIVER": "mysql"}, "APP_DB_DRIVER"},
		{"zero max rows", map[string]string{"QUERY_MAX_ROWS": "0"}, "must be positive"},
		{"zero inline rows", map[string]string{"QUERY_INLINE_ROWS": "-1"}, "must be positive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := FromEnv(getenv(tc.vars))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestLoad(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("JWT_SECRET", secret)
	t.Setenv("ENCRYPTION_KEY", hexKey)
	t.Setenv("HTTP_PORT", "")
	require.NoError(t, os.Unsetenv("HTTP_PORT")) // restored by t.Setenv's cleanup

	// No .env is fine.
	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 3000, cfg.HTTPPort)

	// A .env fills what the environment leaves unset.
	require.NoError(t, os.WriteFile(".env", []byte("HTTP_PORT=4000\n"), 0o600))
	cfg, err = Load()
	require.NoError(t, err)
	assert.Equal(t, 4000, cfg.HTTPPort)

	// A .env that cannot be read is an error, not silently skipped.
	require.NoError(t, os.Remove(".env"))
	require.NoError(t, os.Mkdir(filepath.Join(".", ".env"), 0o700))
	_, err = Load()
	assert.ErrorContains(t, err, "config: .env")
}
