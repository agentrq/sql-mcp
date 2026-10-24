// Package cmd is the sqlmcp commands. The database commands mirror the
// actions of the server's MCP `sql` tool (listTables, describeTable, query,
// markPii), so they must stay in step with it.
package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/agentrq/sql-mcp/cli/internal/client"
	"github.com/agentrq/sql-mcp/cli/internal/config"
	"github.com/agentrq/sql-mcp/cli/internal/machine"
)

// Options are what a command touches outside itself, passed in so tests run
// commands without a terminal, a browser or this machine's key.
type Options struct {
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	Getenv func(string) string
	Now    func() time.Time
	// OpenBrowser shows the consent page.
	OpenBrowser func(url string) error
	// MachineKey is the key that seals the login to this machine and user.
	MachineKey func() ([]byte, error)
	// HTTP calls the server; nil is a client with a timeout.
	HTTP    *http.Client
	Version string
}

// loginTimeout bounds the wait for the person in the browser.
const loginTimeout = 5 * time.Minute

// app carries the global flags to every command.
type app struct {
	opts   Options
	json   bool
	server string
}

// Execute runs the CLI with args (without the program name) and returns the
// exit code.
func Execute(args []string, opts Options) int {
	if opts.HTTP == nil {
		opts.HTTP = &http.Client{Timeout: 5 * time.Minute}
	}
	root := newRoot(&app{opts: opts})
	root.SetArgs(args)
	// Ctrl-C cancels the context: a login stops waiting and a query is
	// abandoned cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	fmt.Fprintf(opts.Err, "error: %s\n", err)
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
		fmt.Fprintln(opts.Err, `Run "sqlmcp login" to sign in again.`)
	}
	return 1
}

func newRoot(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:   "sqlmcp",
		Short: "Query your SQL MCP databases from the terminal",
		Long: `sqlmcp queries the databases you registered on a SQL MCP server, with the
same PII guard as the MCP tool: a query that touches a PII column is refused.

Sign in with "sqlmcp login --server URL", which opens the browser; there you
pick the databases this computer may open. Results come back to the terminal
and are never stored on tmphost.`,
		Version:       a.opts.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(a.opts.In)
	root.SetOut(a.opts.Out)
	root.SetErr(a.opts.Err)
	root.PersistentFlags().BoolVar(&a.json, "json", false, "print JSON")
	root.PersistentFlags().StringVar(&a.server, "server", "", "server URL (default: $"+config.EnvServer+" or the one logged in to)")
	root.AddCommand(newLoginCmd(a), newLogoutCmd(a), newWhoamiCmd(a), newDBCmd(a),
		newTablesCmd(a), newDescribeCmd(a), newQueryCmd(a), newPIICmd(a), newLogCmd(a))
	return root
}

var errNoServer = errors.New(`name the server: sqlmcp login --server https://sql.example.com`)

// serverURL picks the server: --server, then $SQLMCP_SERVER, then the one
// logged in to.
func (a *app) serverURL(f config.File) (string, error) {
	for _, s := range []string{a.server, a.opts.Getenv(config.EnvServer), f.Server} {
		if s != "" {
			return strings.TrimRight(s, "/"), nil
		}
	}
	return "", errNoServer
}

// sealer seals the login to this machine and user.
func (a *app) sealer() (*machine.Sealer, error) {
	key, err := a.opts.MachineKey()
	if err != nil {
		return nil, err
	}
	return machine.NewSealer(key)
}

// output prints v as JSON with --json, or lets human write text.
func (a *app) output(v any, human func(w io.Writer)) error {
	if a.json {
		enc := json.NewEncoder(a.opts.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	human(a.opts.Out)
	return nil
}

// table writes tab-separated rows as aligned columns.
func table(w io.Writer, header string, rows []string) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, header)
	for _, r := range rows {
		fmt.Fprintln(tw, r)
	}
	tw.Flush()
}

// database finds a granted database by ID or name.
func database(ctx context.Context, c *client.Client, ref string) (*client.Database, error) {
	dbs, err := c.Databases(ctx)
	if err != nil {
		return nil, err
	}
	var found []client.Database
	for _, d := range dbs {
		if d.ID == ref {
			return &d, nil
		}
		if strings.EqualFold(d.Name, ref) {
			found = append(found, d)
		}
	}
	switch len(found) {
	case 1:
		return &found[0], nil
	case 0:
		return nil, fmt.Errorf(`no database %q among those this login may open: "sqlmcp db ls" lists them, "sqlmcp login" picks again`, ref)
	}
	ids := make([]string, len(found))
	for i, d := range found {
		ids[i] = d.ID
	}
	return nil, fmt.Errorf("%d databases are named %q: use an ID (%s)", len(found), ref, strings.Join(ids, ", "))
}
