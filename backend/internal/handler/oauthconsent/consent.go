// Package oauthconsent is the page an MCP server's /oauth2/authorize shows a
// signed-in person before an app gets a code: which app is asking, which
// database it will read, and where it sends them back.
//
// The page is rendered here, not by the Vue app, so nothing that can drive the
// app's API can press Allow on someone's behalf.
package oauthconsent

import (
	"crypto/rand"
	"errors"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/rs/zerolog/log"
	"golang.org/x/net/idna"

	"github.com/agentrq/sql-mcp/backend/internal/service/auth"
)

// Tokens is the part of the token service the page needs.
type Tokens interface {
	CreateConsentToken(userID string, consent auth.Consent) (string, error)
	ValidateConsentToken(token, userID string, consent auth.Consent) error
	CreateCode(userID string, consent auth.Consent) (string, error)
}

// Request is one authorization request, its redirect_uri already checked
// with CheckRedirectURI.
type Request struct {
	UserID       string
	Email        string
	ClientName   string // validated; "" when the client gave none
	Registered   bool   // the client_id was minted by this server
	DatabaseName string
	Consent      auth.Consent
	// RegisteredRedirectURIs and BaseURL are what CheckRedirectURI was given.
	RegisteredRedirectURIs []string
	BaseURL                string
}

const (
	DecisionAllow = "allow"
	DecisionDeny  = "deny"
)

