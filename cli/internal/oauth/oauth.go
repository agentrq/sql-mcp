// Package oauth signs the CLI in to a SQL MCP server: the authorization code
// flow with PKCE and a loopback redirect (RFC 8252), and refreshing.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ClientID is the CLI's built-in public client on every SQL MCP server.
const ClientID = "sqlmcp-cli"

// The server's endpoints for the CLI.
const (
	AuthorizePath = "/cli/oauth2/authorize"
	TokenPath     = "/cli/oauth2/token"
)

// listen is swapped in tests to make the loopback listener fail.
var listen = net.Listen

// Token is what the token endpoint grants.
type Token struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
	// Databases are the IDs the person picked on the consent page.
	Databases []string
}

// Error is an OAuth error answer, such as invalid_grant.
type Error struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func (e *Error) Error() string {
	if e.Description == "" {
		return e.Code
	}
	return e.Code + ": " + e.Description
}

// Client talks to one server's CLI endpoints.
type Client struct {
	Server string
	HTTP   *http.Client
	Now    func() time.Time
}

// Exchange trades an authorization code for tokens.
func (c *Client) Exchange(ctx context.Context, code, redirectURI, verifier string) (*Token, error) {
	return c.token(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {ClientID},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	})
}

// Refresh trades a refresh token for new tokens. The server rotates the
// refresh token, so the returned one replaces it.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*Token, error) {
	return c.token(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {ClientID},
	})
}

func (c *Client) token(ctx context.Context, form url.Values) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Server+TokenPath, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		e := &Error{}
		if json.NewDecoder(resp.Body).Decode(e) != nil || e.Code == "" {
			e = &Error{Code: "server_error", Description: "the token endpoint answered " + resp.Status}
		}
		return nil, e
	}
	var body struct {
		AccessToken  string   `json:"access_token"`
		TokenType    string   `json:"token_type"`
		ExpiresIn    int64    `json:"expires_in"`
		RefreshToken string   `json:"refresh_token"`
		Databases    []string `json:"databases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("read the token answer: %w", err)
	}
	if body.AccessToken == "" || !strings.EqualFold(body.TokenType, "bearer") {
		return nil, errors.New("the token endpoint gave no bearer token")
	}
	return &Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		Expiry:       c.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
		Databases:    body.Databases,
	}, nil
}

// LoginOptions shape the interactive login.
type LoginOptions struct {
	// Open shows the consent page, normally in the browser.
	Open func(url string) error
	// Out gets the URL to open by hand when the browser does not.
	Out io.Writer
	// Timeout bounds the wait for the person in the browser.
	Timeout time.Duration
}

// Login listens on a loopback port, sends the person to the consent page,
// where they pick the databases, and exchanges the code the browser brings
// back.
func (c *Client) Login(ctx context.Context, o LoginOptions) (*Token, error) {
	ln, err := listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for the browser: %w", err)
	}
	redirectURI := fmt.Sprintf("http://%s/callback", ln.Addr())
	verifier, state := rand.Text()+rand.Text(), rand.Text()

	results := make(chan result, 1)
	srv := &http.Server{Handler: callback(state, results), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		// Shutdown, not Close, so the browser still gets its page.
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	authURL := c.AuthorizeURL(redirectURI, Challenge(verifier), state)
	fmt.Fprintf(o.Out, "Opening the browser to sign in and pick the databases. If it does not open, visit:\n\n  %s\n\n", authURL)
	if err := o.Open(authURL); err != nil {
		fmt.Fprintf(o.Out, "Could not open the browser: %v\n", err)
	}

	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	select {
	case r := <-results:
		if r.err != nil {
			return nil, r.err
		}
		return c.Exchange(ctx, r.code, redirectURI, verifier)
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("gave up after %s waiting for the browser", o.Timeout)
		}
		return nil, errors.New("login canceled")
	}
}

// AuthorizeURL is the consent page for this login.
func (c *Client) AuthorizeURL(redirectURI, challenge, state string) string {
	return c.Server + AuthorizePath + "?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {ClientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}.Encode()
}

type result struct {
	code string
	err  error
}

const callbackPage = `<!doctype html><meta charset="utf-8"><title>SQL MCP</title>
<body style="font-family:system-ui,sans-serif;text-align:center;margin-top:20vh"><h1>%s</h1><p>%s</p></body>`

// callback answers the browser's redirect and hands Login the outcome. Only
// the first one counts.
func callback(state string, out chan<- result) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var res result
		switch {
		// A redirect that is not for this login's request (login CSRF)
		// must not be used.
		case q.Get("state") != state:
			res.err = errors.New("the browser came back with the wrong state; run sqlmcp login again")
		case q.Get("error") != "":
			res.err = fmt.Errorf("sign-in failed: %w", &Error{Code: q.Get("error"), Description: q.Get("error_description")})
		case q.Get("code") == "":
			res.err = errors.New("the browser came back without a code")
		default:
			res.code = q.Get("code")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if res.err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, callbackPage, "Sign-in failed", "Return to the terminal for details.")
		} else {
			fmt.Fprintf(w, callbackPage, "Signed in to SQL MCP", "You can close this tab.")
		}
		select {
		case out <- res:
		default:
		}
	})
	return mux
}

// Challenge is the S256 PKCE challenge of a verifier.
func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// OpenBrowser opens url in the default browser.
func OpenBrowser(url string) error {
	return browser(runtime.GOOS, url).Start()
}

func browser(goos, url string) *exec.Cmd {
	switch goos {
	case "darwin":
		return exec.Command("open", url)
	case "windows":
		// cmd's start would split the URL at &.
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return exec.Command("xdg-open", url)
	}
}
