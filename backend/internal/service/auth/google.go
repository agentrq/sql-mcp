package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// User is who a sign-in provider says signed in.
type User struct {
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// Provider is a sign-in provider.
type Provider interface {
	AuthURL(state string) string
	Exchange(ctx context.Context, code string) (*User, error)
}

type googleProvider struct {
	config      *oauth2.Config
	userinfoURL string
}

// NewGoogle signs people in with Google.
func NewGoogle(clientID, clientSecret, redirectURL string) Provider {
	return &googleProvider{
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     google.Endpoint,
		},
		userinfoURL: "https://www.googleapis.com/oauth2/v2/userinfo",
	}
}

func (g *googleProvider) AuthURL(state string) string {
	return g.config.AuthCodeURL(state, oauth2.SetAuthURLParam("prompt", "select_account"))
}

func (g *googleProvider) Exchange(ctx context.Context, code string) (*User, error) {
	token, err := g.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("google: exchange: %w", err)
	}
	resp, err := g.config.Client(ctx, token).Get(g.userinfoURL)
	if err != nil {
		return nil, fmt.Errorf("google: userinfo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google: userinfo status %d", resp.StatusCode)
	}
	var u User
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, fmt.Errorf("google: decode userinfo: %w", err)
	}
	// The allowlist trusts the address, so it has to be one Google verified.
	if !u.VerifiedEmail {
		return nil, errors.New("google: email is not verified")
	}
	return &u, nil
}
