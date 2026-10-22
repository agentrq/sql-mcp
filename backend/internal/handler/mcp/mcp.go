// Package mcp serves each database's MCP server at /mcp/{id}, and the OAuth
// 2.1 authorization server in front of it: RFC 8414 and RFC 9728 discovery,
// RFC 7591 dynamic client registration, a consent page, and a token endpoint
// that holds every code to its client, redirect_uri and PKCE verifier.
//
// Every registration and grant is a signed token rather than a row, as in
// AgentRQ: deleting a database is what revokes its grants, because a token
// is accepted only while its database exists and belongs to its subject.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/agentrq/sql-mcp/backend/internal/controller/crud"
	"github.com/agentrq/sql-mcp/backend/internal/controller/mcpserver"
	"github.com/agentrq/sql-mcp/backend/internal/data/entity"
	"github.com/agentrq/sql-mcp/backend/internal/handler/oauthconsent"
	"github.com/agentrq/sql-mcp/backend/internal/service/auth"
)

// Owners looks up who owns a database.
type Owners interface {
	SystemDatabaseOwner(ctx context.Context, id int64) (ownerID int64, name string, err error)
}

// oauthScope is the only scope. Whatever a client asks for it is granted
// this one, and told so (RFC 6749 §3.3).
const oauthScope = "sql"

// Params is what the handler needs.
type Params struct {
	Mux     *http.ServeMux
	Owners  Owners
	Tokens  *auth.TokenService
	MCP     http.Handler
	BaseURL string
}

type handler struct {
	owners  Owners
	tokens  *auth.TokenService
	mcp     http.Handler
	baseURL string
}

// Register mounts the MCP and OAuth routes on mux.
func Register(p Params) {
	h := &handler{owners: p.Owners, tokens: p.Tokens, mcp: p.MCP, baseURL: strings.TrimRight(p.BaseURL, "/")}
	p.Mux.Handle("/mcp/{id}", cors(http.HandlerFunc(h.serveMCP)))
	// RFC 8414 §3.1 and RFC 9728 §3.1 put the well-known suffix between the
	// host and the issuer's path; the suffixed forms are for clients that
	// append it instead.
	p.Mux.Handle("GET /.well-known/oauth-authorization-server/mcp/{id}", cors(http.HandlerFunc(h.authServerMetadata)))
	p.Mux.Handle("GET /mcp/{id}/.well-known/oauth-authorization-server", cors(http.HandlerFunc(h.authServerMetadata)))
	p.Mux.Handle("GET /.well-known/oauth-protected-resource/mcp/{id}", cors(http.HandlerFunc(h.resourceMetadata)))
	p.Mux.Handle("GET /mcp/{id}/.well-known/oauth-protected-resource", cors(http.HandlerFunc(h.resourceMetadata)))
	p.Mux.Handle("/mcp/{id}/oauth2/register", cors(http.HandlerFunc(h.register)))
	p.Mux.Handle("/mcp/{id}/oauth2/token", cors(http.HandlerFunc(h.token)))
	// Not CORS: a page a person opens, never called from another origin.
	p.Mux.Handle("/mcp/{id}/oauth2/authorize", http.HandlerFunc(h.authorize))
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID")
		w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id, Mcp-Protocol-Version, WWW-Authenticate")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// identity is a database's issuer and resource identifiers, derived in one
// place so the documents can never disagree.
type identity struct {
	issuer   string
	resource string
	prmURL   string
}

func (h *handler) identityFor(id string) identity {
	path := "/mcp/" + id
	return identity{
		issuer:   h.baseURL + path,
		resource: h.baseURL + path,
		prmURL:   h.baseURL + "/.well-known/oauth-protected-resource" + path,
	}
}

// database resolves the {id} path value to an existing database.
func (h *handler) database(r *http.Request) (idStr string, id, ownerID int64, name string, err error) {
	idStr = r.PathValue("id")
	id = crud.ParseID(idStr)
	if id == 0 {
		return "", 0, 0, "", entity.ErrNotFound
	}
	ownerID, name, err = h.owners.SystemDatabaseOwner(r.Context(), id)
	return idStr, id, ownerID, name, err
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

func (h *handler) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	idStr, _, _, _, err := h.database(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	id := h.identityFor(idStr)
	writeJSON(w, http.StatusOK, map[string]any{
		"resource": id.resource,
		// Issuer identifiers, not metadata URLs (RFC 9728 §2).
		"authorization_servers":    []string{id.issuer},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{oauthScope},
		"resource_name":            "SQL MCP",
	})
}

func (h *handler) authServerMetadata(w http.ResponseWriter, r *http.Request) {
	idStr, _, _, _, err := h.database(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	id := h.identityFor(idStr)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                   id.issuer,
		"authorization_endpoint":   id.issuer + "/oauth2/authorize",
		"token_endpoint":           id.issuer + "/oauth2/token",
		"registration_endpoint":    id.issuer + "/oauth2/register",
		"response_types_supported": []string{"code"},
		"scopes_supported":         []string{oauthScope},
		// No client_credentials: every token stands for a person's grant.
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported":      []string{"S256"},
	})
}

// register is RFC 7591 dynamic client registration. The client_id it
// returns is a signed token carrying the client's redirect_uris.
func (h *handler) register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, _, _, _, err := h.database(r); err != nil {
		http.NotFound(w, r)
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&payload); err != nil || payload == nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "request body must be a JSON object")
		return
	}
	var uris []string
	if raw, ok := payload["redirect_uris"]; ok {
		items, ok := raw.([]any)
		if !ok {
			oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris must be an array of URIs")
			return
		}
		for _, item := range items {
			s, ok := item.(string)
			if !ok || s == "" {
				oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris must be an array of URIs")
				return
			}
			if _, err := oauthconsent.CheckRedirectURI(s, []string{s}, h.baseURL); err != nil {
				oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
				return
			}
			uris = append(uris, s)
		}
	}
	name, err := auth.ValidateDisplayName(payload["client_name"])
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "client_name: "+err.Error())
		return
	}
	clientID, err := h.tokens.CreateClientID(uris, name)
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "could not register the client")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  clientID,
		"client_id_issued_at":        time.Now().Unix(),
		"client_name":                name,
		"redirect_uris":              uris,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