// Serve shows the consent page on GET and acts on the choice on POST. The
// form posts back to the authorize URL itself, so the query string arrives
// again and the caller validates it again.
func Serve(w http.ResponseWriter, r *http.Request, tokens Tokens, req Request) {
	if checked, err := CheckRedirectURI(req.Consent.RedirectURI, req.RegisteredRedirectURIs, req.BaseURL); err != nil || checked != req.Consent.RedirectURI {
		http.Error(w, ErrRedirectMalformed.Error(), http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		token, err := tokens.CreateConsentToken(req.UserID, req.Consent)
		if err != nil {
			log.Error().Err(err).Msg("could not mint a consent token")
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		render(w, newPage(req, token))
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		if err := tokens.ValidateConsentToken(r.PostForm.Get("consent"), req.UserID, req.Consent); err != nil {
			http.Error(w, "This approval has expired or does not match this request. Go back to the app and connect again.", http.StatusBadRequest)
			return
		}
		state := url.Values{"state": {req.Consent.State}}
		switch r.PostForm.Get("decision") {
		case DecisionAllow:
			code, err := tokens.CreateCode(req.UserID, req.Consent)
			if err != nil {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			state.Set("code", code)
		case DecisionDeny:
			// RFC 6749 §4.1.2.1: the client learns the person said no.
			state.Set("error", "access_denied")
		default:
			http.Error(w, "decision must be allow or deny", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, RedirectURL(req.Consent.RedirectURI, state), http.StatusFound)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// RedirectURL adds params to redirectURI, keeping its own query.
func RedirectURL(redirectURI string, params url.Values) string {
	sep := "?"
	if strings.Contains(redirectURI, "?") {
		sep = "&"
	}
	return redirectURI + sep + params.Encode()
}

// LoginURL sends a signed-out person to the app's login page, and back to
// returnURL afterwards.
func LoginURL(baseURL, returnURL string) string {
	return baseURL + "/login?" + url.Values{"redirect_url": {returnURL}}.Encode()
}

// The reasons a redirect_uri is refused, worded for whoever reads the 400.
var (
	ErrRedirectMissing       = errors.New("invalid redirect_uri: required unless the client registered exactly one")
	ErrRedirectNotRegistered = errors.New("invalid redirect_uri: not registered for this client_id")
	ErrRedirectMalformed     = errors.New("invalid redirect_uri")
	ErrRedirectFragment      = errors.New("invalid redirect_uri: must not contain a fragment")
	ErrRedirectUserinfo      = errors.New("invalid redirect_uri: must not contain a user name or password")
	ErrRedirectHost          = errors.New("invalid redirect_uri: host is not a valid hostname")
	ErrRedirectScheme        = errors.New("invalid redirect_uri: unrecognized custom scheme; register the redirect_uri to use it")
	ErrRedirectInsecure      = errors.New("invalid redirect_uri: https required for non-localhost")
	ErrRedirectHostMismatch  = errors.New("invalid redirect_uri: host mismatch")
)

// CheckRedirectURI decides where an authorization response may go (RFC 6749
// §3.1.2.3). A client that registered redirect_uris is held to them exactly,
// a loopback one on any port (RFC 8252 §7.3). One that registered none may
// use this server's origin, loopback, or a known native app's scheme.
func CheckRedirectURI(requested string, registered []string, baseURL string) (string, error) {
	if requested == "" {
		if len(registered) != 1 {
			return "", ErrRedirectMissing
		}
		requested = registered[0]
	}
	u, err := url.Parse(requested)
	if err != nil || !u.IsAbs() {
		return "", ErrRedirectMalformed
	}
	if strings.Contains(requested, "#") {
		return "", ErrRedirectFragment
	}
	if u.User != nil {
		return "", ErrRedirectUserinfo
	}
	if isWeb(u) {
		if _, ok := displayHost(u); !ok {
			return "", ErrRedirectHost
		}
	}
	if len(registered) > 0 {
		if !auth.AnyRedirectURIMatches(registered, requested) {
			return "", ErrRedirectNotRegistered
		}
		return requested, nil
	}
	if !isWeb(u) {
		if !auth.IsAllowedNativeRedirectScheme(u.Scheme) {
			return "", ErrRedirectScheme
		}
		return requested, nil
	}
	if isLoopback(u) {
		return requested, nil
	}
	if u.Scheme != "https" {
		return "", ErrRedirectInsecure
	}
	if base, err := url.Parse(baseURL); err != nil || u.Host != base.Host {
		return "", ErrRedirectHostMismatch
	}
	return requested, nil
}

func isWeb(u *url.URL) bool { return u.Scheme == "http" || u.Scheme == "https" }

func isLoopback(u *url.URL) bool {
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// displayHost is the host as the page shows it: international names in
// their xn-- form, so a lookalike cannot pass for the real one.
func displayHost(u *url.URL) (string, bool) {
	host := u.Hostname()
	if net.ParseIP(host) == nil {
		ascii, err := idna.Lookup.ToASCII(host)
		if err != nil || ascii == "" || len(ascii) > 253 {
			return "", false
		}
		host = ascii
	}
	if port := u.Port(); port != "" {
		host = net.JoinHostPort(host, port)
	}
	return host, true
}

// destination says where the code will go, in the page's words.
func destination(redirectURI string) string {
	u, _ := url.Parse(redirectURI)
	if !isWeb(u) {
		return "the app that opens " + u.Scheme + ": links"
	}
	host, _ := displayHost(u)
	if isLoopback(u) {
		return "an app on this computer (" + host + ")"
	}
	return host
}

type page struct {
	ClientName   string
	Named        bool
	Registered   bool
	DatabaseName string
	Destination  string
	Email        string
	Token        string
	Nonce        string
	FormAction   string // the CSP form-action sources
}

func newPage(req Request, token string) page {
	return page{
		ClientName:   req.ClientName,
		Named:        req.ClientName != "",
		Registered:   req.Registered,
		DatabaseName: req.DatabaseName,
		Destination:  destination(req.Consent.RedirectURI),
		Email:        req.Email,
		Token:        token,
		FormAction:   "'self' " + formActionSource(req.Consent.RedirectURI),
	}
}

// formActionSource is where the form may end up. Chrome checks form-action
// against the redirect a POST answers with too, so 'self' alone blocks the
// redirect to the app and Allow does nothing. The redirect URI was checked,
// so its scheme and host hold no CSP syntax.
func formActionSource(redirectURI string) string {
	u, _ := url.Parse(redirectURI)
	if !isWeb(u) {
		return u.Scheme + ":"
	}
	// An IP literal ([::1]) is not a host-source CSP matches reliably.
	if net.ParseIP(u.Hostname()) != nil {
		return u.Scheme + ":"
	}
	host, _ := displayHost(u)
	return u.Scheme + "://" + host
}

func render(w http.ResponseWriter, p page) {
	p.Nonce = rand.Text()
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	// A live approval: never cached, never framed (a framed Allow button can
	// be clickjacked), and no script but the theme one.
	h.Set("Cache-Control", "no-store")
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-"+p.Nonce+"'; form-action "+p.FormAction+"; base-uri 'none'; frame-ancestors 'none'")
	if err := pageTemplate.Execute(w, p); err != nil {
		log.Error().Err(err).Msg("could not render the consent page")
	}
}

var pageTemplate = template.Must(template.New("consent").Parse(pageHTML))
