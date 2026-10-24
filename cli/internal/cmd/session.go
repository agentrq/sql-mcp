package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/agentrq/sql-mcp/cli/internal/client"
	"github.com/agentrq/sql-mcp/cli/internal/config"
	"github.com/agentrq/sql-mcp/cli/internal/machine"
	"github.com/agentrq/sql-mcp/cli/internal/oauth"
)

// refreshMargin refreshes the access token this long before it expires, so
// a request never starts with a token that dies on the way.
const refreshMargin = time.Minute

var errLoginExpired = errors.New(`the login has expired: run "sqlmcp login"`)

// session is the login's tokens. It is the client's TokenSource: it
// refreshes the access token when needed and saves the rotated tokens,
// sealed again.
type session struct {
	a      *app
	sealer *machine.Sealer
	server string
	login  config.Login
}

// openSession unseals the saved login for the server the command is for.
func (a *app) openSession() (*session, error) {
	f, err := config.Load(a.opts.Getenv)
	if err != nil {
		return nil, err
	}
	server, err := a.serverURL(f)
	if err != nil {
		return nil, err
	}
	// Tokens only ever go to the server that issued them.
	if f.Login == "" || f.Server != server {
		return nil, fmt.Errorf(`not logged in to %s: run "sqlmcp login"`, server)
	}
	s, err := a.sealer()
	if err != nil {
		return nil, err
	}
	raw, err := s.Open(f.Login, server)
	var login config.Login
	if err == nil {
		err = json.Unmarshal(raw, &login)
	}
	if err != nil {
		return nil, fmt.Errorf(`%w: run "sqlmcp login"`, machine.ErrOpen)
	}
	return &session{a: a, sealer: s, server: server, login: login}, nil
}

// client is the API of the logged-in server.
func (a *app) client() (*client.Client, *session, error) {
	s, err := a.openSession()
	if err != nil {
		return nil, nil, err
	}
	return client.New(s.server, a.opts.HTTP, s), s, nil
}

// save seals the login and writes it with its server.
func (s *session) save() error {
	// A struct of strings and a time always marshals.
	raw, _ := json.Marshal(s.login)
	return config.Save(s.a.opts.Getenv, config.File{Server: s.server, Login: s.sealer.Seal(raw, s.server)})
}

// Token is the access token, refreshed when it is about to expire.
func (s *session) Token(ctx context.Context) (string, error) {
	if s.login.AccessToken != "" && s.a.opts.Now().Add(refreshMargin).Before(s.login.Expiry) {
		return s.login.AccessToken, nil
	}
	return s.Refresh(ctx)
}

// Refresh trades the refresh token for new tokens and saves them.
func (s *session) Refresh(ctx context.Context) (string, error) {
	tok, err := s.a.oauth(s.server).Refresh(ctx, s.login.RefreshToken)
	var oauthErr *oauth.Error
	if errors.As(err, &oauthErr) {
		// Refused: only a new login helps.
		return "", fmt.Errorf("%w (%v)", errLoginExpired, err)
	}
	if err != nil {
		return "", fmt.Errorf("refresh the login: %w", err)
	}
	s.login = config.Login{AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken, Expiry: tok.Expiry, Databases: tok.Databases}
	if err := s.save(); err != nil {
		// This run still has a token; the next one asks for a login,
		// since the old refresh token was rotated away.
		fmt.Fprintf(s.a.opts.Err, "warning: could not save the refreshed login: %v\n", err)
	}
	return s.login.AccessToken, nil
}

// oauth is the OAuth client of server.
func (a *app) oauth(server string) *oauth.Client {
	return &oauth.Client{Server: server, HTTP: a.opts.HTTP, Now: a.opts.Now}
}
