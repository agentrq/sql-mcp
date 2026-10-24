package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tokens is a fake TokenSource: the token is "old" until refreshed.
type tokens struct {
	current   string
	refreshed int
	err       error
}

func (s *tokens) Token(context.Context) (string, error) { return s.current, s.err }
func (s *tokens) Refresh(context.Context) (string, error) {
	s.refreshed++
	s.current = "new"
	return s.current, nil
}

type call struct {
	method, path, auth, body string
}

// api answers path with its canned body; a request with token "old" is
// refused once.
func api(t *testing.T, answers map[string]string) (*Client, *tokens, *[]call) {
	t.Helper()
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		calls = append(calls, call{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), string(raw)})
		if r.Header.Get("Authorization") == "Bearer old" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, ok := answers[r.Method+" "+r.URL.RequestURI()]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"not found","code":404}}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	src := &tokens{current: "tok"}
	return New(srv.URL, srv.Client(), src), src, &calls
}

func TestCalls(t *testing.T) {
	c, _, calls := api(t, map[string]string{
		"GET /api/v1/auth/user":                     `{"id":"u1","email":"ann@example.com","name":"Ann"}`,
		"GET /api/v1/databases":                     `{"databases":[{"id":"db1","name":"shop","driver":"postgres","piiColumnCount":2}]}`,
		"GET /api/v1/databases/db1/schema":          `{"tables":[{"schema":"public","name":"users","kind":"table","columns":[{"name":"email","dataType":"text","pii":true,"suggestedPii":true}]}]}`,
		"GET /api/v1/databases/db1/pii":             `{"piiColumns":[{"schema":"public","table":"users","column":"email"}]}`,
		"POST /api/v1/databases/db1/pii":            `{"piiColumns":[{"schema":"public","table":"users","column":"email"},{"schema":"public","table":"users","column":"phone"}]}`,
		"POST /api/v1/databases/db1/query":          `{"columns":["id","total"],"rows":[[12345678901234567890,"9.50"],[null,true]],"rowCount":2,"truncated":false,"durationMs":3}`,
		"GET /api/v1/databases/db1/queries?limit=5": `{"queries":[{"id":"q1","source":"cli","sql":"select 1","status":"ok","rowCount":1}]}`,
	})
	ctx := context.Background()

	u, err := c.Me(ctx)
	require.NoError(t, err)
	assert.Equal(t, &User{ID: "u1", Email: "ann@example.com", Name: "Ann"}, u)
	dbs, err := c.Databases(ctx)
	require.NoError(t, err)
	assert.Equal(t, []Database{{ID: "db1", Name: "shop", Driver: "postgres", PIIColumnCount: 2}}, dbs)
	tables, err := c.Schema(ctx, "db1")
	require.NoError(t, err)
	assert.Equal(t, []Table{{Schema: "public", Name: "users", Kind: "table", Columns: []Column{{Name: "email", DataType: "text", PII: true, SuggestedPII: true}}}}, tables)
	pii, err := c.PII(ctx, "db1")
	require.NoError(t, err)
	assert.Len(t, pii, 1)
	pii, err = c.AddPII(ctx, "db1", []PIIColumn{{Table: "users", Column: "phone"}})
	require.NoError(t, err)
	assert.Len(t, pii, 2)
	res, err := c.Query(ctx, "db1", "select id, total from orders")
	require.NoError(t, err)
	assert.Equal(t, json.Number("12345678901234567890"), res.Rows[0][0], "big numbers stay exact")
	assert.Equal(t, []any{nil, true}, res.Rows[1])
	logs, err := c.Queries(ctx, "db1", 5)
	require.NoError(t, err)
	assert.Equal(t, "cli", logs[0].Source)

	for _, cl := range *calls {
		assert.Equal(t, "Bearer tok", cl.auth)
	}
	assert.JSONEq(t, `{"piiColumns":[{"schema":"","table":"users","column":"phone"}]}`, (*calls)[4].body)
	assert.JSONEq(t, `{"sql":"select id, total from orders"}`, (*calls)[5].body)
	assert.Empty(t, (*calls)[0].body)
}

func TestRefreshesOnceAfter401(t *testing.T) {
	c, src, calls := api(t, map[string]string{"GET /api/v1/auth/user": `{"id":"u1"}`})
	src.current = "old"
	_, err := c.Me(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, src.refreshed)
	assert.Equal(t, []string{"Bearer old", "Bearer new"}, []string{(*calls)[0].auth, (*calls)[1].auth})
}

func TestErrors(t *testing.T) {
	c, src, _ := api(t, map[string]string{"GET /api/v1/databases": `{`})
	ctx := context.Background()

	_, err := c.Schema(ctx, "nope")
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, &APIError{Status: 404, Message: "not found"}, apiErr)

	_, err = c.Databases(ctx)
	assert.ErrorContains(t, err, "read the answer to GET /databases")

	src.err = errors.New("not logged in")
	_, err = c.Me(ctx)
	assert.EqualError(t, err, "not logged in")

	bad := New("http://bad host", http.DefaultClient, &tokens{current: "t"})
	_, err = bad.Me(ctx)
	assert.Error(t, err)

	// Not the API's error shape: the status says what happened.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>bad gateway</html>"))
	}))
	defer proxy.Close()
	_, err = New(proxy.URL, proxy.Client(), &tokens{current: "t"}).Me(ctx)
	assert.EqualError(t, err, "502 Bad Gateway")
	assert.True(t, strings.Contains(err.Error(), "502"))
}
