package oauth

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// server is a fake token endpoint that answers with answer.
func server(t *testing.T, answer func(w http.ResponseWriter, form url.Values)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, TokenPath, r.URL.Path)
		require.NoError(t, r.ParseForm())
		answer(w, r.PostForm)
	}))
	t.Cleanup(srv.Close)
	return &Client{Server: srv.URL, HTTP: srv.Client(), Now: func() time.Time { return now }}
}

func grant(w http.ResponseWriter, _ url.Values) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","token_type":"Bearer","expires_in":3600,"databases":["db1"]}`))
}

func TestExchangeAndRefresh(t *testing.T) {
	var forms []url.Values
	c := server(t, func(w http.ResponseWriter, form url.Values) { forms = append(forms, form); grant(w, form) })
	tok, err := c.Exchange(context.Background(), "code", "http://127.0.0.1:1/callback", "verifier")
	require.NoError(t, err)
	assert.Equal(t, &Token{AccessToken: "at", RefreshToken: "rt", Expiry: now.Add(time.Hour), Databases: []string{"db1"}}, tok)
	_, err = c.Refresh(context.Background(), "old")
	require.NoError(t, err)

	assert.Equal(t, "authorization_code", forms[0].Get("grant_type"))
	assert.Equal(t, ClientID, forms[0].Get("client_id"))
	assert.Equal(t, "verifier", forms[0].Get("code_verifier"))
	assert.Equal(t, "refresh_token", forms[1].Get("grant_type"))
	assert.Equal(t, "old", forms[1].Get("refresh_token"))
}

func TestTokenErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"oauth error", 400, `{"error":"invalid_grant","error_description":"expired"}`, "invalid_grant: expired"},
		{"oauth error, no description", 400, `{"error":"invalid_grant"}`, "invalid_grant"},
		{"not oauth", 502, `<html>`, "server_error: the token endpoint answered 502 Bad Gateway"},
		{"bad json", 200, `{`, "read the token answer"},
		{"no token", 200, `{"token_type":"Bearer"}`, "no bearer token"},
		{"not bearer", 200, `{"access_token":"x","token_type":"mac"}`, "no bearer token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := server(t, func(w http.ResponseWriter, _ url.Values) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := c.Refresh(context.Background(), "rt")
			assert.ErrorContains(t, err, tc.want)
		})
	}

	c := &Client{Server: "http://127.0.0.1:1", HTTP: &http.Client{}, Now: time.Now}
	_, err := c.Refresh(context.Background(), "rt")
	assert.Error(t, err, "nothing listens there")
	c.Server = "http://bad host"
	_, err = c.Refresh(context.Background(), "rt")
	assert.Error(t, err, "not a URL")
}

// browse plays the person: it opens the consent URL and follows the
// redirect to the CLI's callback with what makeBack adds.
func browse(t *testing.T, makeBack func(q url.Values) url.Values) func(string) error {
	return func(raw string) error {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		q := u.Query()
		assert.Equal(t, AuthorizePath, u.Path)
		assert.Equal(t, ClientID, q.Get("client_id"))
		assert.Equal(t, "S256", q.Get("code_challenge_method"))
		back := makeBack(q)
		go func() {
			res, err := http.Get(q.Get("redirect_uri") + "?" + back.Encode())
			if err == nil {
				res.Body.Close()
			}
		}()
		return nil
	}
}

func TestLogin(t *testing.T) {
	var verifier string
	c := server(t, func(w http.ResponseWriter, form url.Values) {
		verifier = form.Get("code_verifier")
		assert.Equal(t, "the-code", form.Get("code"))
		grant(w, form)
	})
	var challenge string
	var out bytes.Buffer
	tok, err := c.Login(context.Background(), LoginOptions{Out: &out, Timeout: 5 * time.Second, Open: browse(t, func(q url.Values) url.Values {
		challenge = q.Get("code_challenge")
		return url.Values{"code": {"the-code"}, "state": {q.Get("state")}}
	})})
	require.NoError(t, err)
	assert.Equal(t, "at", tok.AccessToken)
	assert.Equal(t, Challenge(verifier), challenge, "the verifier matches the challenge sent")
	assert.Contains(t, out.String(), AuthorizePath)
}

func TestLoginFailures(t *testing.T) {
	c := server(t, grant)
	for _, tc := range []struct {
		name string
		back func(q url.Values) url.Values
		want string
	}{
		{"wrong state", func(url.Values) url.Values { return url.Values{"code": {"c"}, "state": {"forged"}} }, "wrong state"},
		{"denied", func(q url.Values) url.Values {
			return url.Values{"error": {"access_denied"}, "state": {q.Get("state")}}
		}, "sign-in failed: access_denied"},
		{"no code", func(q url.Values) url.Values { return url.Values{"state": {q.Get("state")}} }, "without a code"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.Login(context.Background(), LoginOptions{Out: &bytes.Buffer{}, Timeout: 5 * time.Second, Open: browse(t, tc.back)})
			assert.ErrorContains(t, err, tc.want)
		})
	}

	// The browser does not open, and nobody comes back.
	var out bytes.Buffer
	_, err := c.Login(context.Background(), LoginOptions{Out: &out, Timeout: 50 * time.Millisecond, Open: func(string) error { return errors.New("no display") }})
	assert.ErrorContains(t, err, "gave up after 50ms")
	assert.Contains(t, out.String(), "Could not open the browser: no display")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Login(ctx, LoginOptions{Out: &out, Timeout: time.Minute, Open: func(string) error { return nil }})
	assert.EqualError(t, err, "login canceled")

	old := listen
	t.Cleanup(func() { listen = old })
	listen = func(string, string) (net.Listener, error) { return nil, errors.New("no ports") }
	_, err = c.Login(context.Background(), LoginOptions{Out: &out, Timeout: time.Minute})
	assert.ErrorContains(t, err, "no ports")
}

func TestCallbackCountsOnce(t *testing.T) {
	results := make(chan result, 1)
	h := callback("st", results)
	for range 2 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/callback?state=st&code=c", nil))
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "You can close this tab.")
	}
	assert.Equal(t, "c", (<-results).code)
	assert.Empty(t, results)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/callback?state=no", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBrowser(t *testing.T) {
	assert.Equal(t, []string{"open", "u"}, browser("darwin", "u").Args)
	assert.Equal(t, []string{"rundll32", "url.dll,FileProtocolHandler", "u"}, browser("windows", "u").Args)
	assert.Equal(t, []string{"xdg-open", "u"}, browser("linux", "u").Args)
	if runtime.GOOS == "linux" {
		t.Setenv("PATH", t.TempDir())
		assert.Error(t, OpenBrowser("http://example.com"), "no xdg-open on an empty PATH")
	}
}
