package oauthconsent

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentrq/sql-mcp/backend/internal/service/auth"
)

func testTokens(t *testing.T) *auth.TokenService {
	t.Helper()
	s, err := auth.NewTokenService("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	return s
}

func testRequest() Request {
	return Request{
		UserID:       "u1",
		Email:        "ann@x.com",
		ClientName:   "Claude <b>Code</b>",
		Registered:   true,
		DatabaseName: "shop",
		Consent: auth.Consent{
			DatabaseID:    "db1",
			ClientID:      "c1",
			RedirectURI:   "http://localhost:3118/callback",
			State:         "st",
			CodeChallenge: auth.S256("verifier"),
		},
		RegisteredRedirectURIs: []string{"http://localhost/callback"},
		BaseURL:                "https://sql.example.com",
	}
}

// failingTokens fails whichever call the test names.
type failingTokens struct {
	Tokens
	failConsent, failCode bool
}

func (f failingTokens) CreateConsentToken(userID string, c auth.Consent) (string, error) {
	if f.failConsent {
		return "", errors.New("boom")
	}
	return f.Tokens.CreateConsentToken(userID, c)
}

func (f failingTokens) CreateCode(userID string, c auth.Consent, databases []string) (string, error) {
	if f.failCode {
		return "", errors.New("boom")
	}
	return f.Tokens.CreateCode(userID, c, databases)
}

func serve(tokens Tokens, req Request, method string, form url.Values) *httptest.ResponseRecorder {
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, "/oauth2/authorize", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, "/oauth2/authorize", nil)
	}
	w := httptest.NewRecorder()
	Serve(w, r, tokens, req)
	return w
}

func consentToken(t *testing.T, tokens Tokens, req Request) string {
	t.Helper()
	token, err := tokens.CreateConsentToken(req.UserID, req.Consent)
	require.NoError(t, err)
	return token
}

func TestServeGet(t *testing.T) {
	tokens := testTokens(t)
	req := testRequest()
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			w := serve(tokens, req, method, nil)
			require.Equal(t, http.StatusOK, w.Code)
			h := w.Header()
			assert.Equal(t, "text/html; charset=utf-8", h.Get("Content-Type"))
			assert.Equal(t, "no-store", h.Get("Cache-Control"))
			assert.Equal(t, "DENY", h.Get("X-Frame-Options"))
			assert.Contains(t, h.Get("Content-Security-Policy"), "frame-ancestors 'none'")
			// The POST redirects to the app; form-action must allow it.
			assert.Contains(t, h.Get("Content-Security-Policy"), "form-action 'self' http://localhost:3118;")

			body := w.Body.String()
			assert.Contains(t, body, "Claude &lt;b&gt;Code&lt;/b&gt;")
			assert.NotContains(t, body, "<b>Code</b>")
			assert.Contains(t, body, "an app on this computer (localhost:3118)")
			assert.Contains(t, body, "ann@x.com")
			assert.Contains(t, body, "Unverified name")
			assert.Contains(t, body, "“shop”")

			// The nonce in the header is the one on the script.
			csp := h.Get("Content-Security-Policy")
			nonce := csp[strings.Index(csp, "'nonce-")+7:]
			nonce = nonce[:strings.Index(nonce, "'")]
			assert.Contains(t, body, `<script nonce="`+nonce+`">`)
		})
	}
}

func TestServeGetUnnamedUnregistered(t *testing.T) {
	req := testRequest()
	req.ClientName, req.Registered, req.Email = "", false, ""
	req.RegisteredRedirectURIs = nil
	req.Consent.RedirectURI = "cursor://anysphere.cursor-retrieval/oauth/callback"
	w := serve(testTokens(t), req, http.MethodGet, nil)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "An unnamed app")
	assert.Contains(t, body, "Unknown app")
	assert.Contains(t, body, "the app that opens cursor: links")
	assert.NotContains(t, body, "Signed in as")
}

