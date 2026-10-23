package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// fakeGoogle answers the token endpoint for code "good", and serves
// userinfo as the test sets it.
type fakeGoogle struct {
	status int
	body   string
}

func (f *fakeGoogle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/token":
		_ = r.ParseForm()
		if r.PostForm.Get("code") != "good" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600})
	case "/userinfo":
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(f.body))
	default:
		http.NotFound(w, r)
	}
}

func testGoogle(t *testing.T, fake *fakeGoogle) *googleProvider {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	g := NewGoogle("cid", "csecret", "https://app/cb").(*googleProvider)
	g.config.Endpoint = oauth2.Endpoint{AuthURL: srv.URL + "/auth", TokenURL: srv.URL + "/token", AuthStyle: oauth2.AuthStyleInParams}
	g.userinfoURL = srv.URL + "/userinfo"
	return g
}

func TestGoogleAuthURL(t *testing.T) {
	g := NewGoogle("cid", "csecret", "https://app/cb")
	u, err := url.Parse(g.AuthURL("st"))
	require.NoError(t, err)
	assert.Equal(t, "accounts.google.com", u.Host)
	q := u.Query()
	assert.Equal(t, "cid", q.Get("client_id"))
	assert.Equal(t, "st", q.Get("state"))
	assert.Equal(t, "https://app/cb", q.Get("redirect_uri"))
	assert.Equal(t, "select_account", q.Get("prompt"))
	assert.Equal(t, "openid email profile", q.Get("scope"))
}

func TestGoogleExchange(t *testing.T) {
	g := testGoogle(t, &fakeGoogle{status: http.StatusOK, body: `{"email":"a@x.com","verified_email":true,"name":"Ann","picture":"p"}`})
	u, err := g.Exchange(context.Background(), "good")
	require.NoError(t, err)
	assert.Equal(t, &User{Email: "a@x.com", VerifiedEmail: true, Name: "Ann", Picture: "p"}, u)
}

func TestGoogleExchangeFails(t *testing.T) {
	for _, tc := range []struct {
		name   string
		code   string
		status int
		body   string
		want   string
	}{
		{"bad code", "bad", http.StatusOK, `{}`, "google: exchange"},
		{"userinfo status", "good", http.StatusInternalServerError, ``, "userinfo status 500"},
		{"userinfo not json", "good", http.StatusOK, `nope`, "decode userinfo"},
		{"unverified", "good", http.StatusOK, `{"email":"a@x.com","verified_email":false}`, "not verified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := testGoogle(t, &fakeGoogle{status: tc.status, body: tc.body})
			_, err := g.Exchange(context.Background(), tc.code)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestGoogleUserinfoUnreachable(t *testing.T) {
	g := testGoogle(t, &fakeGoogle{status: http.StatusOK})
	g.userinfoURL = "http://127.0.0.1:1/userinfo"
	_, err := g.Exchange(context.Background(), "good")
	assert.ErrorContains(t, err, "google: userinfo")
}
