package app_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/agentrq/sql-mcp/backend/internal/app"
	"github.com/agentrq/sql-mcp/backend/internal/config"
	"github.com/agentrq/sql-mcp/backend/internal/controller/crud"
	"github.com/agentrq/sql-mcp/backend/internal/repository"
	"github.com/agentrq/sql-mcp/backend/internal/service/auth"
	"github.com/agentrq/sql-mcp/backend/internal/service/dbdriver"
	"github.com/agentrq/sql-mcp/backend/internal/service/tmphost"
	"github.com/agentrq/sql-mcp/backend/internal/testutil"
)

const rootToken = "root-token-1234567890"

// google is a fake provider: the code is the email it signs in.
type google struct{}

func (google) AuthURL(state string) string { return "https://accounts.test/auth?state=" + state }
func (google) Exchange(_ context.Context, code string) (*auth.User, error) {
	if code == "broken" {
		return nil, errors.New("exchange failed")
	}
	return &auth.User{Email: code, VerifiedEmail: true, Name: "G"}, nil
}

type stack struct {
	t       *testing.T
	srv     *httptest.Server
	driver  *testutil.Driver
	tmphost *testutil.Tmphost
	public  string
	dbPath  string
}

func newStack(t *testing.T) *stack {
	t.Helper()
	s := &stack{t: t, driver: &testutil.Driver{Rows: 3}, tmphost: testutil.NewTmphost(t), public: t.TempDir(), dbPath: filepath.Join(t.TempDir(), "nested", "app.db")}
	var h http.Handler
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	t.Cleanup(s.srv.Close)
	cfg := config.Config{
		BaseURL:          s.srv.URL,
		JWTSecret:        testutil.Secret,
		EncryptionKey:    testutil.Key,
		AllowedEmails:    []string{"*@example.com", "root@sqlmcp.local"},
		RootLoginEnabled: true,
		RootToken:        rootToken,
		RootEmail:        "root@sqlmcp.local",
		AppDBDriver:      "sqlite",
		AppDBDSN:         s.dbPath,
		PublicDir:        s.public,
		QueryMaxRows:     100,
		QueryInlineRows:  5,
		QueryTimeout:     time.Second,
	}
	var err error
	h, err = app.New(cfg, app.Deps{
		Drivers: dbdriver.NewRegistry(s.driver, dbdriver.NewMySQL()),
		Tmphost: tmphost.New(s.tmphost.URL, s.tmphost.Client()),
		Google:  google{},
	})
	require.NoError(t, err)
	return s
}

// browser is a client with its own cookies that does not follow redirects.
func (s *stack) browser() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

type response struct {
	status int
	header http.Header
	body   string
}

func (r response) json(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(r.body), &out), r.body)
	return out
}

func (s *stack) do(c *http.Client, method, path string, body any, header ...string) response {
	s.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	case url.Values:
		rd = strings.NewReader(b.Encode())
	default:
		raw, _ := json.Marshal(b)
		rd = strings.NewReader(string(raw))
	}
	target := path
	if strings.HasPrefix(path, "/") {
		target = s.srv.URL + path
	}
	req, err := http.NewRequest(method, target, rd)
	require.NoError(s.t, err)
	switch body.(type) {
	case nil:
	case url.Values:
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	default:
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	res, err := c.Do(req)
	require.NoError(s.t, err)
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return response{status: res.StatusCode, header: res.Header, body: string(raw)}
}

func (s *stack) rootSession() *http.Client {
	c := s.browser()
	res := s.do(c, "POST", "/api/v1/auth/root/login", map[string]string{"rootToken": rootToken})
	require.Equal(s.t, http.StatusOK, res.status, res.body)
	return c
}

func (s *stack) googleSession(email string) *http.Client {
	c := s.browser()
	res := s.do(c, "GET", "/api/v1/auth/google/login?redirect_url=/databases", nil)
	require.Equal(s.t, http.StatusFound, res.status)
	u, _ := url.Parse(res.header.Get("Location"))
	res = s.do(c, "GET", "/api/v1/auth/google/callback?state="+url.QueryEscape(u.Query().Get("state"))+"&code="+url.QueryEscape(email), nil)
	require.Equal(s.t, http.StatusFound, res.status)
	require.Equal(s.t, "/databases", res.header.Get("Location"))
	return c
}

func (s *stack) createDB(c *http.Client) string {
	res := s.do(c, "POST", "/api/v1/databases", map[string]any{"database": map[string]string{"name": "shop", "driver": "postgres", "dsn": testutil.GoodDSN}})
	require.Equal(s.t, http.StatusCreated, res.status, res.body)
	return res.json(s.t)["database"].(map[string]any)["id"].(string)
}

func TestAuth(t *testing.T) {
	s := newStack(t)
	anon := s.browser()

	res := s.do(anon, "GET", "/api/v1/auth/config", nil)
	assert.JSONEq(t, `{"googleLoginEnabled":true,"rootLoginEnabled":true,"basePath":""}`, res.body)
	assert.Equal(t, http.StatusUnauthorized, s.do(anon, "GET", "/api/v1/auth/user", nil).status)
	assert.Equal(t, http.StatusUnauthorized, s.do(anon, "GET", "/api/v1/databases", nil).status)

	assert.Equal(t, http.StatusBadRequest, s.do(anon, "POST", "/api/v1/auth/root/login", "{", "Content-Type", "application/json").status)
	assert.Equal(t, http.StatusUnauthorized, s.do(anon, "POST", "/api/v1/auth/root/login", map[string]string{"rootToken": "wrong"}).status)

	c := s.rootSession()
	res = s.do(c, "GET", "/api/v1/auth/user", nil)
	require.Equal(t, http.StatusOK, res.status)
	assert.Equal(t, "root@sqlmcp.local", res.json(t)["email"])

	res = s.do(c, "POST", "/api/v1/auth/refresh", nil)
	assert.Equal(t, http.StatusOK, res.status)
	assert.Contains(t, res.header.Values("Set-Cookie")[0], "HttpOnly")

	assert.Equal(t, http.StatusNoContent, s.do(c, "POST", "/api/v1/auth/logout", nil).status)
	assert.Equal(t, http.StatusUnauthorized, s.do(c, "GET", "/api/v1/auth/user", nil).status)
	assert.Equal(t, http.StatusUnauthorized, s.do(c, "POST", "/api/v1/auth/refresh", nil).status)
}

