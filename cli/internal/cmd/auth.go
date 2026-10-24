package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/agentrq/sql-mcp/cli/internal/client"
	"github.com/agentrq/sql-mcp/cli/internal/config"
	"github.com/agentrq/sql-mcp/cli/internal/oauth"
)

func newLoginCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "login --server URL",
		Short: "Sign in with the browser and pick the databases",
		Long: `Sign in to a SQL MCP server in the browser. The consent page lists your
databases: tick the ones this computer may open. Logging in again picks again.

The tokens are saved in the config file sealed with a key derived from this
machine's ID and your OS user, so the file opens on no other machine or
account. They are refreshed on their own.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.login(cmd.Context())
		},
	}
}

func (a *app) login(ctx context.Context) error {
	f, err := config.Load(a.opts.Getenv)
	if err != nil {
		return err
	}
	server, err := a.serverURL(f)
	if err != nil {
		return err
	}
	sealer, err := a.sealer()
	if err != nil {
		return err
	}
	// The URL and progress go to stderr, so stdout stays clean for --json.
	tok, err := a.oauth(server).Login(ctx, oauth.LoginOptions{Open: a.opts.OpenBrowser, Out: a.opts.Err, Timeout: loginTimeout})
	if err != nil {
		return err
	}
	s := &session{a: a, sealer: sealer, server: server, login: config.Login{
		AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken, Expiry: tok.Expiry, Databases: tok.Databases,
	}}
	if err := s.save(); err != nil {
		return err
	}
	return a.whoami(ctx, client.New(server, a.opts.HTTP, s), server, "Logged in to")
}

type whoamiView struct {
	Server    string            `json:"server"`
	User      *client.User      `json:"user"`
	Databases []client.Database `json:"databases"`
}

func (a *app) whoami(ctx context.Context, c *client.Client, server, lead string) error {
	user, err := c.Me(ctx)
	if err != nil {
		return err
	}
	dbs, err := c.Databases(ctx)
	if err != nil {
		return err
	}
	return a.output(whoamiView{Server: server, User: user, Databases: dbs}, func(w io.Writer) {
		names := make([]string, len(dbs))
		for i, d := range dbs {
			names[i] = d.Name
		}
		fmt.Fprintf(w, "%s %s as %s\n", lead, server, user.Email)
		fmt.Fprintf(w, "Databases: %s\n", strings.Join(names, ", "))
	})
}

func newLogoutCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget the saved login",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			f, err := config.Load(a.opts.Getenv)
			if err != nil {
				return err
			}
			// The server stays, so the next login goes to the same place.
			if err := config.Save(a.opts.Getenv, config.File{Server: f.Server}); err != nil {
				return err
			}
			return a.output(map[string]bool{"loggedOut": true}, func(w io.Writer) {
				fmt.Fprintln(w, "Logged out.")
			})
		},
	}
}

func newWhoamiCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show who is logged in and the databases this login opens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, s, err := a.client()
			if err != nil {
				return err
			}
			return a.whoami(cmd.Context(), c, s.server, "Logged in to")
		},
	}
}
