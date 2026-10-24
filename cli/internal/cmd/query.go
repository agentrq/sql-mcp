package cmd

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/agentrq/sql-mcp/cli/internal/client"
)

// The query output formats.
const (
	formatTable = "table"
	formatCSV   = "csv"
	formatJSON  = "json"
)

// query is the MCP tool's query, except that the whole result comes back:
// it is never cut to a preview or stored on tmphost.
func newQueryCmd(a *app) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "query <db> [sql]",
		Short: "Run one read-only SELECT",
		Long: `Run one read-only SELECT. Without sql, or with "-", it is read from stdin.

The server refuses a query that touches a PII column, uses SELECT * over a
table with PII, or writes, and says why. It returns at most its row limit
(QUERY_MAX_ROWS); every row comes back here, none is stored on tmphost.`,
		Example: `  sqlmcp query shop "select id, total from orders limit 10"
  sqlmcp query shop --format csv < report.sql > report.csv`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.json {
				format = formatJSON
			}
			if format != formatTable && format != formatCSV && format != formatJSON {
				return fmt.Errorf("unknown format %q: use table, csv or json", format)
			}
			sql := "-"
			if len(args) == 2 {
				sql = args[1]
			}
			if sql == "-" {
				raw, err := io.ReadAll(a.opts.In)
				if err != nil {
					return fmt.Errorf("read the query from stdin: %w", err)
				}
				sql = string(raw)
			}
			if strings.TrimSpace(sql) == "" {
				return errors.New("no query: give it as an argument or on stdin")
			}
			c, _, err := a.client()
			if err != nil {
				return err
			}
			db, err := database(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			res, err := c.Query(cmd.Context(), db.ID, sql)
			var apiErr *client.APIError
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnprocessableEntity {
				return fmt.Errorf("query rejected: %s", apiErr.Message)
			}
			if err != nil {
				return err
			}
			return a.printResult(res, format)
		},
	}
	cmd.Flags().StringVarP(&format, "format", "f", formatTable, "output format: table, csv or json")
	return cmd
}

// printResult writes the rows to stdout and how many to stderr, so the
// output can be piped as it is.
func (a *app) printResult(res *client.Result, format string) error {
	switch format {
	case formatJSON:
		enc := json.NewEncoder(a.opts.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	case formatCSV:
		w := csv.NewWriter(a.opts.Out)
		_ = w.Write(res.Columns)
		for _, r := range res.Rows {
			_ = w.Write(cells(r, ""))
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return err
		}
	default:
		rows := make([]string, len(res.Rows))
		flat := strings.NewReplacer("\t", " ", "\r\n", " ", "\n", " ", "\r", " ")
		for i, r := range res.Rows {
			cs := cells(r, "NULL")
			for j := range cs {
				cs[j] = flat.Replace(cs[j])
			}
			rows[i] = strings.Join(cs, "\t")
		}
		table(a.opts.Out, strings.Join(res.Columns, "\t"), rows)
	}
	fmt.Fprintf(a.opts.Err, "(%d %s, %d ms)\n", res.RowCount, plural(res.RowCount, "row"), res.DurationMs)
	if res.Truncated {
		fmt.Fprintf(a.opts.Err, "The server's row limit cut the result at %d rows; add a LIMIT or a WHERE to see the rest.\n", res.RowCount)
	}
	return nil
}

// cells renders a row's values as text, null as given.
func cells(row []any, null string) []string {
	out := make([]string, len(row))
	for i, v := range row {
		switch x := v.(type) {
		case nil:
			out[i] = null
		case string:
			out[i] = x
		case json.Number:
			out[i] = x.String()
		case bool:
			out[i] = strconv.FormatBool(x)
		default:
			// JSON objects and arrays, as compact JSON. Decoded JSON
			// always marshals again.
			raw, _ := json.Marshal(x)
			out[i] = string(raw)
		}
	}
	return out
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
