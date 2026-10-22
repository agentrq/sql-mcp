package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDisplayName(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  any
		want string
		err  bool
	}{
		{"nil", nil, "", false},
		{"empty", "", "", false},
		{"plain", "Claude Code", "Claude Code", false},
		{"collapses whitespace", "  Claude    Code ", "Claude Code", false},
		{"unicode", "Café ☕", "Café ☕", false},
		{"at the limit", strings.Repeat("é", DisplayNameMaxRunes), strings.Repeat("é", DisplayNameMaxRunes), false},
		{"over the limit", strings.Repeat("a", DisplayNameMaxRunes+1), "", true},
		{"not a string", 42, "", true},
		{"invalid utf-8", "a\xffb", "", true},
		{"newline", "a\nb", "", true},
		{"tab", "a\tb", "", true},
		{"nul", "a\x00b", "", true},
		{"right-to-left override", "moc.elgoog\u202e", "", true},
		{"zero width space", "goo\u200bgle", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateDisplayName(tc.raw)
			if tc.err {
				require.ErrorIs(t, err, ErrInvalidDisplayName)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRedirectURIMatches(t *testing.T) {
	for _, tc := range []struct {
		registered, requested string
		want                  bool
	}{
		{"https://app.com/cb", "https://app.com/cb", true},
		{"https://app.com/cb", "https://app.com:8443/cb", false},
		{"https://app.com/cb", "https://app.com/cb2", false},
		{"cursor://cb", "cursor://cb", true},
		{"http://localhost/callback", "http://localhost:3118/callback", true},
		{"http://localhost:1/callback", "http://LOCALHOST:2/callback", true},
		{"http://127.0.0.1/cb", "http://127.0.0.1:5000/cb", true},
		{"http://127.5.5.5/cb", "http://127.5.5.5:5000/cb", true},
		{"http://[::1]/cb", "http://[::1]:5000/cb", true},
		{"http://localhost/cb", "http://127.0.0.1:5000/cb", false},
		{"http://localhost/cb", "https://localhost:5000/cb", false},
		{"http://localhost/cb", "http://localhost:5000/other", false},
		{"http://localhost/cb?a=1", "http://localhost:5000/cb?a=2", false},
		{"http://localhost/cb?a=1", "http://localhost:5000/cb?a=1", true},
		{"http://u@localhost/cb", "http://v@localhost:5000/cb", false},
		{"http://localhost/cb", "http://evil.com:5000/cb", false},
		{"http://evil.com/cb", "http://localhost:5000/cb", false},
		{"http://localhost/cb", "http://[::1/cb", false},
		{"http://[::1/cb", "http://localhost/cb", false},
	} {
		assert.Equal(t, tc.want, RedirectURIMatches(tc.registered, tc.requested), "%s vs %s", tc.registered, tc.requested)
	}
}

func TestAnyRedirectURIMatches(t *testing.T) {
	registered := []string{"https://app.com/cb", "http://localhost/cb"}
	assert.True(t, AnyRedirectURIMatches(registered, "http://localhost:9/cb"))
	assert.True(t, AnyRedirectURIMatches(registered, "https://app.com/cb"))
	assert.False(t, AnyRedirectURIMatches(registered, "https://evil.com/cb"))
	assert.False(t, AnyRedirectURIMatches(nil, "https://app.com/cb"))
}

func TestIsAllowedNativeRedirectScheme(t *testing.T) {
	for _, s := range []string{"cursor", "VSCode", "vscode-insiders", "claude", "claude-code", "zed"} {
		assert.True(t, IsAllowedNativeRedirectScheme(s), s)
	}
	for _, s := range []string{"", "http", "https", "javascript", "evil"} {
		assert.False(t, IsAllowedNativeRedirectScheme(s), s)
	}
}
