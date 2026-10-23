// Package server runs the HTTP listener, and with TLS enabled, obtains and
// renews a Let's Encrypt certificate for the app's domain.
//
// Certificates are obtained with the DNS-01 challenge through Cloudflare, the
// way AgentRQ does it. DNS-01 never connects back to the server, so neither
// port has to be 80 or 443: HTTPS_PORT and HTTP_PORT can be anything.
package server

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/registration"
	"github.com/rs/zerolog/log"
)

// Config is how the server listens.
type Config struct {
	HTTPPort           int
	HTTPSPort          int
	SSLEnabled         bool
	Domain             string
	SSLCacheDir        string
	LetsencryptEmail   string
	CloudflareAPIToken string
	ACMEDirectoryURL   string
}

// Obtainer gets a certificate from the CA; lego's certifier in production.
type Obtainer interface {
	Obtain(request certificate.ObtainRequest) (*certificate.Resource, error)
}

// Server is the app's listener.
type Server struct {
	cfg      Config
	handler  http.Handler
	obtainer Obtainer

	mu   sync.RWMutex
	cert *tls.Certificate

	// srvMu guards the servers and stopped: Shutdown may run before or while
	// Run makes them.
	srvMu       sync.Mutex
	httpSrv     *http.Server
	redirectSrv *http.Server
	stopped     bool
	stop        chan struct{}
}

// New is a server for handler.
func New(cfg Config, handler http.Handler) *Server {
	return &Server{cfg: cfg, handler: handler, stop: make(chan struct{})}
}

const (
	certFile        = "cert.pem"
	keyFile         = "key.pem"
	certRenewBefore = 30 * 24 * time.Hour
	certCheckEvery  = 12 * time.Hour
)

// Run serves until Shutdown.
func (s *Server) Run() error {
	if !s.cfg.SSLEnabled {
		srv := &http.Server{Addr: fmt.Sprintf(":%d", s.cfg.HTTPPort), Handler: s.handler, ReadHeaderTimeout: 10 * time.Second}
		if !s.setServers(srv, nil) {
			return nil
		}
		log.Info().Str("addr", srv.Addr).Msg("serving HTTP")
		return ignoreClosed(srv.ListenAndServe())
	}

	if err := os.MkdirAll(s.cfg.SSLCacheDir, 0o700); err != nil {
		return fmt.Errorf("ssl cache dir: %w", err)
	}
	if err := s.loadCertificate(); err != nil {
		log.Info().Err(err).Msg("no usable certificate on disk yet; obtaining one")
	}
	go s.manageCertificate()

	// The plain port only redirects to HTTPS, and only for the app's domain.
	redirectSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", s.cfg.HTTPPort),
		Handler:           http.HandlerFunc(s.redirect),
		ReadHeaderTimeout: 10 * time.Second,
	}
	httpSrv := &http.Server{Handler: s.handler, ReadHeaderTimeout: 10 * time.Second}
	if !s.setServers(httpSrv, redirectSrv) {
		return nil
	}
	go func() {
		if err := redirectSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("HTTP redirect server stopped")
		}
	}()

	ln, err := tls.Listen("tcp", fmt.Sprintf(":%d", s.cfg.HTTPSPort), s.tlsConfig())
	if err != nil {
		return fmt.Errorf("TLS listener: %w", err)
	}
	log.Info().Str("addr", ln.Addr().String()).Str("domain", s.cfg.Domain).Msg("serving HTTPS")
	return ignoreClosed(httpSrv.Serve(ln))
}

// setServers records the servers Run made, or reports false when Shutdown
// already ran, in which case Run must not serve.
func (s *Server) setServers(httpSrv, redirectSrv *http.Server) bool {
	s.srvMu.Lock()
	defer s.srvMu.Unlock()
	if s.stopped {
		return false
	}
	s.httpSrv, s.redirectSrv = httpSrv, redirectSrv
	return true
}

func ignoreClosed(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request) {
	host := strings.ToLower(r.Host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host != s.cfg.Domain {
		http.Error(w, "host not allowed", http.StatusNotFound)
		return
	}
	target := "https://" + host
	if s.cfg.HTTPSPort != 443 {
		target += fmt.Sprintf(":%d", s.cfg.HTTPSPort)
	}
	http.Redirect(w, r, target+r.URL.RequestURI(), http.StatusMovedPermanently)
}

func (s *Server) tlsConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			if !strings.EqualFold(hello.ServerName, s.cfg.Domain) {
				return nil, fmt.Errorf("no certificate for %q", hello.ServerName)
			}
			s.mu.RLock()
			defer s.mu.RUnlock()
			if s.cert == nil {
				return nil, errors.New("certificate not ready yet")
			}
			return s.cert, nil
		},
	}
}

