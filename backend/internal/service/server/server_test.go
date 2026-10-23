package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const domain = "sql.example.com"

// selfSigned is a PEM certificate and key for dnsName, valid for validFor.
func selfSigned(t *testing.T, dnsName string, validFor time.Duration) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: dnsName},
		DNSNames:     []string{dnsName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(validFor),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func keyPair(t *testing.T, dnsName string, validFor time.Duration) *tls.Certificate {
	t.Helper()
	c, err := tls.X509KeyPair(selfSigned(t, dnsName, validFor))
	require.NoError(t, err)
	return &c
}

func testServer(t *testing.T) *Server {
	t.Helper()
	return New(Config{Domain: domain, HTTPSPort: 443, SSLCacheDir: t.TempDir()}, http.NotFoundHandler())
}

// fakeObtainer hands out a certificate it makes, or fails.
type fakeObtainer struct {
	t       *testing.T
	err     error
	badPEM  bool
	calls   int
	request certificate.ObtainRequest
}

func (f *fakeObtainer) Obtain(req certificate.ObtainRequest) (*certificate.Resource, error) {
	f.calls++
	f.request = req
	if f.err != nil {
		return nil, f.err
	}
	if f.badPEM {
		return &certificate.Resource{Certificate: []byte("nope"), PrivateKey: []byte("nope")}, nil
	}
	certPEM, keyPEM := selfSigned(f.t, req.Domains[0], 90*24*time.Hour)
	return &certificate.Resource{Certificate: certPEM, PrivateKey: keyPEM}, nil
}

func TestRedirect(t *testing.T) {
	for _, tc := range []struct {
		name      string
		httpsPort int
		host      string
		target    string
		status    int
		location  string
	}{
		{"default port", 443, domain, "/a?b=1", http.StatusMovedPermanently, "https://sql.example.com/a?b=1"},
		{"other port", 8443, domain + ":8080", "/", http.StatusMovedPermanently, "https://sql.example.com:8443/"},
		{"host case", 443, "SQL.Example.COM", "/x", http.StatusMovedPermanently, "https://sql.example.com/x"},
		{"other host", 443, "evil.com", "/", http.StatusNotFound, ""},
		{"no host", 443, "", "/", http.StatusNotFound, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(Config{Domain: domain, HTTPSPort: tc.httpsPort}, nil)
			r := httptest.NewRequest(http.MethodGet, tc.target, nil)
			r.Host = tc.host
			w := httptest.NewRecorder()
			s.redirect(w, r)
			assert.Equal(t, tc.status, w.Code)
			assert.Equal(t, tc.location, w.Header().Get("Location"))
		})
	}
}

func TestTLSConfigGetCertificate(t *testing.T) {
	s := testServer(t)
	cfg := s.tlsConfig()
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)

	_, err := cfg.GetCertificate(&tls.ClientHelloInfo{ServerName: domain})
	assert.ErrorContains(t, err, "not ready")

	s.cert = keyPair(t, domain, time.Hour)
	for _, name := range []string{domain, "SQL.EXAMPLE.COM"} {
		got, err := cfg.GetCertificate(&tls.ClientHelloInfo{ServerName: name})
		require.NoError(t, err)
		assert.Same(t, s.cert, got)
	}
	for _, name := range []string{"", "evil.com", "x." + domain} {
		_, err := cfg.GetCertificate(&tls.ClientHelloInfo{ServerName: name})
		assert.ErrorContains(t, err, "no certificate for")
	}
}

func TestUsable(t *testing.T) {
	s := testServer(t)
	garbage := &tls.Certificate{Certificate: [][]byte{[]byte("not der")}}
	for _, tc := range []struct {
		name string
		cert *tls.Certificate
		want bool
	}{
		{"nil", nil, false},
		{"empty", &tls.Certificate{}, false},
		{"unparsable", garbage, false},
		{"valid", keyPair(t, domain, 60*24*time.Hour), true},
		{"due for renewal", keyPair(t, domain, 10*24*time.Hour), false},
		{"expired", keyPair(t, domain, -time.Minute), false},
		{"other domain", keyPair(t, "other.com", 60*24*time.Hour), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, s.usable(tc.cert))
		})
	}
}

