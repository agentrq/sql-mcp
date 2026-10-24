package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/agentrq/sql-mcp/cli/internal/config"
)

var now = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

var testKey = []byte(strings.Repeat("k", 32))

// server is a fake SQL MCP server: the CLI's OAuth endpoints and the part
// of the API the CLI's token opens.
type server struct {
	*httptest.Server
	mu          sync.Mutex
	access      string // the valid access token
	refresh     string // the valid refresh token
	refreshes   int
	failRefresh bool
	fail        map[string]bool // "METHOD /path" answers 500
	pii         []map[string]string
	queries     []string
}

func newServer(t *testing.T) *server {
	t.Helper()
	s := &server{access: "at0", refresh: "rt0", fail: map[string]bool{},
		pii: []map[string]string{{"schema": "public", "table": "users", "column": "email"}}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cli/oauth2/token", s.token)
	mux.HandleFunc("/api/v1/", s.api)
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func apiError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": msg, "code": status}})
}

func (s *server) token(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = r.ParseForm()
	switch {
	case r.PostForm.Get("grant_type") == "authorization_code" && r.PostForm.Get("code") == "good":
	case r.PostForm.Get("grant_type") == "refresh_token" && r.PostForm.Get("refresh_token") == s.refresh && !s.failRefresh:
		s.refreshes++
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	n := len(s.access)
	s.access, s.refresh = "at"+strings.Repeat("x", n), "rt"+strings.Repeat("x", n)
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": s.access, "refresh_token": s.refresh, "token_type": "Bearer", "expires_in": 3600,
		"databases": []string{"db1", "db2", "db3"},
	})
}

var schema = []map[string]any{
	{"schema": "public", "name": "users", "kind": "table", "columns": []map[string]any{
		{"name": "id", "dataType": "integer"},
		{"name": "email", "dataType": "text", "pii": true, "suggestedPii": true},
		{"name": "phone", "dataType": "text", "suggestedPii": true},
	}},
	{"schema": "public", "name": "orders", "kind": "table", "columns": []map[string]any{
		{"name": "id", "dataType": "integer"}, {"name": "total", "dataType": "numeric"},
	}},
	{"schema": "audit", "name": "events", "kind": "view", "columns": []map[string]any{{"name": "id", "dataType": "integer"}}},
	{"schema": "audit", "name": "orders", "kind": "table", "columns": []map[string]any{{"name": "id", "dataType": "integer"}}},
}

func (s *server) api(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+s.access {
		apiError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1")
	if s.fail[r.Method+" "+path] {
		apiError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	switch r.Method + " " + path {
	case "GET /auth/user":
		writeJSON(w, 200, map[string]any{"id": "u1", "email": "ann@example.com", "name": "Ann"})
	case "GET /databases":
		writeJSON(w, 200, map[string]any{"databases": []map[string]any{
			{"id": "db1", "name": "shop", "driver": "postgres", "piiColumnCount": len(s.pii), "dsnMasked": "postgres://app:****@db/shop"},
			{"id": "db2", "name": "crm", "driver": "mysql"},
			{"id": "db3", "name": "CRM", "driver": "mysql"},
		}})
	case "GET /databases/db1/schema":
		writeJSON(w, 200, map[string]any{"tables": schema})
	case "GET /databases/db1/pii":
		writeJSON(w, 200, map[string]any{"piiColumns": s.pii})
	case "POST /databases/db1/pii":
		var body struct {
			PIIColumns []map[string]string `json:"piiColumns"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.pii = append(s.pii, body.PIIColumns...)
		writeJSON(w, 200, map[string]any{"piiColumns": s.pii})
	case "POST /databases/db1/query":
		var body struct {
			SQL string `json:"sql"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.queries = append(s.queries, body.SQL)
		switch {
		case strings.Contains(body.SQL, "email"):
			apiError(w, http.StatusUnprocessableEntity, `column "email" of public.users is PII`)
		case strings.Contains(body.SQL, "many"):
			_, _ = io.WriteString(w, `{"columns":["n"],"rows":[[1],[2]],"rowCount":2,"truncated":true,"durationMs":9}`)
		case strings.Contains(body.SQL, "one"):
			_, _ = io.WriteString(w, `{"columns":["n"],"rows":[[1]],"rowCount":1,"durationMs":1}`)
		default:
			_, _ = io.WriteString(w, `{"columns":["id","note","total","ok","tags"],"rows":[[12345678901234567890,"a\tb\nc","9.50",true,{"k":[1]}],[2,null,null,false,null]],"rowCount":2,"truncated":false,"durationMs":4}`)
		}
	case "GET /databases/db1/queries":
		writeJSON(w, 200, map[string]any{"queries": []map[string]any{
			{"id": "q2", "source": "cli", "sql": "select\n  id from orders", "status": "ok", "rowCount": 1, "createdAt": now},
			{"id": "q1", "source": "mcp", "sql": "select email from users", "status": "rejected", "reason": "email is PII", "createdAt": now},
		}})
	default:
		apiError(w, http.StatusNotFound, "not found")
	}
}

// env is one CLI user: a config file and this machine's key.
type env struct {
	t      *testing.T
	srv    *server
	config string
	vars   map[string]string
	key    []byte
	// browser plays the person on the consent page: it returns what the
	// redirect to the CLI carries.
	browser func(q url.Values) url.Values
	now     time.Time
}

func newEnv(t *testing.T, srv *server) *env {
	dir := t.TempDir()
	e := &env{t: t, srv: srv, config: filepath.Join(dir, "config.json"), key: testKey, now: now}
	e.vars = map[string]string{config.EnvConfig: e.config}
	e.browser = func(q url.Values) url.Values { return url.Values{"code": {"good"}, "state": {q.Get("state")}} }
	return e
}

type result struct {
	code     int
	out, err string
}

func (e *env) run(stdin string, args ...string) result {
	e.t.Helper()
	var out, errOut bytes.Buffer
	code := Execute(args, Options{
		In:     strings.NewReader(stdin),
		Out:    &out,
		Err:    &errOut,
		Getenv: func(k string) string { return e.vars[k] },
		Now:    func() time.Time { return e.now },
		OpenBrowser: func(raw string) error {
			u, err := url.Parse(raw)
			require.NoError(e.t, err)
			back := e.browser(u.Query())
			go func() {
				res, err := http.Get(u.Query().Get("redirect_uri") + "?" + back.Encode())
				if err == nil {
					res.Body.Close()
				}
			}()
			return nil
		},
		MachineKey: func() ([]byte, error) { return e.key, nil },
		HTTP:       e.srv.Client(),
		Version:    "v9.9.9",
	})
	return result{code: code, out: out.String(), err: errOut.String()}
}

// loggedIn is an env that ran sqlmcp login.
func loggedIn(t *testing.T) (*env, *server) {
	srv := newServer(t)
	e := newEnv(t, srv)
	res := e.run("", "login", "--server", srv.URL)
	require.Equal(t, 0, res.code, res.err)
	return e, srv
}