// authorize shows the consent page to the database's signed-in owner.
func (h *handler) authorize(w http.ResponseWriter, r *http.Request) {
	idStr, _, ownerID, dbName, err := h.database(r)
	if err != nil {
		http.Error(w, "database not found", http.StatusNotFound)
		return
	}
	q := r.URL.Query()
	clientID := q.Get("client_id")
	client, err := h.tokens.ValidateClientID(clientID)
	if err != nil {
		http.Error(w, "invalid_client: register at the registration_endpoint first", http.StatusBadRequest)
		return
	}
	redirectURI, err := oauthconsent.CheckRedirectURI(q.Get("redirect_uri"), client.RedirectURIs, h.baseURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// From here on, errors go back to the client (RFC 6749 §4.1.2.1).
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

	var userID, email string
	if cookie, err := r.Cookie("at"); err == nil {
		if claims, err := h.tokens.ValidateAccessToken(cookie.Value); err == nil {
			userID, email = claims.Subject, claims.Email
		}
	}
	if userID == "" {
		http.Redirect(w, r, oauthconsent.LoginURL(h.baseURL, h.baseURL+r.URL.RequestURI()), http.StatusFound)
		return
	}
	if crud.ParseID(userID) != ownerID {
		http.Error(w, "This database belongs to another account. Sign in as its owner to connect it.", http.StatusForbidden)
		return
	}

	oauthconsent.Serve(w, r, h.tokens, oauthconsent.Request{
		UserID:       userID,
		Email:        email,
		ClientName:   client.ClientName,
		Registered:   true,
		DatabaseName: dbName,
		Consent: auth.Consent{
			DatabaseID:    idStr,
			ClientID:      clientID,
			RedirectURI:   redirectURI,
			State:         q.Get("state"),
			CodeChallenge: q.Get("code_challenge"),
		},
		RegisteredRedirectURIs: client.RedirectURIs,
		BaseURL:                h.baseURL,
	})
}

// token redeems a code or a refresh token. A grant outlives nothing: its
// database must still exist and still belong to the grant's subject.
func (h *handler) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr, _, ownerID, _, err := h.database(r)
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "database not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "invalid form")
		return
	}
	var userID string
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		userID, err = h.tokens.RedeemCode(r.PostForm.Get("code"), idStr, r.PostForm.Get("client_id"), r.PostForm.Get("redirect_uri"), r.PostForm.Get("code_verifier"))
	case "refresh_token":
		userID, err = h.tokens.ValidateMCPRefreshToken(r.PostForm.Get("refresh_token"), idStr)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
		return
	}
	if err != nil || crud.ParseID(userID) != ownerID {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the grant is invalid, expired, or for another client")
		return
	}
	access, refresh, err := h.tokens.CreateMCPTokens(userID, idStr)
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "could not issue tokens")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"refresh_token": refresh,
		"token_type":    "Bearer",
		"scope":         oauthScope,
		"expires_in":    int64(auth.MCPAccessTokenTTL.Seconds()),
	})
}

// challenge is the RFC 9728 §5.1 answer that tells a client where to find
// the metadata, instead of making it guess.
func (h *handler) challenge(w http.ResponseWriter, idStr, message string) {
	id := h.identityFor(idStr)
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm=%q, resource_metadata=%q, scope=%q`, id.resource, id.prmURL, oauthScope))
	writeJSON(w, http.StatusUnauthorized, map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": -32001, "message": message}})
}

func (h *handler) serveMCP(w http.ResponseWriter, r *http.Request) {
	idStr, id, ownerID, name, err := h.database(r)
	if errors.Is(err, entity.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Error().Err(err).Msg("could not look up a database for MCP")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		h.challenge(w, idStr, "authorization required")
		return
	}
	userID, err := h.tokens.ValidateMCPAccessToken(token, idStr)
	if err != nil || crud.ParseID(userID) != ownerID {
		h.challenge(w, idStr, "invalid or expired token")
		return
	}
	ctx := mcpserver.WithCaller(r.Context(), mcpserver.Caller{UserID: ownerID, DatabaseID: id, DatabaseName: name})
	h.mcp.ServeHTTP(w, r.WithContext(ctx))
}
