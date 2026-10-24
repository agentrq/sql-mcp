package cmd

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentrq/sql-mcp/cli/internal/config"
	"github.com/agentrq/sql-mcp/cli/internal/machine"
)

func TestVersionAndHelp(t *testing.T) {
	e := newEnv(t, newServer(t))
	res := e.run("", "--version")
	assert.Equal(t, 0, res.code)
	assert.Contains(t, res.out, "v9.9.9")
	res = e.run("", "--help")
	assert.Contains(t, res.out, "never stored on tmphost")
	res = e.run("", "pii", "--help")
	assert.Contains(t, res.out, "can't unmark")
	res = e.run("", "nope")
	assert.Equal(t, 1, res.code)
	assert.Contains(t, res.err, `unknown command "nope"`)
}

func TestExecuteDefaultsTheHTTPClient(t *testing.T) {
	code := Execute([]string{"--version"}, Options{Out: &strings.Builder{}, Err: &strings.Builder{}, Getenv: os.Getenv, Version: "v1"})
	assert.Equal(t, 0, code)
}

func TestLoginSealsTheTokensToTheMachine(t *testing.T) {
	e, srv := loggedIn(t)
	raw, err := os.ReadFile(e.config)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), srv.access, "the tokens are not in the file in the clear")
	assert.NotContains(t, string(raw), srv.refresh)
	var f config.File
	require.NoError(t, json.Unmarshal(raw, &f))
	assert.Equal(t, srv.URL, f.Server)
	assert.True(t, strings.HasPrefix(f.Login, "v1."))

	res := e.run("", "whoami")
	require.Equal(t, 0, res.code, res.err)
	assert.Equal(t, "Logged in to "+srv.URL+" as ann@example.com\nDatabases: shop, crm, CRM\n", res.out)

	// Another machine, or another user, cannot open the login.
	e.key = []byte(strings.Repeat("o", 32))
	res = e.run("", "whoami")
	assert.Equal(t, 1, res.code)
	assert.Contains(t, res.err, "not made on this machine")
	assert.Contains(t, res.err, "sqlmcp login")

	// Nor can the login be pointed at another server.
	e.key = testKey
	f.Server = "https://evil.example.com"
	raw, _ = json.Marshal(f)
	require.NoError(t, os.WriteFile(e.config, raw, 0o600))
	res = e.run("", "whoami", "--server", "https://evil.example.com")
	assert.Contains(t, res.err, "not made on this machine")
}

func TestLoginOutput(t *testing.T) {
	srv := newServer(t)
	e := newEnv(t, srv)
	e.vars[config.EnvServer] = srv.URL + "/"
	res := e.run("", "login", "--json")
	require.Equal(t, 0, res.code, res.err)
	var v whoamiView
	require.NoError(t, json.Unmarshal([]byte(res.out), &v))
	assert.Equal(t, srv.URL, v.Server)
	assert.Equal(t, "ann@example.com", v.User.Email)
	assert.Len(t, v.Databases, 3)
	assert.Contains(t, res.err, "/cli/oauth2/authorize?", "the URL goes to stderr")
}

func TestLoginFailures(t *testing.T) {
	srv := newServer(t)
	e := newEnv(t, srv)
	res := e.run("", "login")
	assert.Contains(t, res.err, "name the server")

	e.browser = func(q url.Values) url.Values {
		return url.Values{"error": {"access_denied"}, "state": {q.Get("state")}}
	}
	res = e.run("", "login", "--server", srv.URL)
	assert.Contains(t, res.err, "sign-in failed: access_denied")

	e.browser = func(q url.Values) url.Values { return url.Values{"code": {"good"}, "state": {q.Get("state")}} }
	e.key = []byte("short")
	res = e.run("", "login", "--server", srv.URL)
	assert.Contains(t, res.err, "32 bytes")

	e.key = testKey
	srv.fail["GET /auth/user"] = true
	res = e.run("", "login", "--server", srv.URL)
	assert.Contains(t, res.err, "internal server error")
	delete(srv.fail, "GET /auth/user")
	srv.fail["GET /databases"] = true
	res = e.run("", "login", "--server", srv.URL)
	assert.Contains(t, res.err, "internal server error")
}

func TestKeyFailure(t *testing.T) {
	e, _ := loggedIn(t)
	var out, errOut strings.Builder
	code := Execute([]string{"whoami"}, Options{
		Out: &out, Err: &errOut, Getenv: func(k string) string { return e.vars[k] }, Now: time.Now,
		MachineKey: func() ([]byte, error) { return nil, machine.ErrOpen },
	})
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut.String(), "not made on this machine")
}

