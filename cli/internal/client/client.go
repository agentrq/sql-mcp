// Package client calls a SQL MCP server's REST API (/api/v1) with the CLI's
// token. The server lets that token read the databases it was granted,
// query them and add PII columns, and nothing else.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// BasePath is where the API lives on the server.
const BasePath = "/api/v1"

// TokenSource hands out the CLI's access token.
type TokenSource interface {
	// Token is the current access token, refreshed when it is about to
	// expire.
	Token(ctx context.Context) (string, error)
	// Refresh gets a new one after the server refused the current one.
	Refresh(ctx context.Context) (string, error)
}

// APIError is an error answer from the API.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return e.Message }

// Client is the API of one server.
type Client struct {
	server string
	http   *http.Client
	tokens TokenSource
}

// New returns a client for server.
func New(server string, httpClient *http.Client, tokens TokenSource) *Client {
	return &Client{server: server, http: httpClient, tokens: tokens}
}

// User is the signed-in person.
type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// Database is a database the CLI was granted.
type Database struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Driver         string    `json:"driver"`
	DSNMasked      string    `json:"dsnMasked"`
	PIIColumnCount int       `json:"piiColumnCount"`
	AgentMarksPII  bool      `json:"agentMarksPii"`
	MCPURL         string    `json:"mcpUrl"`
	CreatedAt      time.Time `json:"createdAt"`
}

// Column is a column of a table, and whether it is or looks like PII.
type Column struct {
	Name         string `json:"name"`
	DataType     string `json:"dataType"`
	PII          bool   `json:"pii"`
	SuggestedPII bool   `json:"suggestedPii"`
}

// Table is a table or view.
type Table struct {
	Schema  string   `json:"schema"`
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Columns []Column `json:"columns"`
}

// PIIColumn is a column no query may read.
type PIIColumn struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	Column string `json:"column"`
}

// Result is what a query returned. Numbers stay json.Number, so a big
// integer or a decimal prints as the database sent it.
type Result struct {
	Columns    []string `json:"columns"`
	Rows       [][]any  `json:"rows"`
	RowCount   int      `json:"rowCount"`
	Truncated  bool     `json:"truncated"`
	DurationMs int64    `json:"durationMs"`
}

// QueryLog is one query someone ran on a database, allowed or not.
type QueryLog struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	SQL        string    `json:"sql"`
	Status     string    `json:"status"`
	Reason     string    `json:"reason"`
	RowCount   int       `json:"rowCount"`
	DurationMs int64     `json:"durationMs"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Me is the signed-in person.
func (c *Client) Me(ctx context.Context) (*User, error) {
	var u User
	return &u, c.do(ctx, http.MethodGet, "/auth/user", nil, &u)
}

// Databases lists the databases the CLI was granted.
func (c *Client) Databases(ctx context.Context) ([]Database, error) {
	var out struct {
		Databases []Database `json:"databases"`
	}
	return out.Databases, c.do(ctx, http.MethodGet, "/databases", nil, &out)
}

// Schema lists a database's tables and columns.
func (c *Client) Schema(ctx context.Context, id string) ([]Table, error) {
	var out struct {
		Tables []Table `json:"tables"`
	}
	return out.Tables, c.do(ctx, http.MethodGet, dbPath(id, "/schema"), nil, &out)
}

// PII lists a database's PII columns.
func (c *Client) PII(ctx context.Context, id string) ([]PIIColumn, error) {
	var out struct {
		PIIColumns []PIIColumn `json:"piiColumns"`
	}
	return out.PIIColumns, c.do(ctx, http.MethodGet, dbPath(id, "/pii"), nil, &out)
}

// AddPII marks columns as PII and returns the whole set. There is no way to
// unmark one from the CLI.
func (c *Client) AddPII(ctx context.Context, id string, columns []PIIColumn) ([]PIIColumn, error) {
	var out struct {
		PIIColumns []PIIColumn `json:"piiColumns"`
	}
	return out.PIIColumns, c.do(ctx, http.MethodPost, dbPath(id, "/pii"), map[string]any{"piiColumns": columns}, &out)
}

// Query runs one read-only SELECT.
func (c *Client) Query(ctx context.Context, id, sql string) (*Result, error) {
	var out Result
	return &out, c.do(ctx, http.MethodPost, dbPath(id, "/query"), map[string]string{"sql": sql}, &out)
}

// Queries lists a database's latest queries, newest first.
func (c *Client) Queries(ctx context.Context, id string, limit int) ([]QueryLog, error) {
	var out struct {
		Queries []QueryLog `json:"queries"`
	}
	return out.Queries, c.do(ctx, http.MethodGet, dbPath(id, "/queries?limit="+strconv.Itoa(limit)), nil, &out)
}

func dbPath(id, rest string) string {
	return "/databases/" + url.PathEscape(id) + rest
}

// do sends a request with the access token, and once more with a fresh one
// when the server refuses it.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	if in != nil {
		// Maps and slices of strings always marshal.
		body, _ = json.Marshal(in)
	}
	resp, err := c.send(ctx, method, path, body, c.tokens.Token)
	if err == nil && resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		resp, err = c.send(ctx, method, path, body, c.tokens.Refresh)
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return decodeError(resp)
	}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("read the answer to %s %s: %w", method, path, err)
	}
	return nil
}

func (c *Client) send(ctx context.Context, method, path string, body []byte, token func(context.Context) (string, error)) (*http.Response, error) {
	tok, err := token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.server+BasePath+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

// decodeError reads the API's {"error": {"message"}}, or says what came back
// when it is something else (a proxy's page).
func decodeError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	msg := resp.Status
	if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
		msg = e.Error.Message
	}
	return &APIError{Status: resp.StatusCode, Message: msg}
}
