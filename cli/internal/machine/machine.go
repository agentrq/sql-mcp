// Package machine binds the CLI's saved tokens to this computer and OS
// user. They are sealed with a key derived from the machine's ID and the
// user, so a config file copied to another machine, or read by another
// account, opens nowhere else.
package machine

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// The system calls the key depends on, swapped in tests.
var (
	goos         = runtime.GOOS
	readFile     = os.ReadFile
	currentUser  = user.Current
	userCacheDir = os.UserCacheDir
	output       = func(name string, args ...string) ([]byte, error) { return exec.Command(name, args...).Output() }
)

// idFiles hold the machine ID on Linux and the BSDs, most specific first.
var idFiles = []string{"/etc/machine-id", "/var/lib/dbus/machine-id", "/etc/hostid"}

var (
	macUUID     = regexp.MustCompile(`"IOPlatformUUID"\s*=\s*"([^"]+)"`)
	windowsGUID = regexp.MustCompile(`MachineGuid\s+REG_SZ\s+(\S+)`)
)

// ID returns the machine's stable ID: /etc/machine-id on Linux,
// IOPlatformUUID on macOS, MachineGuid on Windows. A machine that has none
// (some containers) gets a random one, kept in the user's cache directory:
// apart from the config, so a synced or leaked config still opens nowhere.
func ID() (string, error) {
	if id := systemID(); id != "" {
		return id, nil
	}
	return localID()
}

func systemID() string {
	var id string
	switch goos {
	case "darwin":
		if out, err := output("ioreg", "-rd1", "-c", "IOPlatformExpertDevice"); err == nil {
			if m := macUUID.FindSubmatch(out); m != nil {
				id = string(m[1])
			}
		}
	case "windows":
		if out, err := output("reg", "query", `HKLM\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid"); err == nil {
			if m := windowsGUID.FindSubmatch(out); m != nil {
				id = string(m[1])
			}
		}
	default:
		for _, p := range idFiles {
			if b, err := readFile(p); err == nil && strings.TrimSpace(string(b)) != "" {
				id = strings.TrimSpace(string(b))
				break
			}
		}
	}
	return id
}

// localID reads, or the first time makes, the stand-in machine ID.
func localID() (string, error) {
	dir, err := userCacheDir()
	if err != nil {
		return "", fmt.Errorf("this machine has no ID, and no cache directory to keep one in: %w", err)
	}
	p := filepath.Join(dir, "sqlmcp", "machine-id")
	if b, err := readFile(p); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return strings.TrimSpace(string(b)), nil
	}
	id := rand.Text() + rand.Text()
	err = os.MkdirAll(filepath.Dir(p), 0o700)
	if err == nil {
		err = os.WriteFile(p, []byte(id+"\n"), 0o600)
	}
	if err != nil {
		return "", fmt.Errorf("this machine has no ID, and one could not be kept in %s: %w", p, err)
	}
	return id, nil
}

// keySalt versions the derivation: a new one makes every saved login
// unreadable, which only costs a new login.
const keySalt = "sqlmcp-cli tokens v1"

// Key derives the 256-bit key that seals the CLI's tokens for this user on
// this machine.
func Key() ([]byte, error) {
	id, err := ID()
	if err != nil {
		return nil, err
	}
	u, err := currentUser()
	if err != nil {
		return nil, fmt.Errorf("could not tell who the OS user is: %w", err)
	}
	return hkdf.Key(sha256.New, []byte(id), []byte(keySalt), u.Uid+"\x00"+u.Username, 32)
}

// sealedPrefix marks the sealing format, so a later one can be told apart.
const sealedPrefix = "v1."

// ErrOpen is a sealed value this key cannot open: sealed on another
// machine, by another user, for another server, or tampered with.
var ErrOpen = errors.New("the saved login was not made on this machine for this user")

// Sealer seals and opens values with AES-256-GCM.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer refuses a key that is not 32 bytes.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != 32 {
		return nil, errors.New("machine: the key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	var aead cipher.AEAD
	if err == nil {
		aead, err = cipher.NewGCM(block)
	}
	return &Sealer{aead: aead}, err
}

// Seal encrypts plain, bound to context: Open must be given the same one.
func (s *Sealer) Seal(plain []byte, context string) string {
	nonce := make([]byte, s.aead.NonceSize())
	// crypto/rand.Read never fails; it crashes the program instead.
	_, _ = rand.Read(nonce)
	return sealedPrefix + base64.RawURLEncoding.EncodeToString(s.aead.Seal(nonce, nonce, plain, []byte(context)))
}

// Open decrypts what Seal returned for the same context.
func (s *Sealer) Open(sealed, context string) ([]byte, error) {
	rest, ok := strings.CutPrefix(sealed, sealedPrefix)
	raw, err := base64.RawURLEncoding.DecodeString(rest)
	if !ok || err != nil || len(raw) < s.aead.NonceSize() {
		return nil, ErrOpen
	}
	n := s.aead.NonceSize()
	plain, err := s.aead.Open(nil, raw[:n], raw[n:], []byte(context))
	if err != nil {
		return nil, ErrOpen
	}
	return plain, nil
}
