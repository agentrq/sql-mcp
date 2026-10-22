package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "0123456789abcdef0123456789abcdef"

// testTokens is a token service whose clock the test moves.
func testTokens(t *testing.T) (*TokenService, *time.Time) {
	t.Helper()
	s, err := NewTokenService(testSecret)
	require.NoError(t, err)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s.now = func() time.Time { return now }
	return s, &now
}

func TestNewTokenServiceNeedsSecret(t *testing.T) {
	_, err := NewTokenService("")
	assert.Error(t, err)
}

func TestAccessToken(t *testing.T) {
	s, now := testTokens(t)
	token, err := s.CreateAccessToken("u1", "a@x.com", "Ann", "https://pic")
	require.NoError(t, err)

	c, err := s.ValidateAccessToken(token)
	require.NoError(t, err)
	assert.Equal(t, "u1", c.Subject)
	assert.Equal(t, "a@x.com", c.Email)
	assert.Equal(t, "Ann", c.Name)
	assert.Equal(t, "https://pic", c.Picture)

	*now = now.Add(AccessTokenTTL + time.Second)
	_, err = s.ValidateAccessToken(token)
	assert.ErrorIs(t, err, jwt.ErrTokenExpired)
}

func TestRefreshToken(t *testing.T) {
	s, _ := testTokens(t)
	token, err := s.CreateRefreshToken("u1")
	require.NoError(t, err)
	c, err := s.ValidateRefreshToken(token)
	require.NoError(t, err)
	assert.Equal(t, "u1", c.Subject)
	assert.Empty(t, c.Email)
}

func TestStateToken(t *testing.T) {
	s, _ := testTokens(t)
	token, err := s.CreateStateToken("google", "/databases")
	require.NoError(t, err)

	got, err := s.ValidateStateToken("google", token)
	require.NoError(t, err)
	assert.Equal(t, "/databases", got)

	_, err = s.ValidateStateToken("github", token)
	assert.ErrorContains(t, err, "not for oauth_state:github")
}

func TestClientID(t *testing.T) {
	s, _ := testTokens(t)
	token, err := s.CreateClientID([]string{"http://localhost/cb"}, "Claude")
	require.NoError(t, err)
	c, err := s.ValidateClientID(token)
	require.NoError(t, err)
	assert.Equal(t, []string{"http://localhost/cb"}, c.RedirectURIs)
	assert.Equal(t, "Claude", c.ClientName)

	_, err = s.ValidateClientID("not-a-jwt")
	assert.Error(t, err)
}

func testConsent() Consent {
	return Consent{DatabaseID: "db1", ClientID: "c1", RedirectURI: "http://localhost/cb", State: "st", CodeChallenge: S256("verifier")}
}

func TestConsentToken(t *testing.T) {
	s, _ := testTokens(t)
	consent := testConsent()
	token, err := s.CreateConsentToken("u1", consent)
	require.NoError(t, err)

	require.NoError(t, s.ValidateConsentToken(token, "u1", consent))

	other := consent
	other.RedirectURI = "http://localhost/other"
	for _, tc := range []struct {
		name    string
		token   string
		user    string
		consent Consent
	}{
		{"other person", token, "u2", consent},
		{"other request", token, "u1", other},
		{"garbage", "x.y.z", "u1", consent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Error(t, s.ValidateConsentToken(tc.token, tc.user, tc.consent))
		})
	}
}

func TestRedeemCode(t *testing.T) {
	s, now := testTokens(t)
	consent := testConsent()
	code, err := s.CreateCode("u1", consent)
	require.NoError(t, err)

	user, err := s.RedeemCode(code, "db1", "c1", "http://localhost/cb", "verifier")
	require.NoError(t, err)
	assert.Equal(t, "u1", user)

	// redirect_uri may be omitted at the token endpoint.
	_, err = s.RedeemCode(code, "db1", "c1", "", "verifier")
	require.NoError(t, err)

	noChallenge := consent
	noChallenge.CodeChallenge = ""
	codeNoChallenge, err := s.CreateCode("u1", noChallenge)
	require.NoError(t, err)
	otherClient := consent
	otherClient.ClientID = "c2"
	codeOtherClient, err := s.CreateCode("u1", otherClient)
	require.NoError(t, err)

	for _, tc := range []struct {
		name, code, db, client, redirect, verifier, want string
	}{
		{"other database", code, "db2", "c1", "", "verifier", "not for db2"},
		{"other client", code, "db1", "c2", "", "verifier", "another client"},
		{"bound to other client", codeOtherClient, "db1", "c1", "", "verifier", "another client"},
		{"other redirect", code, "db1", "c1", "http://localhost/x", "verifier", "redirect_uri"},
		{"wrong verifier", code, "db1", "c1", "", "nope", "code_verifier"},
		{"no challenge", codeNoChallenge, "db1", "c1", "", "", "code_verifier"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.RedeemCode(tc.code, tc.db, tc.client, tc.redirect, tc.verifier)
			assert.ErrorContains(t, err, tc.want)
		})
	}

	*now = now.Add(CodeTTL + time.Second)
	_, err = s.RedeemCode(code, "db1", "c1", "", "verifier")
	assert.ErrorIs(t, err, jwt.ErrTokenExpired)
}

