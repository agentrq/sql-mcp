// Package testutil builds a controller on a throwaway SQLite database with a
// fake database engine and a fake tmphost, for the tests of the packages
// above the controller.
package testutil

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentrq/sql-mcp/backend/internal/controller/crud"
	"github.com/agentrq/sql-mcp/backend/internal/repository"
	"github.com/agentrq/sql-mcp/backend/internal/service/auth"
	"github.com/agentrq/sql-mcp/backend/internal/service/dbdriver"
	"github.com/agentrq/sql-mcp/backend/internal/service/security"
	"github.com/agentrq/sql-mcp/backend/internal/service/sqlguard"
	"github.com/agentrq/sql-mcp/backend/internal/service/tmphost"
)

// GoodDSN is the only DSN the fake engine connects with.
const GoodDSN = "postgres://app:secret@db.internal:5432/shop"

// Driver is a fake engine with a users table whose email column looks
// like PII. Query returns Rows rows, or QueryErr.
type Driver struct {
	Rows      int
	QueryErr  error
	SchemaErr error
}

func (*Driver) ID() string      { return "postgres" }
func (*Driver) Name() string    { return "PostgreSQL" }
func (*Driver) Available() bool { return true }
func (*Driver) ValidateDSN(dsn string) error {
	if !strings.HasPrefix(dsn, "postgres://") {
		return errors.New("the DSN is not a valid PostgreSQL connection string")
	}
	return nil
}
func (*Driver) MaskDSN(dsn string) string { return strings.Replace(dsn, ":secret@", ":****@", 1) }
func (*Driver) Test(_ context.Context, dsn string) (string, error) {
	if dsn != GoodDSN {
		return "", errors.New("could not connect: connection refused")
	}
	return "PostgreSQL 18.0", nil
}
func (d *Driver) Schema(context.Context, string) ([]dbdriver.Table, error) {
	if d.SchemaErr != nil {
		return nil, d.SchemaErr
	}
	return []dbdriver.Table{
		{Schema: "public", Name: "users", Kind: "table", Columns: []dbdriver.Column{
			{Name: "id", DataType: "integer"},
			{Name: "email", DataType: "text", SuggestedPII: true},
		}},
		{Schema: "public", Name: "orders", Kind: "table", Columns: []dbdriver.Column{
			{Name: "id", DataType: "integer"}, {Name: "total", DataType: "numeric"},
		}},
	}, nil
}
func (d *Driver) Query(_ context.Context, _, _ string, maxRows int, _ time.Duration) (*dbdriver.Result, error) {
	if d.QueryErr != nil {
		return nil, d.QueryErr
	}
	res := &dbdriver.Result{Columns: []string{"id"}, Rows: [][]any{}}
	for i := 0; i < d.Rows && i < maxRows; i++ {
		res.Rows = append(res.Rows, []any{i + 1})
	}
	res.Truncated = d.Rows > maxRows
	return res, nil
}
func (*Driver) Guard(pii []sqlguard.Column) dbdriver.Checker { return sqlguard.New(pii) }

// Tmphost is a fake tmphost.io server.
type Tmphost struct {
	*httptest.Server
	Uploads    atomic.Int32
	Registered atomic.Int32
	Refreshed  atomic.Int32
	// FailUpload makes file writes answer 402.
	FailUpload atomic.Bool
	// FailRefresh makes refresh grants answer invalid_grant.
	FailRefresh atomic.Bool
	mu          sync.Mutex
	LastBody    string
}