func TestGoogleSignIn(t *testing.T) {
	s := newStack(t)
	c := s.googleSession("dev@example.com")
	assert.Equal(t, http.StatusOK, s.do(c, "GET", "/api/v1/auth/user", nil).status)

	b := s.browser()
	login := func(redirect string) string {
		res := s.do(b, "GET", "/api/v1/auth/google/login?redirect_url="+url.QueryEscape(redirect), nil)
		u, _ := url.Parse(res.header.Get("Location"))
		return url.QueryEscape(u.Query().Get("state"))
	}
	for name, tc := range map[string]struct{ query, want string }{
		"expired state":   {"state=bad&code=dev@example.com", "/login?error=sign_in_expired"},
		"cancelled":       {"state=" + login("/") + "&error=access_denied", "/login?error=sign_in_cancelled"},
		"exchange failed": {"state=" + login("/") + "&code=broken", "/login?error=sign_in_failed"},
		"not allowed":     {"state=" + login("/") + "&code=eve@evil.com", "/login?error=email_not_allowed"},
		"offsite return":  {"state=" + login("https://evil.com/x") + "&code=dev@example.com", "/"},
		"protocol return": {"state=" + login("//evil.com") + "&code=dev@example.com", "/"},
		"absolute return": {"state=" + login(s.srv.URL+"/settings") + "&code=dev@example.com", s.srv.URL + "/settings"},
	} {
		t.Run(name, func(t *testing.T) {
			res := s.do(b, "GET", "/api/v1/auth/google/callback?"+tc.query, nil)
			assert.Equal(t, http.StatusFound, res.status)
			assert.Equal(t, tc.want, res.header.Get("Location"))
		})
	}
}

func TestDatabasesAPI(t *testing.T) {
	s := newStack(t)
	c := s.rootSession()
	other := s.googleSession("bob@example.com")

	res := s.do(c, "GET", "/api/v1/drivers", nil)
	assert.Contains(t, res.body, `"id":"mysql"`)

	res = s.do(c, "POST", "/api/v1/databases/test", map[string]any{"database": map[string]string{"driver": "postgres", "dsn": testutil.GoodDSN}})
	assert.JSONEq(t, `{"ok":true,"serverVersion":"PostgreSQL 18.0"}`, res.body)
	res = s.do(c, "POST", "/api/v1/databases/test", map[string]any{"database": map[string]string{"driver": "postgres", "dsn": "postgres://nope"}})
	assert.Equal(t, http.StatusBadRequest, res.status)
	assert.Equal(t, float64(400), res.json(t)["error"].(map[string]any)["code"])

	for _, path := range []string{"/api/v1/databases", "/api/v1/databases/test"} {
		assert.Equal(t, http.StatusBadRequest, s.do(c, "POST", path, "{", "Content-Type", "application/json").status, path)
	}

	id := s.createDB(c)
	db := "/api/v1/databases/" + id
	res = s.do(c, "GET", "/api/v1/databases", nil)
	assert.Contains(t, res.body, `"dsnMasked":"postgres://app:****@db.internal:5432/shop"`)
	assert.NotContains(t, res.body, "secret")
	assert.Equal(t, http.StatusOK, s.do(c, "GET", db, nil).status)
	assert.Equal(t, http.StatusNotFound, s.do(other, "GET", db, nil).status)
	assert.Equal(t, http.StatusNotFound, s.do(c, "GET", "/api/v1/databases/missing", nil).status)

	res = s.do(c, "PUT", db, map[string]any{"database": map[string]string{"name": "renamed"}})
	assert.Equal(t, "renamed", res.json(t)["database"].(map[string]any)["name"])
	assert.Equal(t, http.StatusBadRequest, s.do(c, "PUT", db, "{", "Content-Type", "application/json").status)
	assert.Equal(t, http.StatusNotFound, s.do(other, "PUT", db, map[string]any{"database": map[string]string{"name": "x"}}).status)

	res = s.do(c, "PUT", db+"/pii", map[string]any{"piiColumns": []map[string]string{{"table": "users", "column": "email"}}})
	assert.JSONEq(t, `{"piiColumns":[{"schema":"public","table":"users","column":"email"}]}`, res.body)
	assert.Equal(t, http.StatusBadRequest, s.do(c, "PUT", db+"/pii", "{", "Content-Type", "application/json").status)
	assert.Equal(t, http.StatusNotFound, s.do(other, "PUT", db+"/pii", map[string]any{"piiColumns": []any{}}).status)
	assert.Contains(t, s.do(c, "GET", db+"/pii", nil).body, `"column":"email"`)
	assert.Equal(t, http.StatusNotFound, s.do(other, "GET", db+"/pii", nil).status)
	res = s.do(c, "PUT", db+"/pii/agent", map[string]bool{"agentMarksPii": true})
	assert.Equal(t, true, res.json(t)["database"].(map[string]any)["agentMarksPii"])
	assert.Equal(t, http.StatusBadRequest, s.do(c, "PUT", db+"/pii/agent", "{", "Content-Type", "application/json").status)
	assert.Equal(t, http.StatusNotFound, s.do(other, "PUT", db+"/pii/agent", map[string]bool{"agentMarksPii": false}).status)
	assert.Contains(t, s.do(c, "GET", db+"/schema", nil).body, `"name":"email","dataType":"text","pii":true`)
	assert.Equal(t, http.StatusNotFound, s.do(other, "GET", db+"/schema", nil).status)

	res = s.do(c, "POST", db+"/query", map[string]string{"sql": "select email from users"})
	assert.Equal(t, http.StatusUnprocessableEntity, res.status)
	assert.Contains(t, res.body, "users.email")
	res = s.do(c, "POST", db+"/query", map[string]string{"sql": "select id from users"})
	assert.Equal(t, http.StatusOK, res.status)
	assert.Equal(t, float64(3), res.json(t)["rowCount"])
	assert.Equal(t, http.StatusBadRequest, s.do(c, "POST", db+"/query", "{", "Content-Type", "application/json").status)

	res = s.do(c, "GET", db+"/queries?limit=10", nil)
	assert.Len(t, res.json(t)["queries"], 2)
	assert.Equal(t, http.StatusNotFound, s.do(other, "GET", db+"/queries", nil).status)

	assert.Equal(t, http.StatusNotFound, s.do(other, "DELETE", db, nil).status)
	assert.Equal(t, http.StatusNoContent, s.do(c, "DELETE", db, nil).status)
	assert.Equal(t, http.StatusNotFound, s.do(c, "DELETE", db, nil).status)
}

