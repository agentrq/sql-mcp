// Package clilogin signs the sqlmcp command line in: OAuth 2.1 with PKCE and
// a loopback redirect (RFC 8252), for the CLI's built-in public client. The
// person picks on the consent page which of their databases the CLI may
// open, and its tokens carry those IDs and nothing more.
//
// Like the MCP grants, the code and tokens are signed tokens, not rows.
package clilogin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/agentrq/sql-mcp/backend/internal/controller/crud"
	"github.com/agentrq/sql-mcp/backend/internal/data/entity"
	"github.com/agentrq/sql-mcp/backend/internal/handler/oauthconsent"
	"github.com/agentrq/sql-mcp/backend/internal/service/auth"
)

// Paths of the authorization and token endpoints.
const (
	AuthorizePath = "/cli/oauth2/authorize"
	TokenPath     = "/cli/oauth2/token"
)

// redirectURIs are the CLI's only redirects: loopback, on whatever port it
// listens on (RFC 8252 §7.3), so a code never leaves the person's computer.
var redirectURIs = []string{"http://127.0.0.1/callback", "http://[::1]/callback", "http://localhost/callback"}

// Accounts is the part of the controller the endpoints need.
type Accounts interface {
	GetUser(ctx context.Context, userID int64) (*entity.User, error)
	ListDatabases(ctx context.Context, userID int64) ([]entity.Database, error)
}

// Params is what the handler needs.
type Params struct {
	Mux      *http.ServeMux
	Accounts Accounts
	Tokens   *auth.TokenService
	BaseURL  string
}

type handler struct {
	accounts Accounts
	tokens   *auth.TokenService
	baseURL  string
}

// Register mounts the endpoints on mux.
func Register(p Params) {
	h := &handler{accounts: p.Accounts, tokens: p.Tokens, baseURL: strings.TrimRight(p.BaseURL, "/")}
	p.Mux.HandleFunc(AuthorizePath, h.authorize)
	p.Mux.HandleFunc("POST "+TokenPath, h.token)
}

// authorize shows the signed-in person the consent page with their
// databases to pick from.
func (h *handler) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != auth.CLIClientID {
		http.Error(w, "invalid_client: only the sqlmcp CLI signs in here", http.StatusBadRequest)
		return
	}
	redirectURI, err := oauthconsent.CheckRedirectURI(q.Get("redirect_uri"), redirectURIs, h.baseURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// From here on, errors go back to the CLI (RFC 6749 §4.1.2.1).
	fail := func(code, description string) {
		http.Redirect(w, r, oauthconsent.RedirectURL(redirectURI, url.Values{"error": {code}, "error_description": {description}, "state": {q.Get("state")}}), http.StatusFound)
	}
	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "response_type must be code")
		return
	}
	if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		fail("invalid_request", "PKCE with code_challenge_method S256 is required")
		return
	}

	var claims *auth.Claims
	if cookie, err := r.Cookie("at"); err == nil {
		claims, _ = h.tokens.ValidateAccessToken(cookie.Value)
	}
	if claims == nil {
		http.Redirect(w, r, oauthconsent.LoginURL(h.baseURL, h.baseURL+r.URL.RequestURI()), http.StatusFound)
		return
	}
	dbs, err := h.accounts.ListDatabases(r.Context(), crud.ParseID(claims.Subject))
	if err != nil {
		log.Error().Err(err).Msg("could not list databases for the CLI consent page")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	choices := make([]oauthconsent.Choice, len(dbs))
	for i, d := range dbs {
		choices[i] = oauthconsent.Choice{ID: d.ID, Name: d.Name}
	}
	oauthconsent.Serve(w, r, h.tokens, oauthconsent.Request{
		UserID:     claims.Subject,
		Email:      claims.Email,
		ClientName: "SQL MCP CLI",
		CLI:        true,
		Databases:  choices,
		Consent: auth.Consent{
			DatabaseID:    auth.CLIResource,
			ClientID:      auth.CLIClientID,
			RedirectURI:   redirectURI,
			State:         q.Get("state"),
			CodeChallenge: q.Get("code_challenge"),
		},
		RegisteredRedirectURIs: redirectURIs,
		BaseURL:                h.baseURL,
	})
}

// token redeems a code or rotates a refresh token. Either way the person
// must still exist and still be on the allowlist, so removing someone ends
// their CLI within an access token's hour.
func (h *handler) token(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "invalid form")
		return
	}
	var grant auth.Grant
	var err error
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		grant, err = h.tokens.RedeemCode(r.PostForm.Get("code"), auth.CLIResource, r.PostForm.Get("client_id"), r.PostForm.Get("redirect_uri"), r.PostForm.Get("code_verifier"))
	case "refresh_token":
		grant, err = h.tokens.ValidateCLIRefreshToken(r.PostForm.Get("refresh_token"))
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
		return
	}
	if err == nil {
		_, err = h.accounts.GetUser(r.Context(), crud.ParseID(grant.UserID))
	}
	var access, refresh string
	if err == nil {
		access, refresh, err = h.tokens.CreateCLITokens(grant)
	}
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the grant is invalid or expired, or the account may no longer sign in")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"refresh_token": refresh,
		"token_type":    "Bearer",
		"expires_in":    int64(auth.CLIAccessTokenTTL.Seconds()),
		"databases":     grant.Databases,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func oauthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}