func (s *Server) paths() (string, string) {
	return filepath.Join(s.cfg.SSLCacheDir, certFile), filepath.Join(s.cfg.SSLCacheDir, keyFile)
}

func (s *Server) loadCertificate() error {
	certPath, keyPath := s.paths()
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return err
	}
	if !s.usable(&cert) {
		return errors.New("certificate on disk is expiring or for another domain")
	}
	s.mu.Lock()
	s.cert = &cert
	s.mu.Unlock()
	return nil
}

// usable reports whether cert covers the domain and is not due for renewal.
func (s *Server) usable(cert *tls.Certificate) bool {
	if cert == nil || len(cert.Certificate) == 0 {
		return false
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return false
	}
	return time.Until(leaf.NotAfter) > certRenewBefore && slices.Contains(leaf.DNSNames, s.cfg.Domain)
}

func (s *Server) manageCertificate() {
	for {
		if err := s.RefreshCertificate(); err != nil {
			log.Error().Err(err).Msg("could not obtain a certificate; retrying later")
		}
		select {
		case <-s.stop:
			return
		case <-time.After(certCheckEvery):
		}
	}
}

// RefreshCertificate obtains a new certificate when the one served is
// missing or due for renewal.
func (s *Server) RefreshCertificate() error {
	s.mu.RLock()
	current := s.cert
	s.mu.RUnlock()
	if s.usable(current) {
		return nil
	}
	if s.obtainer == nil {
		obtainer, err := s.newObtainer()
		if err != nil {
			return err
		}
		s.obtainer = obtainer
	}
	res, err := s.obtainer.Obtain(certificate.ObtainRequest{Domains: []string{s.cfg.Domain}, Bundle: true})
	if err != nil {
		return err
	}
	return s.install(res.Certificate, res.PrivateKey)
}

// install writes the pair to disk and serves it; a pair that does not parse
// is refused before the files are touched.
func (s *Server) install(certPEM, keyPEM []byte) error {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return err
	}
	certPath, keyPath := s.paths()
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return err
	}
	s.mu.Lock()
	s.cert = &cert
	s.mu.Unlock()
	log.Info().Str("domain", s.cfg.Domain).Msg("certificate installed")
	return nil
}

type acmeUser struct {
	email string
	reg   *registration.Resource
	key   crypto.PrivateKey
}

func (u *acmeUser) GetEmail() string                        { return u.email }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.reg }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

func (s *Server) newObtainer() (Obtainer, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	email := s.cfg.LetsencryptEmail
	if email == "" {
		email = "admin@" + s.cfg.Domain
	}
	user := &acmeUser{email: email, key: key}
	cfg := lego.NewConfig(user)
	cfg.CADirURL = s.cfg.ACMEDirectoryURL
	cfg.Certificate.KeyType = certcrypto.EC256
	client, err := lego.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("ACME client: %w", err)
	}
	cf := cloudflare.NewDefaultConfig()
	cf.AuthToken = s.cfg.CloudflareAPIToken
	provider, err := cloudflare.NewDNSProviderConfig(cf)
	if err != nil {
		return nil, fmt.Errorf("cloudflare DNS provider: %w", err)
	}
	if err := client.Challenge.SetDNS01Provider(provider); err != nil {
		return nil, err
	}
	if user.reg, err = client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true}); err != nil {
		return nil, fmt.Errorf("ACME registration: %w", err)
	}
	return client.Certificate, nil
}

// Shutdown stops listening, letting requests in flight finish within ctx.
func (s *Server) Shutdown(ctx context.Context) error {
	s.srvMu.Lock()
	if !s.stopped {
		s.stopped = true
		close(s.stop)
	}
	httpSrv, redirectSrv := s.httpSrv, s.redirectSrv
	s.srvMu.Unlock()
	if redirectSrv != nil {
		_ = redirectSrv.Shutdown(ctx)
	}
	if httpSrv != nil {
		return httpSrv.Shutdown(ctx)
	}
	return nil
}