func TestInternalErrorsHideDetails(t *testing.T) {
	s := newStack(t)
	c := s.rootSession()
	db := "/api/v1/databases/" + s.createDB(c)
	// A schema failure is the database's message and safe to show.
	s.driver.SchemaErr = errors.New("permission denied for schema billing")
	res := s.do(c, "GET", db+"/schema", nil)
	assert.Equal(t, http.StatusBadRequest, res.status)
	assert.Contains(t, res.body, "permission denied")
}

func TestTmphostSettings(t *testing.T) {
	s := newStack(t)
	c := s.rootSession()

	res := s.do(c, "GET", "/api/v1/settings/tmphost", nil)
	assert.Equal(t, false, res.json(t)["connected"])

	res = s.do(c, "GET", "/api/v1/settings/tmphost/connect", nil)
	require.Equal(t, http.StatusFound, res.status)
	u, _ := url.Parse(res.header.Get("Location"))
	assert.Equal(t, s.tmphost.URL+"/oauth2/authorize", u.Scheme+"://"+u.Host+u.Path)
	assert.NotEmpty(t, u.Query().Get("state"))

	res = s.do(c, "GET", "/api/v1/settings/tmphost/callback?state=wrong&code=good-code", nil)
	assert.Contains(t, res.header.Get("Location"), "&tmphost=error")

	// The flow cookie was spent by the failed attempt; start over.
	res = s.do(c, "GET", "/api/v1/settings/tmphost/connect", nil)
	u, _ = url.Parse(res.header.Get("Location"))
	state := u.Query().Get("state")
	res = s.do(c, "GET", "/api/v1/settings/tmphost/callback?state="+url.QueryEscape(state)+"&code=good-code", nil)
	assert.Equal(t, "/settings?tmphost=connected", res.header.Get("Location"))

	res = s.do(c, "GET", "/api/v1/settings/tmphost", nil)
	assert.Equal(t, "dev@tmphost.test", res.json(t)["account"])
	assert.Equal(t, http.StatusNoContent, s.do(c, "DELETE", "/api/v1/settings/tmphost", nil).status)

	// A tmphost that cannot be reached on the first connect.
	s = newStack(t)
	c = s.rootSession()
	s.tmphost.Close()
	res = s.do(c, "GET", "/api/v1/settings/tmphost/connect", nil)
	assert.Equal(t, "/settings?message=tmphost+could+not+be+reached&tmphost=error", res.header.Get("Location"))
}

