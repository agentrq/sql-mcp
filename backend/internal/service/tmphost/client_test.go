package tmphost

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTmphost emulates the parts of tmphost.io the client uses. Override
// replaces the answer for one path.
type fakeTmphost struct {
	srv            *httptest.Server
	metadataCalls  atomic.Int32
	metadataIssuer string
	metadataBase   string // where the endpoints point; the server itself by default
	override       map[string]http.HandlerFunc
	last           *http.Request
	lastBody       []byte
}

func newFake(t *testing.T) *fakeTmphost {
	t.Helper()
	f := &fakeTmphost{override: map[string]http.HandlerFunc{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTmphost) client() *Client { return New(f.srv.URL+"/", f.srv.Client()) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeTmphost) serve(w http.ResponseWriter, r *http.Request) {
	f.last = r
	f.lastBody, _ = io.ReadAll(r.Body)
	if h, ok := f.override[r.URL.Path]; ok {
		h(w, r)
		return
	}
	authorized := r.Header.Get("Authorization") == "Bearer at"
	switch {
	case r.URL.Path == "/.well-known/oauth-authorization-server":
		f.metadataCalls.Add(1)
		base := f.metadataBase
		if base == "" {
			base = f.srv.URL
		}
		issuer := f.metadataIssuer
		if issuer == "" {
			issuer = f.srv.URL
		}
		writeJSON(w, http.StatusOK, Metadata{
			Issuer:                issuer,
			AuthorizationEndpoint: base + "/oauth2/authorize",
			TokenEndpoint:         base + "/oauth2/token",
			RegistrationEndpoint:  base + "/oauth2/register",
		})
	case r.URL.Path == "/oauth2/register" && r.Method == http.MethodPost:
		writeJSON(w, http.StatusCreated, map[string]any{"client_id": "cid"})
	case r.URL.Path == "/oauth2/token" && r.Method == http.MethodPost:
		form, _ := url.ParseQuery(string(f.lastBody))
		switch {
		case form.Get("grant_type") == "authorization_code" && form.Get("code") == "good":
		case form.Get("grant_type") == "refresh_token" && form.Get("refresh_token") == "rt":
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "code is spent"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"access_token": "at", "refresh_token": "rt2", "expires_in": 3600})
	case !authorized:
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
	case r.URL.Path == "/api/v1/users/me":
		writeJSON(w, http.StatusOK, map[string]any{"user": map[string]any{"id": "u1", "email": "ann@x.com"}})
	case r.URL.Path == "/api/v1/folders" && r.Method == http.MethodPost:
		writeJSON(w, http.StatusCreated, map[string]any{"folder": map[string]any{
			"id": "f1", "url": "https://tmphost.io/f/f1", "expiresAt": "2026-01-02T03:04:05Z",
		}})
	case strings.HasPrefix(r.URL.EscapedPath(), "/api/v1/folders/f1/files/") && r.Method == http.MethodPut:
		writeJSON(w, http.StatusOK, map[string]any{"file": map[string]any{
			"path": strings.TrimPrefix(r.URL.Path, "/api/v1/folders/f1/files/"), "size": len(f.lastBody), "url": "https://tmphost.io/f/f1/x",
		}})
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"code": "not_found"}})
	}
}

func TestNew(t *testing.T) {
	c := New("https://app.tmphost.io/", nil)
	assert.Equal(t, "https://app.tmphost.io", c.BaseURL())
	assert.Equal(t, 60*time.Second, c.http.Timeout)
}

func TestMetadataIsFetchedOnce(t *testing.T) {
	f := newFake(t)
	c := f.client()
	for range 3 {
		m, err := c.Metadata(context.Background())
		require.NoError(t, err)
		assert.Equal(t, f.srv.URL+"/oauth2/token", m.TokenEndpoint)
	}
	assert.EqualValues(t, 1, f.metadataCalls.Load())
}

func TestMetadataRefusesEndpointsElsewhere(t *testing.T) {
	f := newFake(t)
	for _, base := range []string{"https://evil.com", f.srv.URL + ".evil.com", strings.Replace(f.srv.URL, "http://", "https://", 1)} {
		t.Run(base, func(t *testing.T) {
			f.metadataBase = base
			c := f.client()
			_, err := c.Metadata(context.Background())
			assert.ErrorContains(t, err, "is not on "+f.srv.URL)
			// Every call that needs the metadata refuses too.
			_, err = c.Register(context.Background(), "https://app/cb", "SQL MCP")
			assert.Error(t, err)
			_, err = c.AuthURL(context.Background(), "cid", "https://app/cb", "st", "ch")
			assert.Error(t, err)
			_, err = c.Exchange(context.Background(), "cid", "https://app/cb", "good", "v")
			assert.Error(t, err)
			assert.Error(t, c.CheckIssuer(context.Background(), "x"))
		})
	}
}