func TestServeGetTokenFails(t *testing.T) {
	w := serve(failingTokens{Tokens: testTokens(t), failConsent: true}, testRequest(), http.MethodGet, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestServeRefusesUncheckedRedirect(t *testing.T) {
	for name, mutate := range map[string]func(*Request){
		"not registered": func(r *Request) { r.Consent.RedirectURI = "https://evil.com/cb" },
		// CheckRedirectURI would fill in the only registered one, but the
		// caller had to pass it in already checked.
		"empty": func(r *Request) { r.Consent.RedirectURI = "" },
	} {
		t.Run(name, func(t *testing.T) {
			req := testRequest()
			req.RegisteredRedirectURIs = []string{"http://localhost/callback"}
			mutate(&req)
			w := serve(testTokens(t), req, http.MethodGet, nil)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), ErrRedirectMalformed.Error())
		})
	}
}

func TestServePostAllow(t *testing.T) {
	tokens := testTokens(t)
	req := testRequest()
	w := serve(tokens, req, http.MethodPost, url.Values{"consent": {consentToken(t, tokens, req)}, "decision": {DecisionAllow}})
	require.Equal(t, http.StatusFound, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "localhost:3118", loc.Host)
	assert.Equal(t, "/callback", loc.Path)
	assert.Equal(t, "st", loc.Query().Get("state"))

	grant, err := tokens.RedeemCode(loc.Query().Get("code"), "db1", "c1", req.Consent.RedirectURI, "verifier")
	require.NoError(t, err)
	assert.Equal(t, auth.Grant{UserID: "u1"}, grant)
}

func cliRequest() Request {
	req := testRequest()
	req.CLI, req.ClientName, req.Registered = true, "SQL MCP CLI", false
	req.Consent.DatabaseID, req.Consent.ClientID = auth.CLIResource, auth.CLIClientID
	req.Consent.RedirectURI = "http://127.0.0.1:5000/callback"
	req.RegisteredRedirectURIs = []string{"http://127.0.0.1/callback"}
	req.Databases = []Choice{{ID: "db1", Name: "shop <prod>"}, {ID: "db2", Name: "crm"}, {ID: "db3", Name: "hr"}}
	return req
}

func TestServeCLIPage(t *testing.T) {
	tokens := testTokens(t)
	w := serve(tokens, cliRequest(), http.MethodGet, nil)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "sqlmcp login")
	assert.Contains(t, body, `value="db2"`)
	assert.Contains(t, body, "shop &lt;prod&gt;")
	assert.Contains(t, body, "can’t unmark")
	assert.NotContains(t, body, "Unverified name")

	req := cliRequest()
	req.Databases = nil
	w = serve(tokens, req, http.MethodGet, nil)
	assert.Contains(t, w.Body.String(), `data-test="consent-no-databases"`)
}

