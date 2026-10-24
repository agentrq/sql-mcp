package cmd

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/agentrq/sql-mcp/cli/internal/client"
)

func newDBCmd(a *app) *cobra.Command {
	db := &cobra.Command{
		Use:   "db",
		Short: "The databases this login opens",
	}
	db.AddCommand(&cobra.Command{
		Use:   "ls",
		Short: "List the databases this login opens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, _, err := a.client()
			if err != nil {
				return err
			}
			dbs, err := c.Databases(cmd.Context())
			if err != nil {
				return err
			}
			return a.output(map[string]any{"databases": dbs}, func(w io.Writer) {
				rows := make([]string, len(dbs))
				for i, d := range dbs {
					rows[i] = strings.Join([]string{d.Name, d.ID, d.Driver, strconv.Itoa(d.PIIColumnCount), d.DSNMasked}, "\t")
				}
				table(w, "NAME\tID\tENGINE\tPII\tDSN", rows)
			})
		},
	})
	return db
}

// listTables is the MCP tool's listTables.
func newTablesCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "tables <db>",
		Short: "List a database's tables and views, with their PII columns",
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
			return a.output(map[string]any{"tables": tables}, func(w io.Writer) {
				rows := make([]string, len(tables))
				for i, t := range tables {
					var pii []string
					for _, col := range t.Columns {
						if col.PII {
							pii = append(pii, col.Name)
						}
					}
					rows[i] = strings.Join([]string{t.Schema + "." + t.Name, t.Kind, strconv.Itoa(len(t.Columns)), strings.Join(pii, ", ")}, "\t")
				}
				table(w, "TABLE\tKIND\tCOLUMNS\tPII", rows)
			})
		},
	}
}

// describeTable is the MCP tool's describeTable.
func newDescribeCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "describe <db> <[schema.]table>",
		Short: "Show a table's columns, their types and which are PII",
		Args:  cobra.ExactArgs(2),
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
			schema, name, _ := cutLast(args[1])
			t, err := findTable(tables, schema, name)
			if err != nil {
				return err
			}
			return a.output(t, func(w io.Writer) {
				rows := make([]string, len(t.Columns))
				for i, col := range t.Columns {
					pii := ""
					switch {
					case col.PII:
						pii = "yes"
					case col.SuggestedPII:
						pii = "suggested"
					}
					rows[i] = strings.Join([]string{col.Name, col.DataType, pii}, "\t")
				}
				fmt.Fprintf(w, "%s.%s (%s)\n", t.Schema, t.Name, t.Kind)
				table(w, "COLUMN\tTYPE\tPII", rows)
			})
		},
	}
}

// cutLast splits "a.b" at its last dot; a name without one is all after.
func cutLast(s string) (before, after string, found bool) {
	i := strings.LastIndex(s, ".")
	if i < 0 {
		return "", s, false
	}
	return s[:i], s[i+1:], true
}

// findTable finds a table by name, in schema when one is given. Like the
// MCP tool, names match regardless of case.
func findTable(tables []client.Table, schema, name string) (*client.Table, error) {
	var found []*client.Table
	for i, t := range tables {
		if strings.EqualFold(t.Name, name) && (schema == "" || strings.EqualFold(t.Schema, schema)) {
			found = append(found, &tables[i])
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return nil, fmt.Errorf(`there is no table %q: "sqlmcp tables" lists them`, name)
	}
	return nil, fmt.Errorf("%s is in more than one schema: name it as schema.%s", name, name)
}