func TestS256(t *testing.T) {
	// base64url(sha256(verifier)), unpadded.
	assert.Equal(t, "VMumsdO_IDX64oq6ER7tV0Jg6YG2r53AI8GoIecIjtA", S256("dBjftJeZ4CVP-mB0unwt7nFIzDVfbVnRfGsrmFb_fcY"))
}

func TestMCPTokens(t *testing.T) {
	s, _ := testTokens(t)
	access, refresh, err := s.CreateMCPTokens("u1", "db1")
	require.NoError(t, err)

	user, err := s.ValidateMCPAccessToken(access, "db1")
	require.NoError(t, err)
	assert.Equal(t, "u1", user)
	user, err = s.ValidateMCPRefreshToken(refresh, "db1")
	require.NoError(t, err)
	assert.Equal(t, "u1", user)

	_, err = s.ValidateMCPAccessToken(access, "db2")
	assert.Error(t, err)
	_, err = s.ValidateMCPRefreshToken(refresh, "db2")
	assert.Error(t, err)
	_, err = s.ValidateMCPAccessToken(refresh, "db1")
	assert.Error(t, err)
	_, err = s.ValidateMCPRefreshToken(access, "db1")
	assert.Error(t, err)
}

// A token minted for one purpose never passes another's check.
func TestAudiencesDoNotCross(t *testing.T) {
	s, _ := testTokens(t)
	access, _ := s.CreateAccessToken("u1", "", "", "")
	refresh, _ := s.CreateRefreshToken("u1")
	state, _ := s.CreateStateToken("google", "")
	client, _ := s.CreateClientID(nil, "")
	consent, _ := s.CreateConsentToken("u1", testConsent())
	code, _ := s.CreateCode("u1", testConsent())
	mcpAccess, mcpRefresh, _ := s.CreateMCPTokens("u1", "db1")

	validators := map[string]func(string) error{
		"access":  func(t string) error { _, err := s.ValidateAccessToken(t); return err },
		"refresh": func(t string) error { _, err := s.ValidateRefreshToken(t); return err },
		"state":   func(t string) error { _, err := s.ValidateStateToken("google", t); return err },
		"client":  func(t string) error { _, err := s.ValidateClientID(t); return err },
		"consent": func(t string) error { return s.ValidateConsentToken(t, "u1", testConsent()) },
		"code": func(t string) error {
			_, err := s.RedeemCode(t, "db1", "c1", "", "verifier")
			return err
		},
		"mcpAccess":  func(t string) error { _, err := s.ValidateMCPAccessToken(t, "db1"); return err },
		"mcpRefresh": func(t string) error { _, err := s.ValidateMCPRefreshToken(t, "db1"); return err },
	}
	tokens := map[string]string{
		"access": access, "refresh": refresh, "state": state, "client": client,
		"consent": consent, "code": code, "mcpAccess": mcpAccess, "mcpRefresh": mcpRefresh,
	}
	for kind, token := range tokens {
		for name, validate := range validators {
			err := validate(token)
			if kind == name {
				assert.NoError(t, err, "%s token as %s", kind, name)
			} else {
				assert.Error(t, err, "%s token as %s", kind, name)
			}
		}
	}
}

func TestParseRefusesForgedTokens(t *testing.T) {
	s, _ := testTokens(t)
	claims := Claims{RegisteredClaims: s.registered("u1", time.Hour, AccessAudience)}

	otherKey, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("another secret"))
	require.NoError(t, err)
	otherAlg, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte(testSecret))
	require.NoError(t, err)
	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	for name, token := range map[string]string{"other key": otherKey, "HS512": otherAlg, "none": none, "empty": ""} {
		t.Run(name, func(t *testing.T) {
			_, err := s.ValidateAccessToken(token)
			assert.Error(t, err)
		})
	}
}

func TestParseRefusesBadAudienceClaim(t *testing.T) {
	s, _ := testTokens(t)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "u1", "aud": 42, "exp": s.now().Add(time.Hour).Unix(),
	}).SignedString([]byte(testSecret))
	require.NoError(t, err)
	_, err = s.ValidateAccessToken(token)
	assert.Error(t, err)
}
