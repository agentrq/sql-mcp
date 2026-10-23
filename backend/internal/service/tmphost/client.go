// Package tmphost talks to tmphost.io, where query results too large to hand
// an agent inline are stored for a while.
//
// This server is an OAuth client of tmphost: it registers itself once by RFC
// 7591 dynamic client registration, sends a person through tmphost's consent
// page with PKCE, and keeps the tokens it gets back. See
// https://tmphost.io/docs/api.md for the files API.
package tmphost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Metadata is the RFC 8414 document the server publishes.
type Metadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint"`
}

// Token is what the token endpoint answers.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// Folder is a tmphost folder; it and its files expire together.
type Folder struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// File is one file written to a folder.
type File struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

// Error is an answer tmphost refused with.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("tmphost: %d %s", e.Status, e.Message)
}

// IsUnauthorized reports whether tmphost no longer accepts the token.
func IsUnauthorized(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusUnauthorized
}

// Client is one tmphost server.
type Client struct {
	baseURL string
	http    *http.Client

	mu       sync.Mutex
	metadata *Metadata
}

// New is a client of the tmphost server at baseURL (https://app.tmphost.io).
func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: httpClient}
}

// BaseURL is the server this client talks to.
func (c *Client) BaseURL() string { return c.baseURL }

// Metadata fetches, once, the server's OAuth metadata. Every endpoint must
// be on the server's own origin: a document pointing elsewhere would send a
// person's code, or this server's tokens, to someone else.
func (c *Client) Metadata(ctx context.Context) (*Metadata, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.metadata != nil {
		return c.metadata, nil
	}
	var m Metadata
	if err := c.do(ctx, http.MethodGet, c.baseURL+"/.well-known/oauth-authorization-server", "", nil, "", &m); err != nil {
		return nil, err
	}
	for _, endpoint := range []string{m.AuthorizationEndpoint, m.TokenEndpoint, m.RegistrationEndpoint} {
		if !strings.HasPrefix(endpoint, c.baseURL+"/") {
			return nil, fmt.Errorf("tmphost: metadata endpoint %q is not on %s", endpoint, c.baseURL)
		}
	}
	c.metadata = &m
	return &m, nil
}

// Register registers this server as a public client, returning its client_id.
func (c *Client) Register(ctx context.Context, redirectURI, clientName string) (string, error) {
	m, err := c.Metadata(ctx)
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(map[string]any{
		"client_name":                clientName,
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
	var out struct {
		ClientID string `json:"client_id"`
	}
	if err := c.do(ctx, http.MethodPost, m.RegistrationEndpoint, "", bytes.NewReader(body), "application/json", &out); err != nil {
		return "", err
	}
	if out.ClientID == "" {
		return "", errors.New("tmphost: registration returned no client_id")
	}
	return out.ClientID, nil
}

// AuthURL is where to send the person to approve this server.
func (c *Client) AuthURL(ctx context.Context, clientID, redirectURI, state, challenge string) (string, error) {
	m, err := c.Metadata(ctx)
	if err != nil {
		return "", err
	}
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return m.AuthorizationEndpoint + "?" + q.Encode(), nil
}

// CheckIssuer verifies the RFC 9207 iss parameter of an authorization
// response, when the server sent one.
func (c *Client) CheckIssuer(ctx context.Context, iss string) error {
	if iss == "" {
		return nil
	}
	m, err := c.Metadata(ctx)
	if err != nil {
		return err
	}
	if iss != m.Issuer {
		return errors.New("tmphost: authorization response came from another issuer")
	}
	return nil
}

// Exchange trades an authorization code for tokens.
func (c *Client) Exchange(ctx context.Context, clientID, redirectURI, code, verifier string) (*Token, error) {
	return c.token(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	})
}

// Refresh trades a refresh token for new tokens.
func (c *Client) Refresh(ctx context.Context, clientID, refreshToken string) (*Token, error) {
	return c.token(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
	})
}

func (c *Client) token(ctx context.Context, form url.Values) (*Token, error) {
	m, err := c.Metadata(ctx)
	if err != nil {
		return nil, err
	}
	var t Token
	if err := c.do(ctx, http.MethodPost, m.TokenEndpoint, "", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", &t); err != nil {
		return nil, err
	}
	if t.AccessToken == "" {
		return nil, errors.New("tmphost: token response has no access_token")
	}
	return &t, nil
}

// Account names the account a token belongs to: its email, else its name.
func (c *Client) Account(ctx context.Context, accessToken string) (string, error) {
	var me map[string]any
	if err := c.do(ctx, http.MethodGet, c.baseURL+"/api/v1/users/me", accessToken, nil, "", &me); err != nil {
		return "", err
	}
	if u, ok := me["user"].(map[string]any); ok {
		me = u
	}
	for _, key := range []string{"email", "name", "id"} {
		if s, ok := me[key].(string); ok && s != "" {
			return s, nil
		}
	}
	return "", nil
}

// CreateFolder makes a folder that expires after ttlMinutes (the account's
// default, 30 minutes, when zero).
func (c *Client) CreateFolder(ctx context.Context, accessToken, title string, ttlMinutes int) (*Folder, error) {
	payload := map[string]any{}
	if ttlMinutes > 0 {
		payload["ttlMinutes"] = ttlMinutes
	}
	if title != "" {
		payload["title"] = title
	}
	body, _ := json.Marshal(payload)
	var out struct {
		Folder Folder `json:"folder"`
	}
	if err := c.do(ctx, http.MethodPost, c.baseURL+"/api/v1/folders", accessToken, bytes.NewReader(body), "application/json", &out); err != nil {
		return nil, err
	}
	return &out.Folder, nil
}

// PutFile writes content at path in a folder.
func (c *Client) PutFile(ctx context.Context, accessToken, folderID, path string, content []byte, contentType string) (*File, error) {
	var out struct {
		File File `json:"file"`
	}
	endpoint := c.baseURL + "/api/v1/folders/" + url.PathEscape(folderID) + "/files/" + url.PathEscape(path)
	if err := c.do(ctx, http.MethodPut, endpoint, accessToken, bytes.NewReader(content), contentType, &out); err != nil {
		return nil, err
	}
	return &out.File, nil
}

func (c *Client) do(ctx context.Context, method, endpoint, accessToken string, body io.Reader, contentType string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("tmphost: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("tmphost: %w", err)
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error            any    `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		msg := strings.TrimSpace(string(raw))
		if json.Unmarshal(raw, &e) == nil && e.Error != nil {
			msg = fmt.Sprint(e.Error)
			if e.ErrorDescription != "" {
				msg += ": " + e.ErrorDescription
			}
		}
		return &Error{Status: resp.StatusCode, Message: msg}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("tmphost: decode %s: %w", endpoint, err)
	}
	return nil
}