// NewTmphost starts a fake tmphost; it stops with the test.
func NewTmphost(t *testing.T) *Tmphost {
	th := &Tmphost{}
	mux := http.NewServeMux()
	th.Server = httptest.NewServer(mux)
	t.Cleanup(th.Close)
	write := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		write(w, 200, map[string]any{
			"issuer":                 th.URL,
			"authorization_endpoint": th.URL + "/oauth2/authorize",
			"token_endpoint":         th.URL + "/oauth2/token",
			"registration_endpoint":  th.URL + "/oauth2/register",
		})
	})
	mux.HandleFunc("POST /oauth2/register", func(w http.ResponseWriter, _ *http.Request) {
		th.Registered.Add(1)
		write(w, 201, map[string]any{"client_id": "client-1"})
	})
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			if r.PostForm.Get("code") != "good-code" || r.PostForm.Get("code_verifier") == "" {
				write(w, 400, map[string]any{"error": "invalid_grant"})
				return
			}
			write(w, 200, map[string]any{"access_token": "access-1", "refresh_token": "refresh-1", "expires_in": 3600})
		case "refresh_token":
			th.Refreshed.Add(1)
			if th.FailRefresh.Load() {
				write(w, 400, map[string]any{"error": "invalid_grant"})
				return
			}
			write(w, 200, map[string]any{"access_token": "access-2", "expires_in": 3600})
		}
	})
	mux.HandleFunc("GET /api/v1/users/me", func(w http.ResponseWriter, _ *http.Request) {
		write(w, 200, map[string]any{"user": map[string]any{"email": "dev@tmphost.test"}})
	})
	mux.HandleFunc("POST /api/v1/folders", func(w http.ResponseWriter, _ *http.Request) {
		write(w, 201, map[string]any{"folder": map[string]any{"id": "f1", "url": th.URL + "/f1/", "expiresAt": "2030-01-01T00:30:00Z"}})
	})
	mux.HandleFunc("PUT /api/v1/folders/{id}/files/{path...}", func(w http.ResponseWriter, r *http.Request) {
		if th.FailUpload.Load() {
			write(w, 402, map[string]any{"error": "insufficient credits: 1 needed"})
			return
		}
		body, _ := io.ReadAll(r.Body)
		th.mu.Lock()
		th.LastBody = string(body)
		th.mu.Unlock()
		th.Uploads.Add(1)
		write(w, 200, map[string]any{"file": map[string]any{"path": r.PathValue("path"), "url": "https://f1.file.tmphost.test/" + r.PathValue("path")}})
	})
	return th
}

// Env is a controller and what it was built from.
type Env struct {
	Ctrl    *crud.Controller
	Repo    *repository.Repository
	Driver  *Driver
	Tmphost *Tmphost
	Tokens  *auth.TokenService
	Sealer  *security.Sealer
	BaseURL string
	DBPath  string // the app database, a SQLite file
}

// Key is the fixed 32-byte encryption key tests use.
var Key = []byte("0123456789abcdef0123456789abcdef")

// Secret is the fixed JWT secret tests use.
const Secret = "test-secret-test-secret-test-secret!"

// New builds a controller admitting *@example.com.
func New(t *testing.T) *Env {
	t.Helper()
	var next atomic.Int64
	next.Store(1_000_000)
	dbPath := filepath.Join(t.TempDir(), "app.db")
	repo, err := repository.Open("sqlite", dbPath, func() int64 { return next.Add(1) << 22 })
	if err != nil {
		t.Fatal(err)
	}
	sealer, _ := security.NewSealer(Key)
	allow, _ := auth.NewAllowlist([]string{"*@example.com", "root@sqlmcp.local"})
	tokens, _ := auth.NewTokenService(Secret)
	d := &Driver{Rows: 3}
	th := NewTmphost(t)
	baseURL := "http://sqlmcp.test"
	ctrl := crud.New(crud.Params{
		Repo: repo, Drivers: dbdriver.NewRegistry(d, dbdriver.NewMySQL()), Sealer: sealer, Allowlist: allow,
		Tmphost: tmphost.New(th.URL, th.Client()),
		Config:  crud.Config{BaseURL: baseURL, MaxRows: 100, InlineRows: 5, QueryTimeout: time.Second},
	})
	return &Env{Ctrl: ctrl, Repo: repo, Driver: d, Tmphost: th, Tokens: tokens, Sealer: sealer, BaseURL: baseURL, DBPath: dbPath}
}
