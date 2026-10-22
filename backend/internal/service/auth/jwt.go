// Package auth signs and checks every token the server issues, and holds the
// sign-in providers and the email allowlist.
//
// Every token is an HS256 JWT signed with the one secret, so the audience is
// what tells them apart: each Validate method checks its own, and a token
// minted for one purpose never satisfies another's check.
package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// AccessAudience marks the browser's `at` cookie.
	AccessAudience = "actor:human"
	// RefreshAudience marks the browser's `rt` cookie. Distinct from the MCP
	// refresh token: one stands for a signed-in person, the other for an app
	// one database granted.
	RefreshAudience = "refresh:human"
	// StateAudiencePrefix + provider marks a sign-in's OAuth state.
	StateAudiencePrefix = "oauth_state:"
	// ClientAudience marks a client_id minted by dynamic client registration.
	ClientAudience = "dynamic_client"
	// ConsentAudience marks the token the consent page posts back with Allow.
	ConsentAudience = "oauth_consent"
	// CodeAudience marks an OAuth authorization code.
	CodeAudience = "authorization_code"
	// MCPAccessAudience and MCPRefreshAudience mark the tokens an app holds
	// for one database's MCP server; the database ID is the other audience.
	MCPAccessAudience  = "mcp_access"
	MCPRefreshAudience = "mcp_refresh"
	// AdminAudience marks the admin token minted for the root token.
	AdminAudience = "actor:admin"
)

const (
	AccessTokenTTL    = 24 * time.Hour
	RefreshTokenTTL   = 2 * 24 * time.Hour
	StateTTL          = 5 * time.Minute
	ConsentTTL        = 10 * time.Minute
	CodeTTL           = 2 * time.Minute
	AdminTokenTTL     = time.Hour
	MCPAccessTokenTTL = 24 * time.Hour
	// MCPRefreshTokenTTL is long because an agent runs unattended; the
	// database's owner revokes it by deleting the database or rotating
	// JWT_SECRET.
	MCPRefreshTokenTTL = 30 * 24 * time.Hour
)

