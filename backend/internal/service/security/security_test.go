package security

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSealer(t *testing.T) *Sealer {
	t.Helper()
	s, err := NewSealer(bytes.Repeat([]byte{7}, 32))
	require.NoError(t, err)
	return s
}

func TestNewSealerKeyLength(t *testing.T) {
	for _, n := range []int{0, 16, 31, 33} {
		_, err := NewSealer(make([]byte, n))
		assert.Error(t, err, "%d bytes", n)
	}
}

func TestSealOpen(t *testing.T) {
	s := testSealer(t)
	for _, plain := range []string{"", "postgres://u:p@h/db", "ünïcode ✓"} {
		a, err := s.Seal(plain)
		require.NoError(t, err)
		b, err := s.Seal(plain)
		require.NoError(t, err)
		assert.NotEqual(t, a, b, "a fresh nonce every time")

		got, err := s.Open(a)
		require.NoError(t, err)
		assert.Equal(t, plain, got)
	}
}

func TestOpenRefuses(t *testing.T) {
	s := testSealer(t)
	sealed, err := s.Seal("secret")
	require.NoError(t, err)
	raw, _ := base64.RawStdEncoding.DecodeString(sealed)
	raw[len(raw)-1] ^= 1
	tampered := base64.RawStdEncoding.EncodeToString(raw)

	other, err := NewSealer(bytes.Repeat([]byte{8}, 32))
	require.NoError(t, err)

	for _, tc := range []struct {
		name   string
		sealer *Sealer
		sealed string
		want   string
	}{
		{"not base64", s, "!!!", "not valid base64"},
		{"too short", s, base64.RawStdEncoding.EncodeToString([]byte("short")), "too short"},
		{"tampered", s, tampered, "could not be opened"},
		{"other key", other, sealed, "could not be opened"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.sealer.Open(tc.sealed)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestSecureCompare(t *testing.T) {
	assert.True(t, SecureCompare("abc", "abc"))
	assert.True(t, SecureCompare("", ""))
	assert.False(t, SecureCompare("abc", "abd"))
	assert.False(t, SecureCompare("abc", "abcd"))
}