func TestServeCLIAllowCarriesPickedDatabases(t *testing.T) {
	tokens := testTokens(t)
	req := cliRequest()
	// db9 was never offered, so it is dropped; the order is the page's.
	w := serve(tokens, req, http.MethodPost, url.Values{
		"consent": {consentToken(t, tokens, req)}, "decision": {DecisionAllow}, "db": {"db3", "db9", "db1"},
	})
	require.Equal(t, http.StatusFound, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	grant, err := tokens.RedeemCode(loc.Query().Get("code"), auth.CLIResource, auth.CLIClientID, "", "verifier")
	require.NoError(t, err)
	assert.Equal(t, auth.Grant{UserID: "u1", Databases: []string{"db1", "db3"}}, grant)
}

func TestServeCLIAllowNeedsADatabase(t *testing.T) {
	tokens := testTokens(t)
	req := cliRequest()
	w := serve(tokens, req, http.MethodPost, url.Values{
		"consent": {consentToken(t, tokens, req)}, "decision": {DecisionAllow}, "db": {"db9"},
	})
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Pick at least one database.")
	assert.Empty(t, w.Header().Get("Location"))
}

func TestServePostDeny(t *testing.T) {
	tokens := testTokens(t)
	req := testRequest()
	req.RegisteredRedirectURIs = []string{"https://app.com/cb?tenant=1"}
	req.Consent.RedirectURI = "https://app.com/cb?tenant=1"
	w := serve(tokens, req, http.MethodPost, url.Values{"consent": {consentToken(t, tokens, req)}, "decision": {DecisionDeny}})
	require.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://app.com/cb?tenant=1&error=access_denied&state=st", w.Header().Get("Location"))
}

func TestServePostRefuses(t *testing.T) {
	tokens := testTokens(t)
	req := testRequest()
	good := consentToken(t, tokens, req)
	otherReq := req
	otherReq.Consent.DatabaseID = "db2"
	other := consentToken(t, tokens, otherReq)

	for _, tc := range []struct {
		name   string
		tokens Tokens
		form   url.Values
		status int
	}{
		{"no token", tokens, url.Values{"decision": {DecisionAllow}}, http.StatusBadRequest},
		{"bad token", tokens, url.Values{"consent": {"x.y.z"}, "decision": {DecisionAllow}}, http.StatusBadRequest},
		{"token for another request", tokens, url.Values{"consent": {other}, "decision": {DecisionAllow}}, http.StatusBadRequest},
		{"bad decision", tokens, url.Values{"consent": {good}, "decision": {"maybe"}}, http.StatusBadRequest},
		{"no decision", tokens, url.Values{"consent": {good}}, http.StatusBadRequest},
		{"code fails", failingTokens{Tokens: tokens, failCode: true}, url.Values{"consent": {good}, "decision": {DecisionAllow}}, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := serve(tc.tokens, req, http.MethodPost, tc.form)
			assert.Equal(t, tc.status, w.Code)
			assert.Empty(t, w.Header().Get("Location"))
		})
	}
}

func TestServePostInvalidForm(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/oauth2/authorize", strings.NewReader("consent=%zz"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	Serve(w, r, testTokens(t), testRequest())
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid form")
}

func TestServeMethodNotAllowed(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
		w := serve(testTokens(t), testRequest(), method, nil)
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code, method)
		assert.Equal(t, "GET, HEAD, POST", w.Header().Get("Allow"))
	}
}