func TestConfigFailures(t *testing.T) {
	srv := newServer(t)
	e := newEnv(t, srv)
	// The config path is a directory: it can be neither read nor written.
	e.vars[config.EnvConfig] = t.TempDir()
	for _, args := range [][]string{{"login", "--server", srv.URL}, {"logout"}, {"whoami"}} {
		res := e.run("", args...)
		assert.Equal(t, 1, res.code, args)
		assert.Contains(t, res.err, "read config", args)
	}

	// Readable but not writable: login cannot save.
	dir := t.TempDir()
	e.vars[config.EnvConfig] = filepath.Join(dir, "config.json")
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	for _, args := range [][]string{{"login", "--server", srv.URL}, {"logout"}} {
		res := e.run("", args...)
		assert.Contains(t, res.err, "write config", args)
	}
}

func TestLogout(t *testing.T) {
	e, srv := loggedIn(t)
	res := e.run("", "logout")
	require.Equal(t, 0, res.code, res.err)
	assert.Equal(t, "Logged out.\n", res.out)
	raw, _ := os.ReadFile(e.config)
	assert.NotContains(t, string(raw), "v1.", "the login is gone")
	assert.Contains(t, string(raw), srv.URL, "the server stays")

	res = e.run("", "whoami")
	assert.Contains(t, res.err, "not logged in to "+srv.URL)
	res = e.run("", "logout", "--json")
	assert.JSONEq(t, `{"loggedOut":true}`, res.out)
}

func TestRefresh(t *testing.T) {
	e, srv := loggedIn(t)
	// An hour on, the access token has expired: it is refreshed first, and
	// the rotated tokens are saved sealed.
	e.now = now.Add(time.Hour)
	before, _ := os.ReadFile(e.config)
	res := e.run("", "whoami")
	require.Equal(t, 0, res.code, res.err)
	assert.Equal(t, 1, srv.refreshes)
	after, _ := os.ReadFile(e.config)
	assert.NotEqual(t, string(before), string(after))
	assert.NotContains(t, string(after), srv.refresh)

	// The server refuses a token the CLI thinks is good: refreshed once.
	srv.mu.Lock()
	srv.access = "revoked-elsewhere"
	srv.mu.Unlock()
	res = e.run("", "whoami")
	require.Equal(t, 0, res.code, res.err)
	assert.Equal(t, 2, srv.refreshes)

	// Refused: only a new login helps.
	srv.mu.Lock()
	srv.access, srv.failRefresh = "revoked-again", true
	srv.mu.Unlock()
	res = e.run("", "whoami")
	assert.Equal(t, 1, res.code)
	assert.Contains(t, res.err, "the login has expired")

	// The server is gone.
	e.now = now.Add(5 * time.Hour)
	srv.Close()
	res = e.run("", "whoami")
	assert.Contains(t, res.err, "refresh the login")
}

func TestRefreshCannotSave(t *testing.T) {
	e, srv := loggedIn(t)
	e.now = now.Add(time.Hour)
	require.NoError(t, os.Chmod(e.config, 0o400))
	res := e.run("", "whoami")
	require.Equal(t, 0, res.code, res.err)
	assert.Contains(t, res.err, "warning: could not save the refreshed login")
	assert.Equal(t, 1, srv.refreshes)
}

// An account removed after login: the refresh still mints tokens, the API
// refuses them, and the CLI says to log in again.
func TestAPI401Hint(t *testing.T) {
	e, srv := loggedIn(t)
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			apiError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		srv.token(w, r)
	})
	res := e.run("", "db", "ls")
	assert.Equal(t, 1, res.code)
	assert.Contains(t, res.err, `Run "sqlmcp login" to sign in again.`)
}

func TestOpenSessionNeedsJSON(t *testing.T) {
	e, srv := loggedIn(t)
	s, err := machine.NewSealer(testKey)
	require.NoError(t, err)
	raw, _ := json.Marshal(config.File{Server: srv.URL, Login: s.Seal([]byte("not json"), srv.URL)})
	require.NoError(t, os.WriteFile(e.config, raw, 0o600))
	res := e.run("", "whoami")
	assert.Contains(t, res.err, "not made on this machine")
}

func TestNoServerAnywhere(t *testing.T) {
	e := newEnv(t, newServer(t))
	res := e.run("", "whoami")
	assert.Contains(t, res.err, "name the server")
}
