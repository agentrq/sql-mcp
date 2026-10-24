package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/agentrq/sql-mcp/cli/internal/client"
)

// The CLI can mark PII columns and never unmark one: the server refuses it
// any way to, so a login cannot lift its own guard. Unmarking is the web
// app's.
func newPIICmd(a *app) *cobra.Command {
	pii := &cobra.Command{
		Use:   "pii",
		Short: "List and mark PII columns (the CLI can't unmark one)",
	}
	pii.AddCommand(newPIILsCmd(a), newPIIAddCmd(a), newPIISuggestCmd(a))
	return pii
}

func newPIILsCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "ls <db>",
		Short: "List a database's PII columns",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := a.client()
			if err != nil {
				return err
			}
			db, err := database(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			cols, err := c.PII(cmd.Context(), db.ID)
			if err != nil {
				return err
			}
			return a.output(map[string]any{"piiColumns": cols}, func(w io.Writer) {
				for _, p := range cols {
					fmt.Fprintf(w, "%s.%s.%s\n", p.Schema, p.Table, p.Column)
				}
			})
		},
	}
}

// markPii is the MCP tool's markPii, for any number of tables at once.
func newPIIAddCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "add <db> <[schema.]table.column>...",
		Short: "Mark columns as PII, so no query can read them",
		Long: `Mark columns as PII, so no query can read them from then on. Every column
must exist. A mark can't be removed from the CLI: only in the web app.`,
		Example: `  sqlmcp pii add shop users.email public.users.phone`,
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := a.client()
			if err != nil {
				return err
			}
			db, err := database(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			tables, err := c.Schema(cmd.Context(), db.ID)
			if err != nil {
				return err
			}
			var add []client.PIIColumn
			var already []string
			for _, ref := range args[1:] {
				col, t, err := findColumn(tables, ref)
				if err != nil {
					return err
				}
				name := t.Schema + "." + t.Name + "." + col.Name
				if col.PII {
					already = append(already, name)
					continue
				}
				add = append(add, client.PIIColumn{Schema: t.Schema, Table: t.Name, Column: col.Name})
			}
			cols, err := c.AddPII(cmd.Context(), db.ID, add)
			if err != nil {
				return err
			}
			marked := make([]string, len(add))
			for i, p := range add {
				marked[i] = p.Schema + "." + p.Table + "." + p.Column
			}
			return a.output(map[string]any{"marked": marked, "already": already, "piiColumns": cols}, func(w io.Writer) {
				for _, m := range marked {
					fmt.Fprintf(w, "marked   %s\n", m)
				}
				for _, m := range already {
					fmt.Fprintf(w, "already  %s\n", m)
				}
			})
		},
	}
}

// findColumn resolves [schema.]table.column against the schema.
func findColumn(tables []client.Table, ref string) (*client.Column, *client.Table, error) {
	rest, column, ok := cutLast(ref)
	if !ok || rest == "" || column == "" {
		return nil, nil, fmt.Errorf("%q is not a column: write [schema.]table.column", ref)
	}
	schema, name, _ := cutLast(rest)
	t, err := findTable(tables, schema, name)
	if err != nil {
		return nil, nil, err
	}
	for i, col := range t.Columns {
		if strings.EqualFold(col.Name, column) {
			return &t.Columns[i], t, nil
		}
	}
	return nil, nil, fmt.Errorf(`%s.%s has no column %q: "sqlmcp describe" lists them`, t.Schema, t.Name, column)
}

func newPIISuggestCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "suggest <db>",
		Short: "List columns whose names look like PII but aren't marked",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := a.client()
			if err != nil {
				return err
			}
			db, err := database(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			tables, err := c.Schema(cmd.Context(), db.ID)
			if err != nil {
				return err
			}
			suggested := []string{}
			for _, t := range tables {
				for _, col := range t.Columns {
					if col.SuggestedPII && !col.PII {
						suggested = append(suggested, t.Schema+"."+t.Name+"."+col.Name)
					}
				}
			}
			return a.output(map[string]any{"suggested": suggested}, func(w io.Writer) {
				if len(suggested) == 0 {
					fmt.Fprintln(w, "No unmarked column looks like PII by its name.")
					return
				}
				for _, s := range suggested {
					fmt.Fprintln(w, s)
				}
				fmt.Fprintf(w, "\nTo mark them: sqlmcp pii add %s %s\n", args[0], strings.Join(suggested, " "))
			})
		},
	}
}