func TestInstall(t *testing.T) {
	s := testServer(t)
	certPEM, keyPEM := selfSigned(t, domain, 60*24*time.Hour)
	require.NoError(t, s.install(certPEM, keyPEM))
	require.NotNil(t, s.cert)

	certPath, keyPath := s.paths()
	got, err := os.ReadFile(certPath)
	require.NoError(t, err)
	assert.Equal(t, certPEM, got)
	got, err = os.ReadFile(keyPath)
	require.NoError(t, err)
	assert.Equal(t, keyPEM, got)
	info, err := os.Stat(keyPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestInstallRefusesBadPair(t *testing.T) {
	s := testServer(t)
	certPEM, _ := selfSigned(t, domain, time.Hour)
	_, otherKey := selfSigned(t, domain, time.Hour)
	for name, pair := range map[string][2][]byte{
		"garbage":      {[]byte("nope"), []byte("nope")},
		"mismatched":   {certPEM, otherKey},
		"key for cert": {otherKey, certPEM},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, s.install(pair[0], pair[1]))
			assert.Nil(t, s.cert)
			certPath, keyPath := s.paths()
			assert.NoFileExists(t, certPath)
			assert.NoFileExists(t, keyPath)
		})
	}
}

func TestInstallWriteFails(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, domain, time.Hour)

	s := New(Config{Domain: domain, SSLCacheDir: filepath.Join(t.TempDir(), "missing")}, nil)
	assert.Error(t, s.install(certPEM, keyPEM))

	// The certificate is written but the key is not.
	s = testServer(t)
	_, keyPath := s.paths()
	require.NoError(t, os.Mkdir(keyPath, 0o700))
	assert.Error(t, s.install(certPEM, keyPEM))
	assert.Nil(t, s.cert)
}

func TestLoadCertificate(t *testing.T) {
	s := testServer(t)
	assert.Error(t, s.loadCertificate(), "nothing on disk")

	certPath, keyPath := s.paths()
	write := func(dnsName string, validFor time.Duration) {
		certPEM, keyPEM := selfSigned(t, dnsName, validFor)
		require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))
		require.NoError(t, os.WriteFile(keyPath, keyPEM, 0o600))
	}

	write(domain, 10*24*time.Hour)
	assert.ErrorContains(t, s.loadCertificate(), "expiring or for another domain")
	write("other.com", 60*24*time.Hour)
	assert.ErrorContains(t, s.loadCertificate(), "expiring or for another domain")
	assert.Nil(t, s.cert)

	write(domain, 60*24*time.Hour)
	require.NoError(t, s.loadCertificate())
	assert.True(t, s.usable(s.cert))
}

func TestRefreshCertificate(t *testing.T) {
	s := testServer(t)
	obtainer := &fakeObtainer{t: t}
	s.obtainer = obtainer

	require.NoError(t, s.RefreshCertificate())
	assert.Equal(t, 1, obtainer.calls)
	assert.Equal(t, []string{domain}, obtainer.request.Domains)
	assert.True(t, obtainer.request.Bundle)
	assert.True(t, s.usable(s.cert))
	assert.NoError(t, s.loadCertificate(), "the certificate is on disk")

	// A usable certificate is kept.
	require.NoError(t, s.RefreshCertificate())
	assert.Equal(t, 1, obtainer.calls)

	// One due for renewal is replaced.
	s.cert = keyPair(t, domain, 24*time.Hour)
	require.NoError(t, s.RefreshCertificate())
	assert.Equal(t, 2, obtainer.calls)
	assert.True(t, s.usable(s.cert))
}

func TestRefreshCertificateFails(t *testing.T) {
	s := testServer(t)
	s.obtainer = &fakeObtainer{t: t, err: errors.New("rate limited")}
	assert.EqualError(t, s.RefreshCertificate(), "rate limited")
	assert.Nil(t, s.cert)

	s.obtainer = &fakeObtainer{t: t, badPEM: true}
	assert.Error(t, s.RefreshCertificate())
	assert.Nil(t, s.cert)
}

