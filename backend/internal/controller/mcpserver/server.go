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
	"help {topic?}. Start with listTables."

type input struct {
	Action  string         `json:"action" jsonschema:"listTables, describeTable, query or help"`
	Payload map[string]any `json:"payload,omitempty" jsonschema:"the action's fields; help prints them"`
}

// NewServer is the MCP server for one caller and database.
func NewServer(backend Backend, caller Caller, version string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "sql-mcp", Title: "SQL MCP: " + caller.DatabaseName, Version: version}, &mcp.ServerOptions{
		Instructions: fmt.Sprintf("SQL MCP for the database %q. Use the `sql` tool: call listTables first, then query with explicit column lists. "+
			"Columns marked PII cannot be selected, filtered, sorted or joined on; a query that touches one is rejected with the reason. "+
			"Large results come back cut to a preview with a temporary tmphost.io URL holding every row.", caller.DatabaseName),
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
	"listTables":    "listTables takes no payload. It returns every table and view as schema.name, its kind, and the columns marked PII, which no query may read.",
	"describeTable": "describeTable {\"table\": \"<name>\", \"schema\": \"<schema, optional>\"} returns each column's name, type and whether it is PII.",
	"query": "query {\"sql\": \"<one SELECT>\", \"store\": <bool, optional>} runs the query read-only. Refused: any statement but SELECT, more than one statement, " +
		"a PII column anywhere (select list, WHERE, JOIN, ORDER BY), SELECT * or t.* or whole-row references over a table with PII, and functions that run SQL from text. " +
		"A preview of the first rows comes back inline; a larger result (or store: true) is stored whole as CSV on the owner's tmphost.io and its URL returned.",
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
	type row struct {
		Table      string   `json:"table"`
		Kind       string   `json:"kind"`
		Columns    int      `json:"columns"`
		PIIColumns []string `json:"piiColumns,omitempty"`
	}
	out := make([]row, 0, len(tables))
	for _, tb := range tables {
		r := row{Table: tb.Schema + "." + tb.Name, Kind: tb.Kind, Columns: len(tb.Columns)}
		for _, c := range tb.Columns {
			if c.PII {
				r.PIIColumns = append(r.PIIColumns, c.Name)
			}
		}
		out = append(out, r)
	}
	return jsonResult(map[string]any{"tables": out})
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
		type col struct {
			Name     string `json:"name"`
			DataType string `json:"dataType"`
			PII      bool   `json:"pii,omitempty"`
		}
		cols := make([]col, 0, len(tb.Columns))
		for _, c := range tb.Columns {
			cols = append(cols, col{Name: c.Name, DataType: c.DataType, PII: c.PII})
		}
		return jsonResult(map[string]any{"table": tb.Schema + "." + tb.Name, "kind": tb.Kind, "columns": cols})
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
	out := map[string]any{
		"columns":    res.Columns,
		"rows":       res.Rows,
		"rowCount":   res.RowCount,
		"truncated":  res.Truncated,
		"durationMs": res.DurationMs,
	}
	if len(res.Rows) < res.RowCount {
		out["rowsShown"] = len(res.Rows)
	}
	if res.ResultURL != "" {
		out["resultUrl"] = res.ResultURL
		out["resultExpiresAt"] = res.ResultExpiresAt.Format(time.RFC3339)
	}
	if res.StoreError != "" {
		out["note"] = res.StoreError
	}
	return jsonResult(out)
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
	r := textResult(text)
	r.IsError = true
	return r
}

func jsonResult(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return textResult(string(b)), nil, nil
}
