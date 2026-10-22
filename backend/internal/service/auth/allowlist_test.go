package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllowlist(t *testing.T) {
	a, err := NewAllowlist([]string{"ann@x.com", " ", "*@agentrq.com", "re:[a-c]+@re\\.io", "a.b*@dots.com"})
	require.NoError(t, err)
	assert.False(t, a.Empty())

	for _, tc := range []struct {
		email string
		want  bool
	}{
		{"ann@x.com", true},
		{"ANN@X.COM", true},
		{" ann@x.com ", true},
		{"bob@x.com", false},
		{"xann@x.com", false},
		{"anyone@agentrq.com", true},
		{"@agentrq.com", true},
		{"x@agentrq.com.evil.com", false},
		{"x@sub.agentrq.com", false},
		{"abc@re.io", true},
		{"abcd@re.io", false},
		{"abc@re.io.evil", false},
		{"a.b.c@dots.com", true},
		{"axb@dots.com", false}, // a literal dot, not "any character"
		{"", false},
		{"ann @x.com", false},
		{"ann@x.com\nbob@x.com", false},
	} {
		assert.Equal(t, tc.want, a.Allows(tc.email), "%q", tc.email)
	}
}

func TestAllowlistEmptyAdmitsNobody(t *testing.T) {
	for _, entries := range [][]string{nil, {}, {"", "  "}} {
		a, err := NewAllowlist(entries)
		require.NoError(t, err)
		assert.True(t, a.Empty())
		assert.False(t, a.Allows("a@x.com"))
	}
}

func TestAllowlistRefusesBadPattern(t *testing.T) {
	_, err := NewAllowlist([]string{"re:(unclosed"})
	assert.ErrorContains(t, err, "ALLOWED_EMAILS")
}
