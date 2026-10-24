package machine

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

// fake swaps the system calls for the test's.
func fake(t *testing.T, system string, files map[string]string, out string, outErr error) {
	t.Helper()
	oldGOOS, oldRead, oldOut, oldUser, oldCache := goos, readFile, output, currentUser, userCacheDir
	t.Cleanup(func() {
		goos, readFile, output, currentUser, userCacheDir = oldGOOS, oldRead, oldOut, oldUser, oldCache
	})
	cache := t.TempDir()
	userCacheDir = func() (string, error) { return cache, nil }
	goos = system
	readFile = func(p string) ([]byte, error) {
		if s, ok := files[p]; ok {
			return []byte(s), nil
		}
		if strings.HasPrefix(p, os.TempDir()) {
			return os.ReadFile(p)
		}
		return nil, errors.New("no such file")
	}
	output = func(string, ...string) ([]byte, error) { return []byte(out), outErr }
	currentUser = func() (*user.User, error) { return &user.User{Uid: "1000", Username: "ann"}, nil }
}

func TestID(t *testing.T) {
	for _, tc := range []struct {
		name, goos string
		files      map[string]string
		out        string
		outErr     error
		want       string
	}{
		{"linux", "linux", map[string]string{"/etc/machine-id": "abc123\n"}, "", nil, "abc123"},
		{"linux dbus", "linux", map[string]string{"/etc/machine-id": " \n", "/var/lib/dbus/machine-id": "dbus1"}, "", nil, "dbus1"},
		{"freebsd hostid", "freebsd", map[string]string{"/etc/hostid": "host-uuid"}, "", nil, "host-uuid"},
		{"linux none", "linux", nil, "", nil, ""},
		{"darwin", "darwin", nil, `| "IOPlatformUUID" = "5E1F-AA"` + "\n", nil, "5E1F-AA"},
		{"darwin no uuid", "darwin", nil, "nothing", nil, ""},
		{"darwin fails", "darwin", nil, "", errors.New("no ioreg"), ""},
		{"windows", "windows", nil, "\r\nHKEY_LOCAL_MACHINE\\SOFTWARE\\Microsoft\\Cryptography\r\n    MachineGuid    REG_SZ    1b2c-3d\r\n", nil, "1b2c-3d"},
		{"windows no guid", "windows", nil, "nothing", nil, ""},
		{"windows fails", "windows", nil, "", errors.New("no reg"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake(t, tc.goos, tc.files, tc.out, tc.outErr)
			got, err := ID()
			if tc.want == "" {
				// No system ID: a random one stands in, and stays.
				again, err2 := ID()
				if err != nil || err2 != nil || len(got) < 32 || again != got {
					t.Fatalf("stand-in IDs %q, %q: %v, %v", got, again, err, err2)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestKeyIsBoundToMachineAndUser(t *testing.T) {
	fake(t, "linux", map[string]string{"/etc/machine-id": "m1"}, "", nil)
	k1, err := Key()
	if err != nil || len(k1) != 32 {
		t.Fatalf("Key: %d bytes, %v", len(k1), err)
	}
	again, _ := Key()
	if string(again) != string(k1) {
		t.Fatal("the key is not stable")
	}

	fake(t, "linux", map[string]string{"/etc/machine-id": "m2"}, "", nil)
	if k, _ := Key(); string(k) == string(k1) {
		t.Fatal("another machine derived the same key")
	}
	fake(t, "linux", map[string]string{"/etc/machine-id": "m1"}, "", nil)
	currentUser = func() (*user.User, error) { return &user.User{Uid: "1001", Username: "bob"}, nil }
	if k, _ := Key(); string(k) == string(k1) {
		t.Fatal("another user derived the same key")
	}

	currentUser = func() (*user.User, error) { return nil, errors.New("no passwd") }
	if _, err := Key(); err == nil || !strings.Contains(err.Error(), "OS user") {
		t.Fatalf("Key without a user: %v", err)
	}
	fake(t, "linux", nil, "", nil)
	userCacheDir = func() (string, error) { return "", errors.New("no home") }
	if _, err := Key(); err == nil || !strings.Contains(err.Error(), "no cache directory") {
		t.Fatalf("Key without any ID: %v", err)
	}
}

func TestLocalIDCannotBeKept(t *testing.T) {
	fake(t, "linux", nil, "", nil)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	userCacheDir = func() (string, error) { return file, nil }
	if _, err := ID(); err == nil || !strings.Contains(err.Error(), "could not be kept") {
		t.Fatalf("ID under a file: %v", err)
	}
}

func TestSealer(t *testing.T) {
	if _, err := NewSealer([]byte("short")); err == nil {
		t.Fatal("a short key was accepted")
	}
	key := []byte(strings.Repeat("k", 32))
	s, err := NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed := s.Seal([]byte("tokens"), "https://sql.example.com")
	if !strings.HasPrefix(sealed, "v1.") || strings.Contains(sealed, "tokens") {
		t.Fatalf("sealed: %q", sealed)
	}
	if plain, err := s.Open(sealed, "https://sql.example.com"); err != nil || string(plain) != "tokens" {
		t.Fatalf("Open: %q, %v", plain, err)
	}

	other, _ := NewSealer([]byte(strings.Repeat("x", 32)))
	for name, open := range map[string]func() ([]byte, error){
		"other key":     func() ([]byte, error) { return other.Open(sealed, "https://sql.example.com") },
		"other context": func() ([]byte, error) { return s.Open(sealed, "https://evil.example.com") },
		"no prefix":     func() ([]byte, error) { return s.Open(strings.TrimPrefix(sealed, "v1."), "https://sql.example.com") },
		"not base64":    func() ([]byte, error) { return s.Open("v1.***", "https://sql.example.com") },
		"too short":     func() ([]byte, error) { return s.Open("v1.AAAA", "https://sql.example.com") },
	} {
		if _, err := open(); !errors.Is(err, ErrOpen) {
			t.Errorf("%s: %v, want ErrOpen", name, err)
		}
	}
}

func TestOutputRunsTheCommand(t *testing.T) {
	out, err := output("go", "env", "GOOS")
	if err != nil || strings.TrimSpace(string(out)) == "" {
		t.Fatalf("output: %q, %v", out, err)
	}
}