func TestMetadataErrorIsNotCached(t *testing.T) {
	f := newFake(t)
	f.override["/.well-known/oauth-authorization-server"] = func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}
	c := f.client()
	_, err := c.Metadata(context.Background())
	require.Error(t, err)
	delete(f.override, "/.well-known/oauth-authorization-server")
	_, err = c.Metadata(context.Background())
	assert.NoError(t, err)
}

func TestRegister(t *testing.T) {
	f := newFake(t)
	c := f.client()
	id, err := c.Register(context.Background(), "https://app/cb", "SQL MCP")
	require.NoError(t, err)
	assert.Equal(t, "cid", id)
	assert.Equal(t, "application/json", f.last.Header.Get("Content-Type"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(f.lastBody, &body))
	assert.Equal(t, "SQL MCP", body["client_name"])
	assert.Equal(t, []any{"https://app/cb"}, body["redirect_uris"])
	assert.Equal(t, "none", body["token_endpoint_auth_method"])

	f.override["/oauth2/register"] = func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusCreated, map[string]any{}) }
	_, err = c.Register(context.Background(), "https://app/cb", "SQL MCP")
	assert.ErrorContains(t, err, "no client_id")

	f.override["/oauth2/register"] = func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_redirect_uri"})
	}
	_, err = c.Register(context.Background(), "https://app/cb", "SQL MCP")
	assert.EqualError(t, err, "tmphost: 400 invalid_redirect_uri")
}

func TestAuthURL(t *testing.T) {
	f := newFake(t)
	raw, err := f.client().AuthURL(context.Background(), "cid", "https://app/cb", "st", "ch")
	require.NoError(t, err)
	u, err := url.Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, f.srv.URL+"/oauth2/authorize", u.Scheme+"://"+u.Host+u.Path)
	assert.Equal(t, url.Values{
		"response_type":         {"code"},
		"client_id":             {"cid"},
		"redirect_uri":          {"https://app/cb"},
		"state":                 {"st"},
		"code_challenge":        {"ch"},
		"code_challenge_method": {"S256"},
	}, u.Query())
}

func TestCheckIssuer(t *testing.T) {
	f := newFake(t)
	c := f.client()
	assert.NoError(t, c.CheckIssuer(context.Background(), ""))
	assert.Zero(t, f.metadataCalls.Load(), "no iss, nothing to check")
	assert.NoError(t, c.CheckIssuer(context.Background(), f.srv.URL))
	assert.ErrorContains(t, c.CheckIssuer(context.Background(), "https://evil.com"), "another issuer")
}

func TestExchangeAndRefresh(t *testing.T) {
	f := newFake(t)
	c := f.client()

	tok, err := c.Exchange(context.Background(), "cid", "https://app/cb", "good", "verifier")
	require.NoError(t, err)
	assert.Equal(t, &Token{AccessToken: "at", RefreshToken: "rt2", ExpiresIn: 3600}, tok)
	assert.Equal(t, "application/x-www-form-urlencoded", f.last.Header.Get("Content-Type"))
	form, _ := url.ParseQuery(string(f.lastBody))
	assert.Equal(t, "verifier", form.Get("code_verifier"))
	assert.Equal(t, "cid", form.Get("client_id"))
	assert.Equal(t, "https://app/cb", form.Get("redirect_uri"))

	tok, err = c.Refresh(context.Background(), "cid", "rt")
	require.NoError(t, err)
	assert.Equal(t, "at", tok.AccessToken)

	_, err = c.Exchange(context.Background(), "cid", "https://app/cb", "bad", "verifier")
	assert.EqualError(t, err, "tmphost: 400 invalid_grant: code is spent")
	_, err = c.Refresh(context.Background(), "cid", "old")
	assert.Error(t, err)

	f.override["/oauth2/token"] = func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, map[string]any{}) }
	_, err = c.Refresh(context.Background(), "cid", "rt")
	assert.ErrorContains(t, err, "no access_token")
}

