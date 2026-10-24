package cmd

import (
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newLogCmd(a *app) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "log <db>",
		Short: "Show a database's latest queries, from every source",
		Long: `Show a database's latest queries, newest first: from MCP agents, the web
console and the CLI, whether they ran, were rejected or failed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := a.client()
			if err != nil {
				return err
			}
			db, err := database(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			logs, err := c.Queries(cmd.Context(), db.ID, limit)
			if err != nil {
				return err
			}
			return a.output(map[string]any{"queries": logs}, func(w io.Writer) {
				rows := make([]string, len(logs))
				for i, q := range logs {
					outcome := strconv.Itoa(q.RowCount) + " " + plural(q.RowCount, "row")
					if q.Status != "ok" {
						outcome = q.Reason
					}
					sql := strings.Join(strings.Fields(q.SQL), " ")
					rows[i] = strings.Join([]string{q.CreatedAt.Local().Format(time.DateTime), q.Source, q.Status, sql, outcome}, "\t")
				}
				table(w, "TIME\tSOURCE\tSTATUS\tSQL\tRESULT", rows)
			})
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "how many queries, at most 200")
	return cmd
}
