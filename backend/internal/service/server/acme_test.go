package server

import (
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeACME is just enough of an ACME CA, over https as lego insists, to
// register an account; registration fails when refuseAccount is set, and
// every order is refused so nothing is ever issued.
func fakeACME(t *testing.T, refuseAccount bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	problem := func(w http.ResponseWriter, status int, detail string) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.Header().Set("Replay-Nonce", "nonce")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "urn:ietf:params:acme:error:unauthorized", "detail": detail, "status": status})
	}
	mux.HandleFunc("/dir", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"newNonce":   srv.URL + "/nonce",
			"newAccount": srv.URL + "/account",
			"newOrder":   srv.URL + "/order",
			"revokeCert": srv.URL + "/revoke",
			"keyChange":  srv.URL + "/key",
		})
	})
	mux.HandleFunc("/nonce", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Replay-Nonce", "nonce")
	})
	mux.HandleFunc("/account", func(w http.ResponseWriter, _ *http.Request) {
		if refuseAccount {
			problem(w, http.StatusForbidden, "no accounts today")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Replay-Nonce", "nonce")
		w.Header().Set("Location", srv.URL+"/account/1")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "valid"})
	})
	mux.HandleFunc("/order", func(w http.ResponseWriter, _ *http.Request) {
		problem(w, http.StatusForbidden, "no orders today")
	})
	srv = httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	// lego trusts the CA certificates this names.
	ca := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600))
	t.Setenv("LEGO_CA_CERTIFICATES", ca)
	return srv
}

func acmeServer(t *testing.T, dirURL, cfToken string) *Server {
	t.Helper()
	s := testServer(t)
	s.cfg.ACMEDirectoryURL = dirURL
	s.cfg.CloudflareAPIToken = cfToken
	return s
}

func TestNewObtainerFails(t *testing.T) {
	notACME := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(notACME.Close)

	for _, tc := range []struct {
		name        string
		dir         func(t *testing.T) string
		token, want string
	}{
		{"plain http", func(*testing.T) string { return notACME.URL + "/dir" }, "cf", "ACME client"},
		{"no directory", func(t *testing.T) string { return fakeACME(t, false).URL + "/missing" }, "cf", "ACME client"},
		{"no cloudflare token", func(t *testing.T) string { return fakeACME(t, false).URL + "/dir" }, "", "cloudflare DNS provider"},
		{"registration refused", func(t *testing.T) string { return fakeACME(t, true).URL + "/dir" }, "cf", "ACME registration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := acmeServer(t, tc.dir(t), tc.token)
			assert.ErrorContains(t, s.RefreshCertificate(), tc.want)
			assert.Nil(t, s.obtainer)
		})
	}
}

func TestNewObtainerRegisters(t *testing.T) {
	s := acmeServer(t, fakeACME(t, false).URL+"/dir", "cf")
	s.cfg.LetsencryptEmail = "ops@example.com"
	err := s.RefreshCertificate()
	require.Error(t, err, "the fake CA issues nothing")
	assert.NotNil(t, s.obtainer, "the registered client is kept for the next try")
	assert.Nil(t, s.cert)
}

func TestACMEUser(t *testing.T) {
	u := &acmeUser{email: "a@x.com", key: "k"}
	assert.Equal(t, "a@x.com", u.GetEmail())
	assert.Nil(t, u.GetRegistration())
	assert.Equal(t, "k", u.GetPrivateKey())
}
