package crud

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/agentrq/sql-mcp/backend/internal/data/entity"
	"github.com/agentrq/sql-mcp/backend/internal/data/model"
	"github.com/agentrq/sql-mcp/backend/internal/service/sqlguard"
)

// QueryRequest is one query to run.
type QueryRequest struct {
	UserID     int64
	DatabaseID int64
	SQL        string
	Source     string // model.SourceMCP or model.SourceConsole
	// Store asks for the result on tmphost even when it fits inline.
	Store bool
}

// RunQuery checks a query against the database's PII columns, runs it
// read-only, and records it either way. An MCP result larger than the inline
// limit is stored whole on tmphost when the owner connected it, and cut to
// the limit inline.
func (c *Controller) RunQuery(ctx context.Context, rq QueryRequest) (*entity.QueryResult, error) {
	row, d, dsn, err := c.open(ctx, rq.UserID, rq.DatabaseID)
	if err != nil {
		return nil, err
	}
	logEntry := &model.QueryLog{DatabaseID: row.ID, UserID: rq.UserID, Source: rq.Source, SQL: truncate(rq.SQL, 20000)}

	pii, err := c.repo.ListPIIColumns(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	cols := make([]sqlguard.Column, len(pii))
	for i, p := range pii {
		cols[i] = sqlguard.Column{Schema: p.Schema, Table: p.Table, Column: p.Column}
	}
	if err := d.Guard(cols).Check(rq.SQL); err != nil {
		logEntry.Status, logEntry.Reason = model.QueryRejected, err.Error()
		c.record(ctx, logEntry)
		return nil, &entity.RejectedError{Reason: err.Error()}
	}

	start := c.now()
	setup := c.live().setup
	res, err := d.Query(ctx, dsn, rq.SQL, setup.QueryMaxRows, setup.QueryTimeout)
	logEntry.DurationMs = c.now().Sub(start).Milliseconds()
	if err != nil {
		logEntry.Status, logEntry.Reason = model.QueryError, err.Error()
		c.record(ctx, logEntry)
		return nil, &entity.QueryFailedError{Message: err.Error()}
	}

	out := &entity.QueryResult{
		Columns:    res.Columns,
		Rows:       res.Rows,
		RowCount:   len(res.Rows),
		Truncated:  res.Truncated,
		DurationMs: logEntry.DurationMs,
	}
	if rq.Source == model.SourceMCP && (rq.Store || len(res.Rows) > setup.QueryInlineRows) {
		url, expiresAt, err := c.storeResult(ctx, rq.UserID, row.Name, res.Columns, res.Rows)
		switch err {
		case nil:
			out.ResultURL, out.ResultExpiresAt = url, expiresAt
		case errTmphostNotConnected:
			out.StoreError = "tmphost is not connected; connect it in SQL MCP settings to receive every row"
		default:
			log.Warn().Err(err).Msg("could not store a query result on tmphost")
			out.StoreError = "could not store the result on tmphost: " + err.Error()
		}
		if len(out.Rows) > setup.QueryInlineRows {
			out.Rows = out.Rows[:setup.QueryInlineRows]
		}
	}

	logEntry.Status, logEntry.RowCount, logEntry.ResultURL = model.QueryOK, out.RowCount, out.ResultURL
	c.record(ctx, logEntry)
	return out, nil
}

// record writes the query log and tells whoever watches the database. A
// failure to log does not fail the query the person already has, so it is
// only reported.
func (c *Controller) record(ctx context.Context, q *model.QueryLog) {
	if err := c.repo.AddQueryLog(context.WithoutCancel(ctx), q); err != nil {
		log.Error().Err(err).Msg("could not record a query")
		return
	}
	c.queries.Publish(q.DatabaseID, queryLogEntity(q))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ListQueries returns a database's recent queries.
func (c *Controller) ListQueries(ctx context.Context, userID, id int64, limit int) ([]entity.QueryLog, error) {
	if _, err := c.repo.GetDatabase(ctx, userID, id); err != nil {
		return nil, mapErr(err)
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := c.repo.ListQueryLogs(ctx, id, limit)
	if err != nil {
		return nil, err
	}
	out := make([]entity.QueryLog, 0, len(rows))
	for i := range rows {
		out = append(out, queryLogEntity(&rows[i]))
	}
	return out, nil
}

// WatchQueries streams a database's queries as they are recorded. The
// channel is closed when stop is called or the database is deleted; a
// watcher that falls behind misses queries rather than slowing them down.
func (c *Controller) WatchQueries(ctx context.Context, userID, id int64) (queries <-chan entity.QueryLog, stop func(), err error) {
	if _, err := c.repo.GetDatabase(ctx, userID, id); err != nil {
		return nil, nil, mapErr(err)
	}
	queries, stop = c.queries.Subscribe(id, 64)
	return queries, stop, nil
}

func queryLogEntity(q *model.QueryLog) entity.QueryLog {
	return entity.QueryLog{
		ID: FormatID(q.ID), Source: q.Source, SQL: q.SQL, Status: q.Status, Reason: q.Reason,
		RowCount: q.RowCount, ResultURL: q.ResultURL, DurationMs: q.DurationMs, CreatedAt: q.CreatedAt,
	}
}

// DatabaseOwner is who owns a database, its name and its engine.
type DatabaseOwner struct {
	UserID int64
	Name   string
	Driver string
}

// SystemDatabaseOwner returns who owns a database, for the MCP server to
// check a token against before anything else.
func (c *Controller) SystemDatabaseOwner(ctx context.Context, id int64) (DatabaseOwner, error) {
	row, err := c.repo.SystemGetDatabase(ctx, id)
	if err != nil {
		return DatabaseOwner{}, mapErr(err)
	}
	return DatabaseOwner{UserID: row.UserID, Name: row.Name, Driver: row.Driver}, nil
}

// ToCSV writes a result as CSV, the header first.
func ToCSV(columns []string, rows [][]any) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(columns); err != nil {
		return nil, err
	}
	record := make([]string, len(columns))
	for _, r := range rows {
		for i := range record {
			record[i] = ""
			if i < len(r) {
				record[i] = cell(r[i])
			}
		}
		if err := w.Write(record); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

func cell(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.RawMessage:
		return string(x)
	case time.Time:
		return x.Format(time.RFC3339Nano)
	}
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return fmt.Sprint(v)
}
