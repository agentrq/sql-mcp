package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminToken(t *testing.T) {
	s, now := testTokens(t)
	token, expiresAt, err := s.CreateAdminToken()
	require.NoError(t, err)
	assert.Equal(t, now.Add(AdminTokenTTL), expiresAt)

	c, err := s.ValidateAdminToken(token)
	require.NoError(t, err)
	assert.Equal(t, "admin", c.Subject)

	_, err = s.ValidateAccessToken(token)
	assert.Error(t, err, "an admin token is nobody's session")
	access, _ := s.CreateAccessToken("u1", "a@x.com", "", "")
	_, err = s.ValidateAdminToken(access)
	assert.Error(t, err, "a session is not an admin token")

	*now = now.Add(AdminTokenTTL + time.Second)
	_, err = s.ValidateAdminToken(token)
	assert.ErrorIs(t, err, jwt.ErrTokenExpired)
}

func TestAllowlistEntriesAndMatch(t *testing.T) {
	a, err := NewAllowlist([]string{" a@x.com ", "", "*@agentrq.com"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a@x.com", "*@agentrq.com"}, a.Entries())
	assert.Equal(t, "*@agentrq.com", a.Match("Dev@AgentRQ.com"))
	assert.Equal(t, "", a.Match("dev @agentrq.com"))
	assert.Equal(t, "", a.Match(""))
	assert.Equal(t, "", a.Match("b@x.com"))
}
