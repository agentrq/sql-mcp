// Package mcpserver is the MCP server each database gets: one tool, `sql`,
// taking an action and its payload like every AgentRQ tool.
//
// The transport is stateless. Every request has already been authenticated
// by the handler, which names the caller and the database in the request's
// context, and a server is built for that pair alone, so nothing one
// database's caller does can reach another database.
package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentrq/sql-mcp/backend/internal/controller/crud"
	"github.com/agentrq/sql-mcp/backend/internal/data/entity"
	"github.com/agentrq/sql-mcp/backend/internal/data/model"
	"github.com/agentrq/sql-mcp/backend/internal/service/toon"
)

// Backend is the part of the controller the tool calls.
type Backend interface {
	Schema(ctx context.Context, userID, id int64) ([]entity.SchemaTable, error)
	RunQuery(ctx context.Context, rq crud.QueryRequest) (*entity.QueryResult, error)
}

// Caller is who a request is from and which database it is for.
type Caller struct {
	UserID       int64
	DatabaseID   int64
	DatabaseName string
}

type callerKey struct{}

// WithCaller names the caller of the request in its context.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// NewHandler serves the MCP protocol for whichever database the request's
// context names; a request without a caller is refused.
func NewHandler(backend Backend, version string) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		caller, ok := r.Context().Value(callerKey{}).(Caller)
		if !ok {
			return nil
		}
		return NewServer(backend, caller, version)
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}

// ToolDescription is the `sql` tool's description.
const ToolDescription = "Read-only SQL on this database. Takes {action, payload}. " +
	"Actions: listTables (every table and view, and which columns are PII), " +
	"describeTable {table, schema?} (columns and types), " +
	"query {sql, store?} (one SELECT; PII columns, SELECT * over a table with PII, and writes are rejected), " +
	"help {topic?}. Answers are TOON: a table names its columns once, then one line per row. Start with listTables."

type input struct {
	Action  string         `json:"action" jsonschema:"listTables, describeTable, query or help"`
	Payload map[string]any `json:"payload,omitempty" jsonschema:"the action's fields; help prints them"`
}

// NewServer is the MCP server for one caller and database.
func NewServer(backend Backend, caller Caller, version string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "sql-mcp", Title: "SQL MCP: " + caller.DatabaseName, Version: version}, &mcp.ServerOptions{
		Instructions: fmt.Sprintf("SQL MCP for the database %q. Use the `sql` tool: call listTables first, then query with explicit column lists. "+
			"Columns marked PII cannot be selected, filtered, sorted or joined on; a query that touches one is rejected with the reason. "+
			"Large results come back cut to a preview with a temporary tmphost.io URL holding every row. "+
			"Answers are TOON: `key: value` lines, and tables as `name[rows]{col,col}:` followed by one comma-separated line per row.", caller.DatabaseName),
	})
	t := &tool{backend: backend, caller: caller}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "sql",
		Title:       "SQL",
		Description: ToolDescription,
		Annotations: &mcp.ToolAnnotations{Title: "Query " + caller.DatabaseName, ReadOnlyHint: true, IdempotentHint: true},
	}, t.handle)
	return srv
}

type tool struct {
	backend Backend
	caller  Caller
}

type describePayload struct {
	Table  string `json:"table"`
	Schema string `json:"schema,omitempty"`
}

type queryPayload struct {
	SQL   string `json:"sql"`
	Store bool   `json:"store,omitempty"`
}

type helpPayload struct {
	Topic string `json:"topic,omitempty"`
}

var help = map[string]string{
	"":              "Usage: sql {\"action\": \"<action>\", \"payload\": {...}}\n\nActions:\n  listTables     Every table and view, with its PII columns.\n  describeTable  {\"table\": \"users\", \"schema\": \"public\"} columns, types and PII flags.\n  query          {\"sql\": \"select id from users limit 10\", \"store\": false} one read-only SELECT.\n  help           {\"topic\": \"<action>\"}",
	"listTables":    "listTables takes no payload. It returns a tables table (schema.name, kind, column count) and a pii table of the columns no query may read.",
	"describeTable": "describeTable {\"table\": \"<name>\", \"schema\": \"<schema, optional>\"} returns a columns table: each column's name, type and whether it is PII.",
	"query": "query {\"sql\": \"<one SELECT>\", \"store\": <bool, optional>} runs the query read-only. Refused: any statement but SELECT, more than one statement, " +
		"a PII column anywhere (select list, WHERE, JOIN, ORDER BY), SELECT * or t.* or whole-row references over a table with PII, and functions that run SQL from text. " +
		"The first rows come back inline as a TOON rows table after rowCount and the other totals; a larger result (or store: true) is stored whole as CSV on the owner's tmphost.io and its URL returned.",
}