func TestAccount(t *testing.T) {
	f := newFake(t)
	c := f.client()
	got, err := c.Account(context.Background(), "at")
	require.NoError(t, err)
	assert.Equal(t, "ann@x.com", got)

	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"top level email", `{"email":"bob@x.com"}`, "bob@x.com"},
		{"name when no email", `{"user":{"email":"","name":"Bob"}}`, "Bob"},
		{"id when no name", `{"user":{"id":"u9","name":7}}`, "u9"},
		{"nothing", `{"user":{}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.override["/api/v1/users/me"] = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, tc.body) }
			got, err := c.Account(context.Background(), "at")
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	delete(f.override, "/api/v1/users/me")
	_, err = c.Account(context.Background(), "expired")
	assert.True(t, IsUnauthorized(err))
}

func TestCreateFolder(t *testing.T) {
	f := newFake(t)
	c := f.client()
	folder, err := c.CreateFolder(context.Background(), "at", "results", 60)
	require.NoError(t, err)
	assert.Equal(t, &Folder{ID: "f1", URL: "https://tmphost.io/f/f1", ExpiresAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}, folder)
	assert.Equal(t, "Bearer at", f.last.Header.Get("Authorization"))
	assert.JSONEq(t, `{"ttlMinutes":60,"title":"results"}`, string(f.lastBody))

	_, err = c.CreateFolder(context.Background(), "at", "", 0)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(f.lastBody))

	_, err = c.CreateFolder(context.Background(), "nope", "", 0)
	assert.True(t, IsUnauthorized(err))
}

func TestPutFile(t *testing.T) {
	f := newFake(t)
	c := f.client()
	file, err := c.PutFile(context.Background(), "at", "f1", "result 1.csv", []byte("a,b\n1,2\n"), "text/csv")
	require.NoError(t, err)
	assert.Equal(t, &File{Path: "result 1.csv", Size: 8, URL: "https://tmphost.io/f/f1/x"}, file)
	assert.Equal(t, "/api/v1/folders/f1/files/result%201.csv", f.last.URL.EscapedPath())
	assert.Equal(t, "text/csv", f.last.Header.Get("Content-Type"))
	assert.Equal(t, "application/json", f.last.Header.Get("Accept"))

	// The folder ID is escaped, so it cannot climb out of the folder.
	_, err = c.PutFile(context.Background(), "at", "../f1", "x", nil, "text/plain")
	assert.Error(t, err)
	assert.Equal(t, "/api/v1/folders/..%2Ff1/files/x", f.last.URL.EscapedPath())
}

func TestErrorMapping(t *testing.T) {
	f := newFake(t)
	c := f.client()
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"oauth error", http.StatusBadRequest, `{"error":"invalid_request","error_description":"bad"}`, "tmphost: 400 invalid_request: bad"},
		{"oauth error no description", http.StatusForbidden, `{"error":"forbidden"}`, "tmphost: 403 forbidden"},
		{"structured error", http.StatusNotFound, `{"error":{"code":"not_found"}}`, "tmphost: 404 map[code:not_found]"},
		{"plain text", http.StatusBadGateway, " upstream down \n", "tmphost: 502 upstream down"},
		{"json without error", http.StatusInternalServerError, `{"message":"x"}`, `tmphost: 500 {"message":"x"}`},
		{"redirect", http.StatusMultipleChoices, ``, "tmphost: 300 "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.override["/api/v1/users/me"] = func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}
			_, err := c.Account(context.Background(), "at")
			var e *Error
			require.ErrorAs(t, err, &e)
			assert.Equal(t, tc.status, e.Status)
			assert.EqualError(t, err, tc.want)
			assert.False(t, IsUnauthorized(err))
		})
	}
}

func TestDoFailures(t *testing.T) {
	f := newFake(t)
	c := f.client()

	f.override["/api/v1/users/me"] = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "not json") }
	_, err := c.Account(context.Background(), "at")
	assert.ErrorContains(t, err, "tmphost: decode "+f.srv.URL+"/api/v1/users/me")

	// The body is cut short mid-read.
	f.override["/api/v1/users/me"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "{")
	}
	_, err = c.Account(context.Background(), "at")
	assert.ErrorContains(t, err, "tmphost:")

	// An endpoint that cannot be a request.
	assert.Error(t, c.do(context.Background(), "BAD METHOD", f.srv.URL, "", nil, "", nil))

	// No answer to decode into.
	delete(f.override, "/api/v1/users/me")
	assert.NoError(t, c.do(context.Background(), http.MethodGet, f.srv.URL+"/api/v1/users/me", "at", nil, "", nil))

	unreachable := New("http://127.0.0.1:1", nil)
	_, err = unreachable.Account(context.Background(), "at")
	assert.ErrorContains(t, err, "tmphost:")
	assert.False(t, IsUnauthorized(err))
}

func TestIsUnauthorized(t *testing.T) {
	assert.True(t, IsUnauthorized(&Error{Status: http.StatusUnauthorized}))
	assert.True(t, IsUnauthorized(errors.Join(errors.New("x"), &Error{Status: http.StatusUnauthorized})))
	assert.False(t, IsUnauthorized(&Error{Status: http.StatusForbidden}))
	assert.False(t, IsUnauthorized(errors.New("401")))
	assert.False(t, IsUnauthorized(nil))
}
