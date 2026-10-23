// Package config reads the server's settings from the environment, after
// loading a .env file when there is one. Every setting has one name, listed
// in .env.example at the repository root.
package config

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	// BaseURL is where people reach the app, with no trailing slash. MCP URLs,
	// OAuth issuers and the Google redirect are all derived from it.
	BaseURL string

	HTTPPort int
	// TLS: when enabled the app is served on HTTPSPort with a certificate
	// for Domain from Let's Encrypt (DNS-01 through Cloudflare, so neither
	// port has to be 80 or 443), and HTTPPort only redirects.
	SSLEnabled         bool
	HTTPSPort          int
	Domain             string
	SSLCacheDir        string
	LetsencryptEmail   string
	CloudflareAPIToken string
	ACMEDirectoryURL   string

	JWTSecret string
	// EncryptionKey is the 32-byte AES-256 key DSNs and tokens are sealed with.
	EncryptionKey []byte

	AllowedEmails []string

	GoogleClientID     string
	GoogleClientSecret string

	RootLoginEnabled bool
	RootToken        string
	RootEmail        string

	AppDBDriver string
	AppDBDSN    string

	PublicDir string

	TmphostAPIURL string

	QueryMaxRows    int
	QueryInlineRows int
	QueryTimeout    time.Duration
}

// CookieSecure is true when the app is served over https.
func (c Config) CookieSecure() bool {
	return strings.HasPrefix(c.BaseURL, "https://")
}

// Load reads the environment, after .env when the file exists.
func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("config: .env: %w", err)
	}
	return FromEnv(os.Getenv)
}

// FromEnv builds the configuration from getenv, and refuses one the server
// cannot run safely with.
func FromEnv(getenv func(string) string) (Config, error) {
	e := env{getenv: getenv}
	cfg := Config{
		BaseURL:            strings.TrimRight(e.str("APP_BASE_URL", "http://localhost:3000"), "/"),
		HTTPPort:           e.int("HTTP_PORT", 3000),
		SSLEnabled:         e.bool("SSL_ENABLED", false),
		HTTPSPort:          e.int("HTTPS_PORT", 3443),
		Domain:             strings.ToLower(e.str("APP_DOMAIN", "")),
		SSLCacheDir:        e.str("SSL_CACHE_DIR", "./_data/certs"),
		LetsencryptEmail:   e.str("LETSENCRYPT_EMAIL", ""),
		CloudflareAPIToken: e.str("CLOUDFLARE_API_TOKEN", ""),
		ACMEDirectoryURL:   e.str("ACME_DIRECTORY_URL", "https://acme-v02.api.letsencrypt.org/directory"),
		JWTSecret:          e.str("JWT_SECRET", ""),
		AllowedEmails:      splitList(e.str("ALLOWED_EMAILS", "")),
		GoogleClientID:     e.str("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: e.str("GOOGLE_CLIENT_SECRET", ""),
		RootLoginEnabled:   e.bool("ROOT_LOGIN_ENABLED", false),
		RootToken:          e.str("ROOT_TOKEN", ""),
		RootEmail:          strings.ToLower(e.str("ROOT_EMAIL", "root@sqlmcp.local")),
		AppDBDriver:        e.str("APP_DB_DRIVER", "sqlite"),
		AppDBDSN:           e.str("APP_DB_DSN", "./_data/sqlmcp.db"),
		PublicDir:          e.str("PUBLIC_DIR", "./public"),
		TmphostAPIURL:      strings.TrimRight(e.str("TMPHOST_API_URL", "https://app.tmphost.io"), "/"),
		QueryMaxRows:       e.int("QUERY_MAX_ROWS", 1000),
		QueryInlineRows:    e.int("QUERY_INLINE_ROWS", 25),
		QueryTimeout:       e.duration("QUERY_TIMEOUT", 30*time.Second),
	}
	if e.err != nil {
		return Config{}, e.err
	}

	key, err := decodeKey(e.str("ENCRYPTION_KEY", ""))
	if err != nil {
		return Config{}, err
	}
	cfg.EncryptionKey = key

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	// Every token is signed with this; a short one can be brute-forced
	// offline from any token a client holds.
	if len(c.JWTSecret) < 32 {
		return errors.New("config: JWT_SECRET must be at least 32 characters")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("config: APP_BASE_URL %q must be an absolute http(s) URL", c.BaseURL)
	}
	if c.SSLEnabled {
		if c.Domain == "" {
			return errors.New("config: APP_DOMAIN is required when SSL_ENABLED is true")
		}
		if c.CloudflareAPIToken == "" {
			return errors.New("config: CLOUDFLARE_API_TOKEN is required when SSL_ENABLED is true")
		}
	}
	// The root token also opens the admin setup at /admin.
	if (c.RootLoginEnabled || c.RootToken != "") && len(c.RootToken) < 16 {
		return errors.New("config: ROOT_TOKEN must be at least 16 characters (it opens the admin setup and, with ROOT_LOGIN_ENABLED, root sign-in)")
	}
	if c.AppDBDriver != "sqlite" && c.AppDBDriver != "postgres" {
		return fmt.Errorf("config: APP_DB_DRIVER %q must be sqlite or postgres", c.AppDBDriver)
	}
	if c.QueryMaxRows < 1 || c.QueryInlineRows < 1 {
		return errors.New("config: QUERY_MAX_ROWS and QUERY_INLINE_ROWS must be positive")
	}
	return nil
}

// decodeKey accepts the 32-byte key as 64 hex characters or as base64.
func decodeKey(raw string) ([]byte, error) {
	if raw == "" {
		return nil, errors.New("config: ENCRYPTION_KEY is required (32 bytes, hex or base64; `openssl rand -hex 32`)")
	}
	if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, errors.New("config: ENCRYPTION_KEY must decode to exactly 32 bytes (hex or base64)")
}

// splitList splits a comma separated list, dropping blanks.
func splitList(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// env reads typed values, keeping the first error so Load reports one.
type env struct {
	getenv func(string) string
	err    error
}

func (e *env) str(key, def string) string {
	if v := strings.TrimSpace(e.getenv(key)); v != "" {
		return v
	}
	return def
}

func (e *env) int(key string, def int) int {
	raw := e.str(key, "")
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil && e.err == nil {
		e.err = fmt.Errorf("config: %s must be an integer", key)
	}
	return v
}

func (e *env) bool(key string, def bool) bool {
	raw := e.str(key, "")
	if raw == "" {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil && e.err == nil {
		e.err = fmt.Errorf("config: %s must be true or false", key)
	}
	return v
}

func (e *env) duration(key string, def time.Duration) time.Duration {
	raw := e.str(key, "")
	if raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil && e.err == nil {
		e.err = fmt.Errorf("config: %s must be a duration such as 30s", key)
	}
	return v
}