func (t *tool) handle(ctx context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, any, error) {
	switch in.Action {
	case "help":
		var p helpPayload
		if err := decode(in.Payload, &p); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		text, ok := help[p.Topic]
		if !ok {
			return errorResult("there is no action " + p.Topic + "; use listTables, describeTable, query or help"), nil, nil
		}
		return textResult(text), nil, nil
	case "listTables":
		return t.listTables(ctx)
	case "describeTable":
		var p describePayload
		if err := decode(in.Payload, &p); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return t.describeTable(ctx, p)
	case "query":
		var p queryPayload
		if err := decode(in.Payload, &p); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return t.query(ctx, p)
	default:
		return errorResult(fmt.Sprintf("unknown action %q: use listTables, describeTable, query or help", in.Action)), nil, nil
	}
}

func (t *tool) schema(ctx context.Context) ([]entity.SchemaTable, error) {
	return t.backend.Schema(ctx, t.caller.UserID, t.caller.DatabaseID)
}

func (t *tool) listTables(ctx context.Context) (*mcp.CallToolResult, any, error) {
	tables, err := t.schema(ctx)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	list := toon.Table{Columns: []string{"table", "kind", "columns"}}
	pii := toon.Table{Columns: []string{"table", "column"}}
	for _, tb := range tables {
		name := tb.Schema + "." + tb.Name
		list.Rows = append(list.Rows, []any{name, tb.Kind, len(tb.Columns)})
		for _, c := range tb.Columns {
			if c.PII {
				pii.Rows = append(pii.Rows, []any{name, c.Name})
			}
		}
	}
	return toonResult(toon.Object{{Key: "tables", Value: list}, {Key: "pii", Value: pii}})
}

func (t *tool) describeTable(ctx context.Context, p describePayload) (*mcp.CallToolResult, any, error) {
	if p.Table == "" {
		return errorResult("describeTable needs a table"), nil, nil
	}
	tables, err := t.schema(ctx)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	for _, tb := range tables {
		if !strings.EqualFold(tb.Name, p.Table) || (p.Schema != "" && !strings.EqualFold(tb.Schema, p.Schema)) {
			continue
		}
		cols := toon.Table{Columns: []string{"name", "type", "pii"}}
		for _, c := range tb.Columns {
			cols.Rows = append(cols.Rows, []any{c.Name, c.DataType, c.PII})
		}
		return toonResult(toon.Object{
			{Key: "table", Value: tb.Schema + "." + tb.Name}, {Key: "kind", Value: tb.Kind}, {Key: "columns", Value: cols},
		})
	}
	return errorResult("there is no table " + p.Table + "; listTables lists them"), nil, nil
}

func (t *tool) query(ctx context.Context, p queryPayload) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(p.SQL) == "" {
		return errorResult("query needs sql"), nil, nil
	}
	res, err := t.backend.RunQuery(ctx, crud.QueryRequest{
		UserID: t.caller.UserID, DatabaseID: t.caller.DatabaseID, SQL: p.SQL, Source: model.SourceMCP, Store: p.Store,
	})
	var rejected *entity.RejectedError
	switch {
	case errors.As(err, &rejected):
		return errorResult("Query rejected: " + rejected.Reason), nil, nil
	case err != nil:
		return errorResult("Query failed: " + err.Error()), nil, nil
	}
	// The totals come first, so a reader knows what it is looking at before
	// the rows.
	out := toon.Object{{Key: "rowCount", Value: res.RowCount}}
	if len(res.Rows) < res.RowCount {
		out = append(out, toon.Field{Key: "rowsShown", Value: len(res.Rows)})
	}
	out = append(out, toon.Field{Key: "truncated", Value: res.Truncated}, toon.Field{Key: "durationMs", Value: res.DurationMs})
	if res.ResultURL != "" {
		out = append(out, toon.Field{Key: "resultUrl", Value: res.ResultURL},
			toon.Field{Key: "resultExpiresAt", Value: res.ResultExpiresAt.Format(time.RFC3339)})
	}
	if res.StoreError != "" {
		out = append(out, toon.Field{Key: "note", Value: res.StoreError})
	}
	out = append(out, toon.Field{Key: "rows", Value: toon.Table{Columns: res.Columns, Rows: res.Rows}})
	return toonResult(out)
}

func decode(payload map[string]any, out any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("bad payload: %s", strings.TrimPrefix(err.Error(), "json: "))
	}
	return nil
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func errorResult(text string) *mcp.CallToolResult {
	r := textResult(toon.Marshal(toon.Object{{Key: "error", Value: text}}))
	r.IsError = true
	return r
}

func toonResult(o toon.Object) (*mcp.CallToolResult, any, error) {
	return textResult(toon.Marshal(o)), nil, nil
}