func pkce() (verifier, challenge string) {
	verifier = "verifier-verifier-verifier-verifier-verifier-0001"
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestMCPOAuthFlow(t *testing.T) {
	s := newStack(t)
	c := s.rootSession()
	id := s.createDB(c)
	mcpURL := "/mcp/" + id
	api := s.browser()
	const redirect = "https://client.example/callback"

	res := s.do(api, "GET", "/.well-known/oauth-protected-resource"+mcpURL, nil)
	assert.Equal(t, []any{s.srv.URL + mcpURL}, res.json(t)["authorization_servers"])
	res = s.do(api, "GET", mcpURL+"/.well-known/oauth-authorization-server", nil)
	assert.Equal(t, s.srv.URL+mcpURL+"/oauth2/token", res.json(t)["token_endpoint"])
	assert.Equal(t, "*", res.header.Get("Access-Control-Allow-Origin"))
	assert.Empty(t, res.header.Get("X-Frame-Options"))
	assert.Equal(t, http.StatusNoContent, s.do(api, "OPTIONS", mcpURL+"/oauth2/token", nil).status)
	for _, p := range []string{"/.well-known/oauth-authorization-server/mcp/missing", "/mcp/missing/.well-known/oauth-protected-resource"} {
		assert.Equal(t, http.StatusNotFound, s.do(api, "GET", p, nil).status, p)
	}

	// Unauthenticated MCP calls are told where the metadata is.
	res = s.do(api, "POST", mcpURL, "{}")
	assert.Equal(t, http.StatusUnauthorized, res.status)
	assert.Contains(t, res.header.Get("WWW-Authenticate"), `resource_metadata="`+s.srv.URL+"/.well-known/oauth-protected-resource"+mcpURL+`"`)
	assert.Equal(t, http.StatusUnauthorized, s.do(api, "POST", mcpURL, "{}", "Authorization", "Bearer junk").status)
	assert.Equal(t, http.StatusNotFound, s.do(api, "POST", "/mcp/missing", "{}").status)

	// Registration.
	assert.Equal(t, http.StatusMethodNotAllowed, s.do(api, "GET", mcpURL+"/oauth2/register", nil).status)
	assert.Equal(t, http.StatusNotFound, s.do(api, "POST", "/mcp/missing/oauth2/register", "{}").status)
	for _, bad := range []string{`nope`, `{"redirect_uris":"x"}`, `{"redirect_uris":[1]}`, `{"redirect_uris":["https://u:p@client.example/cb"]}`, `{"client_name":5}`} {
		assert.Equal(t, http.StatusBadRequest, s.do(api, "POST", mcpURL+"/oauth2/register", bad).status, bad)
	}
	res = s.do(api, "POST", mcpURL+"/oauth2/register", map[string]any{"client_name": "Claude", "redirect_uris": []string{redirect}})
	require.Equal(t, http.StatusCreated, res.status, res.body)
	clientID := res.json(t)["client_id"].(string)

	verifier, challenge := pkce()
	authorize := func(extra url.Values) string {
		q := url.Values{"client_id": {clientID}, "redirect_uri": {redirect}, "response_type": {"code"},
			"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {"xyz"}}
		for k, v := range extra {
			q[k] = v
		}
		return mcpURL + "/oauth2/authorize?" + q.Encode()
	}

	assert.Equal(t, http.StatusNotFound, s.do(c, "GET", "/mcp/missing/oauth2/authorize", nil).status)
	assert.Equal(t, http.StatusBadRequest, s.do(c, "GET", authorize(url.Values{"client_id": {"junk"}}), nil).status)
	assert.Equal(t, http.StatusBadRequest, s.do(c, "GET", authorize(url.Values{"redirect_uri": {"https://other.example/cb"}}), nil).status)
	res = s.do(c, "GET", authorize(url.Values{"response_type": {"token"}}), nil)
	assert.Contains(t, res.header.Get("Location"), "error=unsupported_response_type")
	res = s.do(c, "GET", authorize(url.Values{"code_challenge_method": {"plain"}}), nil)
	assert.Contains(t, res.header.Get("Location"), "error=invalid_request")

	// Signed out: to the login page and back.
	res = s.do(api, "GET", authorize(nil), nil)
	assert.Equal(t, http.StatusFound, res.status)
	assert.True(t, strings.HasPrefix(res.header.Get("Location"), s.srv.URL+"/login?redirect_url="))
	// Someone else's database.
	assert.Equal(t, http.StatusForbidden, s.do(s.googleSession("bob@example.com"), "GET", authorize(nil), nil).status)

	// The owner consents.
	res = s.do(c, "GET", authorize(nil), nil)
	require.Equal(t, http.StatusOK, res.status)
	assert.Contains(t, res.body, "Claude")
	assert.Contains(t, res.body, "shop")
	consent := regexp.MustCompile(`name="consent" value="([^"]+)"`).FindStringSubmatch(res.body)
	require.Len(t, consent, 2)
	res = s.do(c, "POST", authorize(nil), url.Values{"consent": {consent[1]}, "decision": {"allow"}})
	require.Equal(t, http.StatusFound, res.status, res.body)
	back, _ := url.Parse(res.header.Get("Location"))
	assert.Equal(t, "xyz", back.Query().Get("state"))
	code := back.Query().Get("code")
	require.NotEmpty(t, code)

	// Token endpoint.
	token := func(form url.Values) response {
		return s.do(api, "POST", mcpURL+"/oauth2/token", form)
	}
	assert.Equal(t, http.StatusMethodNotAllowed, s.do(api, "GET", mcpURL+"/oauth2/token", nil).status)
	assert.Equal(t, http.StatusBadRequest, s.do(api, "POST", "/mcp/missing/oauth2/token", url.Values{}).status)
	assert.Equal(t, "unsupported_grant_type", token(url.Values{"grant_type": {"password"}}).json(t)["error"])
	assert.Equal(t, http.StatusBadRequest, s.do(api, "POST", mcpURL+"/oauth2/token", "a=%zz", "Content-Type", "application/x-www-form-urlencoded").status)
	codeForm := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "redirect_uri": {redirect}, "code_verifier": {"wrong"}}
	assert.Equal(t, "invalid_grant", token(codeForm).json(t)["error"], "a wrong PKCE verifier is refused")
	codeForm.Set("code_verifier", verifier)
	res = token(codeForm)
	require.Equal(t, http.StatusOK, res.status, res.body)
	tokens := res.json(t)
	access := tokens["access_token"].(string)

	res = token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens["refresh_token"].(string)}})
	require.Equal(t, http.StatusOK, res.status, res.body)

	// The MCP server itself.
	initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	res = s.do(api, "POST", mcpURL, initBody, "Authorization", "Bearer "+access, "Content-Type", "application/json", "Accept", "application/json, text/event-stream")
	require.Equal(t, http.StatusOK, res.status, res.body)
	assert.Contains(t, res.body, "SQL MCP: shop")
	assert.Contains(t, res.body, "The engine is PostgreSQL", "the instructions name the database's engine")
	call := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"sql","arguments":{"action":"query","payload":{"sql":"select id from users"}}}}`
	res = s.do(api, "POST", mcpURL, call, "Authorization", "Bearer "+access, "Content-Type", "application/json", "Accept", "application/json, text/event-stream", "Mcp-Protocol-Version", "2025-06-18")
	require.Equal(t, http.StatusOK, res.status, res.body)
	assert.Contains(t, res.body, `rowCount: 3`)

	// markPii works once the owner turns it on, and only adds.
	mark := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"sql","arguments":{"action":"markPii","payload":{"table":"orders","columns":["total"]}}}}`
	mcpHeaders := []string{"Authorization", "Bearer " + access, "Content-Type", "application/json", "Accept", "application/json, text/event-stream", "Mcp-Protocol-Version", "2025-06-18"}
	res = s.do(api, "POST", mcpURL, mark, mcpHeaders...)
	assert.Contains(t, res.body, "the owner has not let agents mark PII columns")
	require.Equal(t, http.StatusOK, s.do(c, "PUT", "/api/v1/databases/"+id+"/pii/agent", map[string]bool{"agentMarksPii": true}).status)
	res = s.do(api, "POST", mcpURL, initBody, mcpHeaders...)
	assert.Contains(t, res.body, "mark it with markPii")
	res = s.do(api, "POST", mcpURL, mark, mcpHeaders...)
	assert.Contains(t, res.body, `columns[1]{name,pii}:\n  total,marked`)
	assert.Contains(t, s.do(c, "GET", "/api/v1/databases/"+id+"/pii", nil).body, `"column":"total"`)

	// Deleting the database revokes every grant.
	require.Equal(t, http.StatusNoContent, s.do(c, "DELETE", "/api/v1/databases/"+id, nil).status)
	assert.Equal(t, http.StatusNotFound, s.do(api, "POST", mcpURL, initBody, "Authorization", "Bearer "+access).status)
	assert.Equal(t, http.StatusBadRequest, token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens["refresh_token"].(string)}}).status)
}

