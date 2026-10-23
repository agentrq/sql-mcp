// Package security seals secrets at rest — a database's DSN, a person's
// tmphost token — with AES-256-GCM, and compares secrets in constant time.
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
)

// Sealer encrypts with one key. The nonce is stored with the ciphertext, so a
// sealed value is one opaque string.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer needs a 32-byte key.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != 32 {
		return nil, errors.New("security: key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts plaintext. The same plaintext seals differently every time.
func (s *Sealer) Seal(plaintext string) (string, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := s.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawStdEncoding.EncodeToString(out), nil
}

// Open decrypts what Seal returned, failing on any tampering.
func (s *Sealer) Open(sealed string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(sealed)
	if err != nil {
		return "", errors.New("security: sealed value is not valid base64")
	}
	n := s.aead.NonceSize()
	if len(raw) < n {
		return "", errors.New("security: sealed value is too short")
	}
	plain, err := s.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", errors.New("security: sealed value could not be opened")
	}
	return string(plain), nil
}

// SecureCompare compares two secrets in constant time.
func SecureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