// Claims is a person's session, or an MCP grant.
type Claims struct {
	jwt.RegisteredClaims
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`
	Picture string `json:"picture,omitempty"`
}

type stateClaims struct {
	jwt.RegisteredClaims
	RedirectURL string `json:"rurl,omitempty"`
}

// ClientClaims is what a dynamic client registered. The client_id IS this
// token, so registrations need no table.
type ClientClaims struct {
	jwt.RegisteredClaims
	RedirectURIs []string `json:"redirect_uris,omitempty"`
	ClientName   string   `json:"client_name,omitempty"`
}

// Consent is the authorization request a consent or code token is bound to.
// Every field must match when it is spent, so a grant for one app, redirect
// or database cannot be spent on another.
type Consent struct {
	DatabaseID    string `json:"db"`
	ClientID      string `json:"cid"`
	RedirectURI   string `json:"ruri"`
	State         string `json:"st,omitempty"`
	CodeChallenge string `json:"cc,omitempty"`
}

type consentClaims struct {
	jwt.RegisteredClaims
	Consent
}

// TokenService signs and checks tokens.
type TokenService struct {
	secret []byte
	now    func() time.Time
}

// NewTokenService refuses an empty secret.
func NewTokenService(secret string) (*TokenService, error) {
	if secret == "" {
		return nil, errors.New("auth: JWT secret is required")
	}
	return &TokenService{secret: []byte(secret), now: time.Now}, nil
}

func (s *TokenService) sign(claims jwt.Claims) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

func (s *TokenService) registered(subject string, ttl time.Duration, audience ...string) jwt.RegisteredClaims {
	now := s.now()
	return jwt.RegisteredClaims{
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		Audience:  audience,
	}
}

// parse checks the signature, the expiry and every audience in want.
func (s *TokenService) parse(token string, claims jwt.Claims, want ...string) error {
	opts := []jwt.ParserOption{jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithTimeFunc(s.now)}
	parsed, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return s.secret, nil }, opts...)
	if err != nil {
		return err
	}
	if !parsed.Valid {
		return errors.New("auth: invalid token")
	}
	aud, err := claims.GetAudience()
	if err != nil {
		return err
	}
	for _, w := range want {
		if !hasAudience(aud, w) {
			return fmt.Errorf("auth: token is not for %s", w)
		}
	}
	return nil
}

func hasAudience(aud jwt.ClaimStrings, want string) bool {
	for _, a := range aud {
		if a == want {
			return true
		}
	}
	return false
}

// CreateAccessToken mints the browser's 24-hour `at` cookie.
func (s *TokenService) CreateAccessToken(userID, email, name, picture string) (string, error) {
	return s.sign(Claims{RegisteredClaims: s.registered(userID, AccessTokenTTL, AccessAudience), Email: email, Name: name, Picture: picture})
}

// ValidateAccessToken accepts only the `at` cookie.
func (s *TokenService) ValidateAccessToken(token string) (*Claims, error) {
	c := &Claims{}
	if err := s.parse(token, c, AccessAudience); err != nil {
		return nil, err
	}
	return c, nil
}

// CreateAdminToken mints the admin token: short-lived, for nobody's
// account, and accepted only by the admin API.
func (s *TokenService) CreateAdminToken() (string, time.Time, error) {
	claims := s.registered("admin", AdminTokenTTL, AdminAudience)
	token, err := s.sign(claims)
	return token, claims.ExpiresAt.Time, err
}

// ValidateAdminToken accepts only an admin token.
func (s *TokenService) ValidateAdminToken(token string) (*Claims, error) {
	c := &Claims{}
	if err := s.parse(token, c, AdminAudience); err != nil {
		return nil, err
	}
	return c, nil
}

// CreateRefreshToken mints the browser's `rt` cookie. It carries no profile:
// a refresh mints the access token from the database at that moment.
func (s *TokenService) CreateRefreshToken(userID string) (string, error) {
	return s.sign(Claims{RegisteredClaims: s.registered(userID, RefreshTokenTTL, RefreshAudience)})
}

// ValidateRefreshToken accepts only the `rt` cookie; without the audience
// check a stolen access token could be walked forward forever.
func (s *TokenService) ValidateRefreshToken(token string) (*Claims, error) {
	c := &Claims{}
	if err := s.parse(token, c, RefreshAudience); err != nil {
		return nil, err
	}
	return c, nil
}

// CreateStateToken is the OAuth state of a sign-in with provider, carrying
// where to go afterwards.
func (s *TokenService) CreateStateToken(provider, redirectURL string) (string, error) {
	return s.sign(stateClaims{RegisteredClaims: s.registered("state", StateTTL, StateAudiencePrefix+provider), RedirectURL: redirectURL})
}

// ValidateStateToken returns the redirect a state token carries.
func (s *TokenService) ValidateStateToken(provider, token string) (string, error) {
	c := &stateClaims{}
	if err := s.parse(token, c, StateAudiencePrefix+provider); err != nil {
		return "", err
	}
	return c.RedirectURL, nil
}

// CreateClientID mints an RFC 7591 client_id carrying the client's
// registered redirect_uris, so authorize can hold it to them statelessly.
func (s *TokenService) CreateClientID(redirectURIs []string, clientName string) (string, error) {
	return s.sign(ClientClaims{
		RegisteredClaims: s.registered("dynamic_client", 10*365*24*time.Hour, ClientAudience),
		RedirectURIs:     redirectURIs,
		ClientName:       clientName,
	})
}

// ValidateClientID parses a client_id this server minted.
func (s *TokenService) ValidateClientID(token string) (*ClientClaims, error) {
	c := &ClientClaims{}
	if err := s.parse(token, c, ClientAudience); err != nil {
		return nil, err
	}
	return c, nil
}

// CreateConsentToken is the token the consent page posts back with Allow,
// bound to the person it was shown to and the request it describes.
func (s *TokenService) CreateConsentToken(userID string, consent Consent) (string, error) {
	return s.sign(consentClaims{RegisteredClaims: s.registered(userID, ConsentTTL, ConsentAudience), Consent: consent})
}

// ValidateConsentToken accepts only a consent token for this person and
// exactly this request.
func (s *TokenService) ValidateConsentToken(token, userID string, consent Consent) error {
	c := &consentClaims{}
	if err := s.parse(token, c, ConsentAudience); err != nil {
		return err
	}
	if c.Subject != userID || c.Consent != consent {
		return errors.New("auth: consent was given for a different request")
	}
	return nil
}

// CreateCode mints a two-minute authorization code bound to the request.
func (s *TokenService) CreateCode(userID string, consent Consent) (string, error) {
	return s.sign(consentClaims{RegisteredClaims: s.registered(userID, CodeTTL, consent.DatabaseID, CodeAudience), Consent: consent})
}

// RedeemCode checks a code against the token request: the same database,
// client and redirect_uri it was issued for, and the PKCE verifier whose
// challenge the authorization request carried (RFC 7636, S256 only).
func (s *TokenService) RedeemCode(code, databaseID, clientID, redirectURI, verifier string) (string, error) {
	c := &consentClaims{}
	if err := s.parse(code, c, databaseID, CodeAudience); err != nil {
		return "", err
	}
	if c.DatabaseID != databaseID || c.ClientID != clientID {
		return "", errors.New("auth: code was issued to another client")
	}
	if redirectURI != "" && redirectURI != c.RedirectURI {
		return "", errors.New("auth: redirect_uri does not match the authorization request")
	}
	if c.CodeChallenge == "" || S256(verifier) != c.CodeChallenge {
		return "", errors.New("auth: code_verifier does not match the code_challenge")
	}
	return c.Subject, nil
}

// S256 is the PKCE S256 challenge of verifier.
func S256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// CreateMCPTokens mints the access and refresh token an app holds for one
// database's MCP server.
func (s *TokenService) CreateMCPTokens(userID, databaseID string) (access, refresh string, err error) {
	access, err = s.sign(Claims{RegisteredClaims: s.registered(userID, MCPAccessTokenTTL, databaseID, MCPAccessAudience)})
	if err != nil {
		return "", "", err
	}
	refresh, err = s.sign(Claims{RegisteredClaims: s.registered(userID, MCPRefreshTokenTTL, databaseID, MCPRefreshAudience)})
	return access, refresh, err
}

// ValidateMCPAccessToken returns the user an access token for databaseID
// was granted by.
func (s *TokenService) ValidateMCPAccessToken(token, databaseID string) (string, error) {
	c := &Claims{}
	if err := s.parse(token, c, databaseID, MCPAccessAudience); err != nil {
		return "", err
	}
	return c.Subject, nil
}

// ValidateMCPRefreshToken returns the user a refresh token for databaseID
// was granted by.
func (s *TokenService) ValidateMCPRefreshToken(token, databaseID string) (string, error) {
	c := &Claims{}
	if err := s.parse(token, c, databaseID, MCPRefreshAudience); err != nil {
		return "", err
	}
	return c.Subject, nil
}