func TestCLILogin(t *testing.T) {
	s := newStack(t)
	c := s.rootSession()
	shop := s.createDB(c)
	res := s.do(c, "POST", "/api/v1/databases", map[string]any{"database": map[string]string{"name": "crm", "driver": "postgres", "dsn": testutil.GoodDSN}})
	require.Equal(t, http.StatusCreated, res.status, res.body)
	crm := res.json(t)["database"].(map[string]any)["id"].(string)
	api := s.browser()
	const redirect = "http://127.0.0.1:41234/callback"

	verifier, challenge := pkce()
	authorize := func(extra url.Values) string {
		q := url.Values{"client_id": {auth.CLIClientID}, "redirect_uri": {redirect}, "response_type": {"code"},
			"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {"st"}}
		for k, v := range extra {
			q[k] = v
		}
		return "/cli/oauth2/authorize?" + q.Encode()
	}
	assert.Equal(t, http.StatusBadRequest, s.do(c, "GET", authorize(url.Values{"client_id": {"other"}}), nil).status)
	for _, bad := range []string{"https://evil.example/callback", "http://127.0.0.1:41234/other"} {
		assert.Equal(t, http.StatusBadRequest, s.do(c, "GET", authorize(url.Values{"redirect_uri": {bad}}), nil).status, bad)
	}
	res = s.do(c, "GET", authorize(url.Values{"response_type": {"token"}}), nil)
	assert.Contains(t, res.header.Get("Location"), "error=unsupported_response_type")
	res = s.do(c, "GET", authorize(url.Values{"code_challenge": {""}}), nil)
	assert.Contains(t, res.header.Get("Location"), "error=invalid_request")
	res = s.do(api, "GET", authorize(nil), nil)
	assert.True(t, strings.HasPrefix(res.header.Get("Location"), s.srv.URL+"/login?redirect_url="), "signed out: to the login page")

	// The person picks shop and not crm.
	res = s.do(c, "GET", authorize(nil), nil)
	require.Equal(t, http.StatusOK, res.status, res.body)
	assert.Contains(t, res.body, `value="`+shop+`"`)
	assert.Contains(t, res.body, `value="`+crm+`"`)
	consent := regexp.MustCompile(`name="consent" value="([^"]+)"`).FindStringSubmatch(res.body)
	require.Len(t, consent, 2)
	res = s.do(c, "POST", authorize(nil), url.Values{"consent": {consent[1]}, "decision": {"allow"}, "db": {shop}})
	require.Equal(t, http.StatusFound, res.status, res.body)
	back, _ := url.Parse(res.header.Get("Location"))
	assert.Equal(t, "127.0.0.1:41234", back.Host)
	code := back.Query().Get("code")
	require.NotEmpty(t, code)

	token := func(form url.Values) response { return s.do(api, "POST", "/cli/oauth2/token", form) }
	assert.Equal(t, "unsupported_grant_type", token(url.Values{"grant_type": {"password"}}).json(t)["error"])
	assert.Equal(t, http.StatusBadRequest, s.do(api, "POST", "/cli/oauth2/token", "a=%zz", "Content-Type", "application/x-www-form-urlencoded").status)
	codeForm := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {auth.CLIClientID}, "redirect_uri": {redirect}, "code_verifier": {"wrong"}}
	assert.Equal(t, "invalid_grant", token(codeForm).json(t)["error"])
	codeForm.Set("code_verifier", verifier)
	res = token(codeForm)
	require.Equal(t, http.StatusOK, res.status, res.body)
	tokens := res.json(t)
	assert.Equal(t, []any{shop}, tokens["databases"])
	bearer := []string{"Authorization", "Bearer " + tokens["access_token"].(string)}

	// The CLI sees shop alone, as if crm were someone else's.
	assert.Contains(t, s.do(api, "GET", "/api/v1/auth/user", nil, bearer...).body, "root@sqlmcp.local")
	list := s.do(api, "GET", "/api/v1/databases", nil, bearer...).json(t)["databases"].([]any)
	require.Len(t, list, 1)
	assert.Equal(t, shop, list[0].(map[string]any)["id"])
	assert.Equal(t, http.StatusOK, s.do(api, "GET", "/api/v1/databases/"+shop+"/schema", nil, bearer...).status)
	for _, p := range []string{"/api/v1/databases/" + crm, "/api/v1/databases/" + crm + "/schema", "/api/v1/databases/" + crm + "/pii"} {
		assert.Equal(t, http.StatusNotFound, s.do(api, "GET", p, nil, bearer...).status, p)
	}

	// It adds PII columns and never removes one, nor changes a database.
	res = s.do(api, "POST", "/api/v1/databases/"+shop+"/pii", map[string]any{"piiColumns": []map[string]string{{"table": "users", "column": "email"}}}, bearer...)
	require.Equal(t, http.StatusOK, res.status, res.body)
	assert.Contains(t, res.body, `"column":"email"`)
	assert.Equal(t, http.StatusBadRequest, s.do(api, "POST", "/api/v1/databases/"+shop+"/pii", "nope", bearer...).status)
	assert.Equal(t, http.StatusBadRequest, s.do(api, "POST", "/api/v1/databases/"+shop+"/pii", map[string]any{"piiColumns": []map[string]string{{"table": "users"}}}, bearer...).status)
	assert.Equal(t, http.StatusNotFound, s.do(api, "POST", "/api/v1/databases/"+shop+"x/pii", map[string]any{"piiColumns": []any{}}, bearer...).status)
	for _, r := range [][2]string{
		{"PUT", "/api/v1/databases/" + shop + "/pii"}, {"PUT", "/api/v1/databases/" + shop + "/pii/agent"},
		{"PUT", "/api/v1/databases/" + shop}, {"DELETE", "/api/v1/databases/" + shop}, {"POST", "/api/v1/databases"},
		{"GET", "/api/v1/settings/tmphost"}, {"POST", "/api/v1/auth/logout"},
	} {
		assert.Equal(t, http.StatusForbidden, s.do(api, r[0], r[1], map[string]any{"piiColumns": []any{}}, bearer...).status, r)
	}
	assert.Contains(t, s.do(c, "GET", "/api/v1/databases/"+shop+"/pii", nil).body, `"column":"email"`)

	// A query comes back whole, past the inline limit, and is never stored
	// on tmphost; the log says it came from the CLI.
	s.driver.Rows = 8
	res = s.do(api, "POST", "/api/v1/databases/"+shop+"/query", map[string]string{"sql": "select id from users"}, bearer...)
	require.Equal(t, http.StatusOK, res.status, res.body)
	assert.EqualValues(t, 8, res.json(t)["rowCount"])
	assert.Len(t, res.json(t)["rows"], 8)
	assert.Zero(t, s.tmphost.Uploads.Load())
	res = s.do(api, "POST", "/api/v1/databases/"+shop+"/query", map[string]string{"sql": "select email from users"}, bearer...)
	assert.Equal(t, http.StatusUnprocessableEntity, res.status, "the PII guard holds")
	assert.Contains(t, s.do(api, "GET", "/api/v1/databases/"+shop+"/queries", nil, bearer...).body, `"source":"cli"`)

	// Other tokens are not the CLI's, and the CLI's is not a session.
	assert.Equal(t, http.StatusUnauthorized, s.do(api, "GET", "/api/v1/databases", nil, "Authorization", "Bearer "+tokens["refresh_token"].(string)).status)
	assert.Equal(t, http.StatusUnauthorized, s.do(api, "GET", "/api/v1/databases", nil, "Authorization", "Bearer junk").status)

	// Refreshing keeps the grant; an account that is gone cannot refresh.
	res = token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens["refresh_token"].(string)}})
	require.Equal(t, http.StatusOK, res.status, res.body)
	assert.Equal(t, []any{shop}, res.json(t)["databases"])
	signer, err := auth.NewTokenService(testutil.Secret)
	require.NoError(t, err)
	_, ghost, err := signer.CreateCLITokens(auth.Grant{UserID: "AAAAAAAAAAB", Databases: []string{shop}})
	require.NoError(t, err)
	assert.Equal(t, "invalid_grant", token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {ghost}}).json(t)["error"])
}

