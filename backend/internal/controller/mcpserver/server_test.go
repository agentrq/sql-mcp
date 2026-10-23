package mcpserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentrq/sql-mcp/backend/internal/controller/crud"
	"github.com/agentrq/sql-mcp/backend/internal/controller/mcpserver"
	"github.com/agentrq/sql-mcp/backend/internal/data/entity"
	"github.com/agentrq/sql-mcp/backend/internal/data/model"
)

type backend struct {
	schemaErr error
	result    *entity.QueryResult
	queryErr  error
	last      crud.QueryRequest
}

func (b *backend) Schema(context.Context, int64, int64) ([]entity.SchemaTable, error) {
	if b.schemaErr != nil {
		return nil, b.schemaErr
	}
	return []entity.SchemaTable{
		{Schema: "public", Name: "users", Kind: "table", Columns: []entity.SchemaColumn{
			{Name: "id", DataType: "integer"}, {Name: "email", DataType: "text", PII: true},
		}},
		{Schema: "billing", Name: "users", Kind: "view", Columns: []entity.SchemaColumn{{Name: "plan", DataType: "text"}}},
	}, nil
}

func (b *backend) RunQuery(_ context.Context, rq crud.QueryRequest) (*entity.QueryResult, error) {
	b.last = rq
	return b.result, b.queryErr
}

var caller = mcpserver.Caller{UserID: 7, DatabaseID: 9, DatabaseName: "shop"}

func session(t *testing.T, b *backend) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	srv := mcpserver.NewServer(b, caller, "test")
	ss, err := srv.Connect(ctx, st, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil).Connect(ctx, ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, action string, payload map[string]any) (string, bool) {
	t.Helper()
	args := map[string]any{"action": action}
	if payload != nil {
		args["payload"] = payload
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "sql", Arguments: args})
	require.NoError(t, err)
	require.Len(t, res.Content, 1)
	return res.Content[0].(*mcp.TextContent).Text, res.IsError
}

func TestToolList(t *testing.T) {
	cs := session(t, &backend{})
	init := cs.InitializeResult()
	assert.Equal(t, "SQL MCP: shop", init.ServerInfo.Title)
	assert.Contains(t, init.Instructions, `"shop"`)
	tools, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, tools.Tools, 1)
	assert.Equal(t, "sql", tools.Tools[0].Name)
	assert.True(t, tools.Tools[0].Annotations.ReadOnlyHint)
}

func TestHelpAndUnknown(t *testing.T) {
	cs := session(t, &backend{})
	text, isErr := call(t, cs, "help", nil)
	assert.False(t, isErr)
	assert.Contains(t, text, "Actions:")
	text, _ = call(t, cs, "help", map[string]any{"topic": "query"})
	assert.Contains(t, text, "PII")
	_, isErr = call(t, cs, "help", map[string]any{"topic": "drop"})
	assert.True(t, isErr)
	_, isErr = call(t, cs, "help", map[string]any{"bogus": 1})
	assert.True(t, isErr)
	text, isErr = call(t, cs, "delete", nil)
	assert.True(t, isErr)
	assert.Contains(t, text, `unknown action "delete"`)
}

func TestListAndDescribe(t *testing.T) {
	b := &backend{}
	cs := session(t, b)
	text, isErr := call(t, cs, "listTables", nil)
	require.False(t, isErr)
	assert.JSONEq(t, `{"tables":[{"table":"public.users","kind":"table","columns":2,"piiColumns":["email"]},{"table":"billing.users","kind":"view","columns":1}]}`, text)

	text, isErr = call(t, cs, "describeTable", map[string]any{"table": "USERS", "schema": "billing"})
	require.False(t, isErr)
	assert.JSONEq(t, `{"table":"billing.users","kind":"view","columns":[{"name":"plan","dataType":"text"}]}`, text)
	text, _ = call(t, cs, "describeTable", map[string]any{"table": "users"})
	assert.Contains(t, text, `"pii":true`)

	for _, p := range []map[string]any{{}, {"table": "nope"}, {"table": 5}} {
		_, isErr = call(t, cs, "describeTable", p)
		assert.True(t, isErr, p)
	}

	b.schemaErr = errors.New("permission denied")
	text, isErr = call(t, cs, "listTables", nil)
	assert.True(t, isErr)
	assert.Equal(t, "permission denied", text)
	_, isErr = call(t, cs, "describeTable", map[string]any{"table": "users"})
	assert.True(t, isErr)
}

func TestQuery(t *testing.T) {
	b := &backend{}
	cs := session(t, b)

	_, isErr := call(t, cs, "query", map[string]any{"sql": "  "})
	assert.True(t, isErr)
	_, isErr = call(t, cs, "query", map[string]any{"sql": 1})
	assert.True(t, isErr)

	b.queryErr = &entity.RejectedError{Reason: "column users.email is PII"}
	text, isErr := call(t, cs, "query", map[string]any{"sql": "select email from users"})
	assert.True(t, isErr)
	assert.Equal(t, "Query rejected: column users.email is PII", text)

	b.queryErr = errors.New("timeout")
	text, _ = call(t, cs, "query", map[string]any{"sql": "select 1"})
	assert.Equal(t, "Query failed: timeout", text)

	b.queryErr = nil
	b.result = &entity.QueryResult{
		Columns: []string{"id"}, Rows: [][]any{{1}}, RowCount: 3, DurationMs: 4,
		ResultURL: "https://x.tmphost.io/r.csv", ResultExpiresAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		StoreError: "",
	}
	text, isErr = call(t, cs, "query", map[string]any{"sql": "select id from users", "store": true})
	require.False(t, isErr)
	assert.JSONEq(t, `{"columns":["id"],"rows":[[1]],"rowCount":3,"rowsShown":1,"truncated":false,"durationMs":4,
		"resultUrl":"https://x.tmphost.io/r.csv","resultExpiresAt":"2030-01-01T00:00:00Z"}`, text)
	assert.Equal(t, crud.QueryRequest{UserID: 7, DatabaseID: 9, SQL: "select id from users", Source: model.SourceMCP, Store: true}, b.last)

	b.result = &entity.QueryResult{Columns: []string{"id"}, Rows: [][]any{{1}}, RowCount: 1, StoreError: "tmphost is not connected"}
	text, _ = call(t, cs, "query", map[string]any{"sql": "select id from users"})
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, "tmphost is not connected", out["note"])
	assert.NotContains(t, out, "rowsShown")

	b.result = &entity.QueryResult{Columns: []string{"f"}, Rows: [][]any{{func() {}}}, RowCount: 1}
	_, isErr = call(t, cs, "query", map[string]any{"sql": "select f"})
	assert.True(t, isErr, "an unencodable result is an error, not a crash")
}

func TestHandlerNeedsACaller(t *testing.T) {
	h := mcpserver.NewHandler(&backend{}, "test")
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/mcp/x", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		return r
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req())
	assert.GreaterOrEqual(t, w.Code, 400, "no caller, no server")

	w = httptest.NewRecorder()
	r := req()
	h.ServeHTTP(w, r.WithContext(mcpserver.WithCaller(r.Context(), caller)))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "SQL MCP: shop")
}