func TestCheckRedirectURI(t *testing.T) {
	const base = "https://sql.example.com"
	long := "https://" + strings.Repeat("a.", 127) + "com/cb"
	for _, tc := range []struct {
		name       string
		requested  string
		registered []string
		want       string
		err        error
	}{
		{"only registered is the default", "", []string{"https://app.com/cb"}, "https://app.com/cb", nil},
		{"missing with none registered", "", nil, "", ErrRedirectMissing},
		{"missing with two registered", "", []string{"https://a.com/cb", "https://b.com/cb"}, "", ErrRedirectMissing},
		{"relative", "/cb", nil, "", ErrRedirectMalformed},
		{"unparsable", "http://[::1/cb", nil, "", ErrRedirectMalformed},
		{"fragment", "https://sql.example.com/cb#x", nil, "", ErrRedirectFragment},
		{"empty fragment", "https://sql.example.com/cb#", nil, "", ErrRedirectFragment},
		{"userinfo", "https://u:p@sql.example.com/cb", nil, "", ErrRedirectUserinfo},
		{"bad host", "https://bad_host.com/cb", nil, "", ErrRedirectHost},
		{"too long host", long, nil, "", ErrRedirectHost},
		{"empty host", "https:///cb", nil, "", ErrRedirectHost},
		{"registered exact", "https://app.com/cb", []string{"https://app.com/cb"}, "https://app.com/cb", nil},
		{"registered loopback any port", "http://127.0.0.1:5555/cb", []string{"http://127.0.0.1/cb"}, "http://127.0.0.1:5555/cb", nil},
		{"registered custom scheme", "myapp://cb", []string{"myapp://cb"}, "myapp://cb", nil},
		{"not registered", "https://sql.example.com/cb", []string{"https://app.com/cb"}, "", ErrRedirectNotRegistered},
		{"known native scheme", "vscode://cb", nil, "vscode://cb", nil},
		{"unknown native scheme", "myapp://cb", nil, "", ErrRedirectScheme},
		{"javascript", "javascript:alert(1)", nil, "", ErrRedirectScheme},
		{"localhost", "http://localhost:9/cb", nil, "http://localhost:9/cb", nil},
		{"127.0.0.1", "http://127.0.0.1/cb", nil, "http://127.0.0.1/cb", nil},
		{"::1", "http://[::1]:9/cb", nil, "http://[::1]:9/cb", nil},
		{"plain http elsewhere", "http://sql.example.com/cb", nil, "", ErrRedirectInsecure},
		{"own origin", "https://sql.example.com/cb", nil, "https://sql.example.com/cb", nil},
		{"other origin", "https://evil.com/cb", nil, "", ErrRedirectHostMismatch},
		{"own host other port", "https://sql.example.com:8443/cb", nil, "", ErrRedirectHostMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CheckRedirectURI(tc.requested, tc.registered, base)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	_, err := CheckRedirectURI("https://sql.example.com/cb", nil, "http://[::1")
	assert.ErrorIs(t, err, ErrRedirectHostMismatch)
}

func TestRedirectURL(t *testing.T) {
	params := url.Values{"code": {"c"}, "state": {"a b"}}
	assert.Equal(t, "https://app.com/cb?code=c&state=a+b", RedirectURL("https://app.com/cb", params))
	assert.Equal(t, "https://app.com/cb?x=1&code=c&state=a+b", RedirectURL("https://app.com/cb?x=1", params))
}

func TestLoginURL(t *testing.T) {
	assert.Equal(t,
		"https://sql.example.com/login?redirect_url=%2Foauth2%2Fauthorize%3Fa%3D1%26b%3D2",
		LoginURL("https://sql.example.com", "/oauth2/authorize?a=1&b=2"))
}

func TestDisplayHost(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
		ok   bool
	}{
		{"https://example.com/cb", "example.com", true},
		{"https://EXAMPLE.com/cb", "example.com", true},
		{"https://example.com:8443/cb", "example.com:8443", true},
		{"https://bücher.example/cb", "xn--bcher-kva.example", true},
		{"https://аpple.com/cb", "xn--pple-43d.com", true}, // Cyrillic а
		{"https://127.0.0.1:9/cb", "127.0.0.1:9", true},
		{"http://[::1]:9/cb", "[::1]:9", true},
		{"https://bad_host/cb", "", false},
		{"https:///cb", "", false},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			u, err := url.Parse(tc.raw)
			require.NoError(t, err)
			got, ok := displayHost(u)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDestination(t *testing.T) {
	for raw, want := range map[string]string{
		"cursor://cb":                    "the app that opens cursor: links",
		"http://localhost:3118/callback": "an app on this computer (localhost:3118)",
		"http://[::1]:9/cb":              "an app on this computer ([::1]:9)",
		"https://bücher.example/cb":      "xn--bcher-kva.example",
		"https://app.com:8443/cb":        "app.com:8443",
	} {
		assert.Equal(t, want, destination(raw), raw)
	}
}

func TestFormActionSource(t *testing.T) {
	for raw, want := range map[string]string{
		"cursor://cb":                    "cursor:",
		"http://localhost:3118/callback": "http://localhost:3118",
		"http://127.0.0.1:9/cb":          "http:",
		"http://[::1]:9/cb":              "http:",
		"https://bücher.example/cb?x=1":  "https://xn--bcher-kva.example",
		"https://app.com:8443/cb":        "https://app.com:8443",
	} {
		assert.Equal(t, want, formActionSource(raw), raw)
	}
}

// errWriter fails every write, as a connection the client dropped does.
type errWriter struct{ header http.Header }

func (w *errWriter) Header() http.Header       { return w.header }
func (w *errWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }
func (w *errWriter) WriteHeader(int)           {}

func TestRenderWriteFails(t *testing.T) {
	w := &errWriter{header: http.Header{}}
	render(w, http.StatusOK, newPage(testRequest(), "token"))
	assert.Equal(t, "no-store", w.header.Get("Cache-Control"))
}