func TestShutdownIdempotent(t *testing.T) {
	s := testServer(t)
	require.NoError(t, s.Shutdown(context.Background()))
	require.NoError(t, s.Shutdown(context.Background()))
	select {
	case <-s.stop:
	default:
		t.Fatal("stop is not closed")
	}
}

func TestIgnoreClosed(t *testing.T) {
	assert.NoError(t, ignoreClosed(http.ErrServerClosed))
	assert.NoError(t, ignoreClosed(nil))
	err := errors.New("boom")
	assert.Equal(t, err, ignoreClosed(err))
}

// run starts s and returns a channel with what Run returned.
func run(s *Server) <-chan error {
	done := make(chan error, 1)
	go func() { done <- s.Run() }()
	return done
}

// shutdownWhenUp shuts s down once Run has created its listener's server.
func shutdownWhenUp(t *testing.T, s *Server, done <-chan error) {
	t.Helper()
	require.Eventually(t, func() bool {
		select {
		case err := <-done:
			t.Fatalf("Run returned early: %v", err)
		default:
		}
		s.srvMu.Lock()
		defer s.srvMu.Unlock()
		return s.httpSrv != nil
	}, 5*time.Second, 10*time.Millisecond)
	// Serve may not have started yet; Shutdown makes it return at once
	// either way.
	require.NoError(t, s.Shutdown(context.Background()))
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Shutdown")
	}
}

func TestRunHTTP(t *testing.T) {
	s := New(Config{HTTPPort: 0}, http.NotFoundHandler())
	shutdownWhenUp(t, s, run(s))
}

func TestRunHTTPS(t *testing.T) {
	s := New(Config{SSLEnabled: true, Domain: domain, SSLCacheDir: filepath.Join(t.TempDir(), "certs")}, http.NotFoundHandler())
	s.obtainer = &fakeObtainer{t: t}
	done := run(s)
	// The certificate is obtained in its own goroutine, so wait for it under
	// the lock rather than racing it.
	assert.Eventually(t, func() bool {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.usable(s.cert)
	}, 5*time.Second, 10*time.Millisecond, "the managed certificate was obtained")
	shutdownWhenUp(t, s, done)
}

func TestRunHTTPSUsesCertificateOnDisk(t *testing.T) {
	// The redirect port is taken; HTTPS is served regardless.
	taken := httptest.NewServer(http.NotFoundHandler())
	defer taken.Close()
	s := New(Config{SSLEnabled: true, Domain: domain, SSLCacheDir: t.TempDir(), HTTPPort: taken.Listener.Addr().(*net.TCPAddr).Port}, http.NotFoundHandler())
	certPEM, keyPEM := selfSigned(t, domain, 60*24*time.Hour)
	certPath, keyPath := s.paths()
	require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0o600))
	obtainer := &fakeObtainer{t: t}
	s.obtainer = obtainer

	shutdownWhenUp(t, s, run(s))
	assert.Zero(t, obtainer.calls)
}

func TestRunHTTPSFails(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	s := New(Config{SSLEnabled: true, Domain: domain, SSLCacheDir: filepath.Join(file, "certs")}, nil)
	assert.ErrorContains(t, s.Run(), "ssl cache dir")

	s = New(Config{SSLEnabled: true, Domain: domain, SSLCacheDir: t.TempDir(), HTTPSPort: -1}, nil)
	s.obtainer = &fakeObtainer{t: t, err: errors.New("offline")}
	assert.ErrorContains(t, s.Run(), "TLS listener")
	require.NoError(t, s.Shutdown(context.Background()))
}

func TestRunHTTPPortTaken(t *testing.T) {
	ln := httptest.NewServer(http.NotFoundHandler())
	defer ln.Close()
	s := New(Config{HTTPPort: ln.Listener.Addr().(*net.TCPAddr).Port}, nil)
	assert.Error(t, s.Run())
}

// A Shutdown that lands before Run has made its server must still stop Run.
func TestShutdownBeforeRunStopsRun(t *testing.T) {
	s := New(Config{HTTPPort: 0}, http.NotFoundHandler())
	require.NoError(t, s.Shutdown(context.Background()))
	done := run(s)
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run kept serving after Shutdown")
	}
}