func TestCLIConsentNeedsDatabases(t *testing.T) {
	s := newStack(t)
	c := s.rootSession()
	_, challenge := pkce()
	q := url.Values{"client_id": {auth.CLIClientID}, "redirect_uri": {"http://localhost:5000/callback"}, "response_type": {"code"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	res := s.do(c, "GET", "/cli/oauth2/authorize?"+q.Encode(), nil)
	assert.Contains(t, res.body, "You have no databases yet")

	db, err := gorm.Open(sqlite.Open(s.dbPath), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("DROP TABLE databases").Error)
	res = s.do(c, "GET", "/cli/oauth2/authorize?"+q.Encode(), nil)
	assert.Equal(t, http.StatusInternalServerError, res.status)
	assert.NotContains(t, res.body, "no such table")
}

func TestSPA(t *testing.T) {
	s := newStack(t)
	b := s.browser()

	res := s.do(b, "GET", "/databases/x", nil)
	assert.Equal(t, http.StatusNotFound, res.status, "no build yet")
	assert.Contains(t, res.body, "make build")

	require.NoError(t, os.MkdirAll(filepath.Join(s.public, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(s.public, "index.html"),
		[]byte(`<html><head><link href="favicon.svg"><script src="./assets/app.js"></script><link href="./assets/app.css"></head></html>`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(s.public, "assets", "app.js"), []byte("1"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(s.public, "robots.txt"), []byte("ok"), 0o644))

	for _, p := range []string{"/", "/index.html", "/databases/x"} {
		res = s.do(b, "GET", p, nil)
		assert.Equal(t, http.StatusOK, res.status, p)
		assert.Equal(t, `<html><head><script>window.__SQLMCP_BASE_PATH__="";</script><link href="/favicon.svg"><script src="/assets/app.js"></script><link href="/assets/app.css"></head></html>`, res.body, p)
		assert.Equal(t, "DENY", res.header.Get("X-Frame-Options"))
		assert.Equal(t, "nosniff", res.header.Get("X-Content-Type-Options"))
	}
	res = s.do(b, "GET", "/assets/app.js", nil)
	assert.Equal(t, "1", res.body)
	assert.Contains(t, res.header.Get("Cache-Control"), "immutable")
	assert.Equal(t, "ok", s.do(b, "GET", "/robots.txt", nil).body)
	assert.Equal(t, http.StatusNotFound, s.do(b, "GET", "/assets/old.js", nil).status)
	assert.Equal(t, http.StatusNotFound, s.do(b, "GET", "/missing.png", nil).status)
	assert.Equal(t, http.StatusMethodNotAllowed, s.do(b, "POST", "/", "x").status)

	require.NoError(t, os.Chmod(filepath.Join(s.public, "index.html"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(s.public, "index.html"), 0o644) })
	if _, err := os.ReadFile(filepath.Join(s.public, "index.html")); err != nil {
		assert.Equal(t, http.StatusInternalServerError, s.do(b, "GET", "/", nil).status)
	}

	assert.JSONEq(t, `{"status":"ok"}`, s.do(b, "GET", "/healthz", nil).body)
}

func TestNewRefusesBadConfig(t *testing.T) {
	good := config.Config{
		JWTSecret: testutil.Secret, EncryptionKey: testutil.Key, AppDBDriver: "sqlite",
		AppDBDSN: filepath.Join(t.TempDir(), "a.db"), QueryMaxRows: 1, QueryInlineRows: 1,
		BaseURL: "http://x.test", GoogleClientID: "id",
	}
	_, err := app.New(good, app.Deps{})
	require.NoError(t, err, "defaults fill every dependency")

	for name, mutate := range map[string]func(*config.Config){
		"short key":   func(c *config.Config) { c.EncryptionKey = []byte("short") },
		"bad pattern": func(c *config.Config) { c.AllowedEmails = []string{"re:("} },
		"bad db": func(c *config.Config) {
			c.AppDBDriver = "postgres"
			c.AppDBDSN = "host=127.0.0.1 port=1 connect_timeout=1"
		},
		"unwritable": func(c *config.Config) { c.AppDBDSN = "/proc/nope/a.db" },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := good
			cfg.AppDBDSN = filepath.Join(t.TempDir(), "a.db")
			mutate(&cfg)
			_, err := app.New(cfg, app.Deps{})
			assert.Error(t, err)
		})
	}
}

// A validly signed session for nobody, or with a bad subject, is no session.
func TestSessionForUnknownUser(t *testing.T) {
	s := newStack(t)
	tokens, err := auth.NewTokenService(testutil.Secret)
	require.NoError(t, err)
	ghost := crud.FormatID(424242 << 22)
	for _, subject := range []string{ghost, "bad"} {
		access, err := tokens.CreateAccessToken(subject, "ghost@example.com", "", "")
		require.NoError(t, err)
		res := s.do(s.browser(), "GET", "/api/v1/auth/user", nil, "Cookie", "at="+access)
		assert.Equal(t, http.StatusUnauthorized, res.status, subject)
	}
	refresh, err := tokens.CreateRefreshToken(ghost)
	require.NoError(t, err)
	res := s.do(s.browser(), "POST", "/api/v1/auth/refresh", nil, "Cookie", "rt="+refresh)
	assert.Equal(t, http.StatusUnauthorized, res.status)
	assert.Contains(t, strings.Join(res.header.Values("Set-Cookie"), ";"), "at=;")
}

func TestAdmin(t *testing.T) {
	s := newStack(t)
	anon := s.browser()

	assert.JSONEq(t, `{"enabled":true}`, s.do(anon, "GET", "/admin/api/v1/config", nil).body)
	assert.Equal(t, http.StatusUnauthorized, s.do(anon, "GET", "/admin/api/v1/settings", nil).status)
	assert.Equal(t, http.StatusUnauthorized, s.do(anon, "POST", "/admin/api/v1/login", map[string]string{"rootToken": "wrong"}).status)
	assert.Equal(t, http.StatusBadRequest, s.do(anon, "POST", "/admin/api/v1/login", "{", "Content-Type", "application/json").status)

	// A person's session is not an admin session, and an admin token is
	// nobody's session.
	user := s.rootSession()
	assert.Equal(t, http.StatusUnauthorized, s.do(user, "GET", "/admin/api/v1/settings", nil).status)

	admin := s.browser()
	res := s.do(admin, "POST", "/admin/api/v1/login", map[string]string{"rootToken": rootToken})
	require.Equal(t, http.StatusOK, res.status)
	token := res.json(t)["token"].(string)
	cookie := strings.Join(res.header.Values("Set-Cookie"), ";")
	assert.Contains(t, cookie, "admin_at=")
	assert.Contains(t, cookie, "path=/admin;")
	assert.Contains(t, cookie, "HttpOnly")
	assert.Equal(t, http.StatusUnauthorized, s.do(anon, "GET", "/api/v1/auth/user", nil, "Cookie", "at="+token).status)

	res = s.do(admin, "GET", "/admin/api/v1/session", nil)
	assert.Equal(t, http.StatusOK, res.status)
	res = s.do(anon, "GET", "/admin/api/v1/settings", nil, "Authorization", "Bearer "+token)
	require.Equal(t, http.StatusOK, res.status)
	got := res.json(t)
	assert.Equal(t, "env", got["source"])
	assert.Equal(t, []any{"*@example.com", "root@sqlmcp.local"}, got["allowedEmails"])
	assert.Equal(t, s.srv.URL, got["server"].(map[string]any)["baseUrl"])

	res = s.do(admin, "POST", "/admin/api/v1/settings/check-email", map[string]any{"email": "dev@agentrq.com", "allowedEmails": []string{"*@agentrq.com"}})
	assert.JSONEq(t, `{"allowed":true,"matchedEntry":"*@agentrq.com"}`, res.body)
	assert.Equal(t, http.StatusBadRequest, s.do(admin, "POST", "/admin/api/v1/settings/check-email", map[string]any{"email": "a", "allowedEmails": []string{"re:("}}).status)
	assert.Equal(t, http.StatusBadRequest, s.do(admin, "POST", "/admin/api/v1/settings/check-email", "{", "Content-Type", "application/json").status)

	res = s.do(admin, "PUT", "/admin/api/v1/settings", map[string]any{"allowedEmails": []string{}, "queryMaxRows": 10, "queryInlineRows": 2, "queryTimeoutSeconds": 5})
	assert.Equal(t, http.StatusBadRequest, res.status)
	assert.Contains(t, res.body, "at least one entry")
	assert.Equal(t, http.StatusBadRequest, s.do(admin, "PUT", "/admin/api/v1/settings", "{", "Content-Type", "application/json").status)

	res = s.do(admin, "PUT", "/admin/api/v1/settings", map[string]any{"allowedEmails": []string{"*@agentrq.com"}, "queryMaxRows": 10, "queryInlineRows": 2, "queryTimeoutSeconds": 5})
	require.Equal(t, http.StatusOK, res.status, res.body)
	got = res.json(t)
	assert.Equal(t, "saved", got["source"])
	assert.Equal(t, float64(5), got["queryTimeoutSeconds"])
	assert.NotEmpty(t, got["updatedAt"])

	// Applied at once: example.com sessions end at their next refresh.
	assert.Equal(t, http.StatusForbidden, s.do(s.browser(), "POST", "/api/v1/auth/root/login", map[string]string{"rootToken": rootToken}).status, "root@sqlmcp.local is no longer allowed")
	assert.Equal(t, http.StatusUnauthorized, s.do(user, "POST", "/api/v1/auth/refresh", nil).status)

	res = s.do(admin, "DELETE", "/admin/api/v1/settings", nil)
	assert.Equal(t, "env", res.json(t)["source"])
	assert.Equal(t, http.StatusOK, s.do(s.browser(), "POST", "/api/v1/auth/root/login", map[string]string{"rootToken": rootToken}).status)

	assert.Equal(t, http.StatusNoContent, s.do(admin, "POST", "/admin/api/v1/logout", nil).status)
	assert.Equal(t, http.StatusUnauthorized, s.do(admin, "GET", "/admin/api/v1/settings", nil).status)

	// The admin pages are the web app's.
	require.NoError(t, os.WriteFile(filepath.Join(s.public, "index.html"), []byte(`<html><head></head></html>`), 0o644))
	assert.Equal(t, http.StatusOK, s.do(anon, "GET", "/admin/login", nil).status)
}

func TestAdminDisabledWithoutRootToken(t *testing.T) {
	cfg := config.Config{
		JWTSecret: testutil.Secret, EncryptionKey: testutil.Key, AppDBDriver: "sqlite",
		AppDBDSN: filepath.Join(t.TempDir(), "a.db"), QueryMaxRows: 1, QueryInlineRows: 1,
		BaseURL: "http://x.test", AllowedEmails: []string{"*@x.test"},
	}
	h, err := app.New(cfg, app.Deps{})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/admin/api/v1/config", nil))
	assert.JSONEq(t, `{"enabled":false}`, w.Body.String())
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/admin/api/v1/login", strings.NewReader(`{"rootToken":""}`)))
	assert.Equal(t, http.StatusForbidden, w.Code)

	tokens, _ := auth.NewTokenService(testutil.Secret)
	token, _, _ := tokens.CreateAdminToken()
	r := httptest.NewRequest("GET", "/admin/api/v1/settings", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code, "no admin token is accepted while the setup is disabled")
}

func TestNewRefusesAnUnreadableSavedConfiguration(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "a.db")
	cfg := config.Config{
		JWTSecret: testutil.Secret, EncryptionKey: testutil.Key, AppDBDriver: "sqlite",
		AppDBDSN: dsn, QueryMaxRows: 1, QueryInlineRows: 1, BaseURL: "http://x.test",
	}
	repo, err := repository.Open("sqlite", dsn, func() int64 { return 1 << 22 })
	require.NoError(t, err)
	require.NoError(t, repo.SetSetting(context.Background(), "configuration.json", "garbage"))
	_, err = app.New(cfg, app.Deps{})
	assert.ErrorContains(t, err, "ENCRYPTION_KEY")
}

func TestQueryStream(t *testing.T) {
	s := newStack(t)
	c := s.rootSession()
	db := "/api/v1/databases/" + s.createDB(c)
	assert.Equal(t, http.StatusNotFound, s.do(s.googleSession("bob@example.com"), "GET", db+"/queries/stream", nil).status)
	assert.Equal(t, http.StatusUnauthorized, s.do(s.browser(), "GET", db+"/queries/stream", nil).status)

	res, err := c.Get(s.srv.URL + db + "/queries/stream")
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "text/event-stream", res.Header.Get("Content-Type"))
	r := bufio.NewReader(res.Body)
	line, err := r.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, ": connected\n", line, "the stream is flushed, not buffered by Fiber")

	s.do(c, "POST", db+"/query", map[string]string{"sql": "select id from users"})
	var event strings.Builder
	for !strings.Contains(event.String(), "data: ") {
		line, err = r.ReadString('\n')
		require.NoError(t, err)
		event.WriteString(line)
	}
	assert.Contains(t, event.String(), "event: query\n")
	assert.Contains(t, event.String(), `"sql":"select id from users"`)

	assert.Equal(t, http.StatusNoContent, s.do(c, "DELETE", db, nil).status)
	_, err = io.ReadAll(r)
	assert.NoError(t, err, "deleting the database ends the stream")
}
