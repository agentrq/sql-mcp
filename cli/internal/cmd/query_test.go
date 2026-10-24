package cmd

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentrq/sql-mcp/cli/internal/client"
)

func TestQueryTable(t *testing.T) {
	e, srv := loggedIn(t)
	res := e.run("", "query", "shop", "select id, note, total, ok, tags from orders")
	require.Equal(t, 0, res.code, res.err)
	assert.Equal(t, "id                    note   total  ok     tags\n"+
		"12345678901234567890  a b c  9.50   true   {\"k\":[1]}\n"+
		"2                     NULL   NULL   false  NULL\n", res.out)
	assert.Equal(t, "(2 rows, 4 ms)\n", res.err)
	assert.Equal(t, []string{"select id, note, total, ok, tags from orders"}, srv.queries)

	res = e.run("", "query", "shop", "select one")
	assert.Equal(t, "(1 row, 1 ms)\n", res.err)
	res = e.run("", "query", "shop", "select many")
	assert.Contains(t, res.err, "The server's row limit cut the result at 2 rows")
}

func TestQueryCSVAndJSON(t *testing.T) {
	e, _ := loggedIn(t)
	res := e.run("", "query", "shop", "select x", "--format", "csv")
	require.Equal(t, 0, res.code, res.err)
	assert.Equal(t, "id,note,total,ok,tags\n12345678901234567890,\"a\tb\nc\",9.50,true,\"{\"\"k\"\":[1]}\"\n2,,,false,\n", res.out)

	for _, args := range [][]string{{"-f", "json"}, {"--json"}} {
		res = e.run("", append([]string{"query", "shop", "select x"}, args...)...)
		require.Equal(t, 0, res.code, res.err)
		assert.Contains(t, res.out, `12345678901234567890,`, "numbers stay exact")
		assert.Contains(t, res.out, `"rowCount": 2`)
	}

	res = e.run("", "query", "shop", "select x", "--format", "xml")
	assert.Contains(t, res.err, `unknown format "xml"`)
}

func TestQueryFromStdin(t *testing.T) {
	e, srv := loggedIn(t)
	res := e.run("select one\n", "query", "shop")
	require.Equal(t, 0, res.code, res.err)
	res = e.run("select one", "query", "shop", "-")
	require.Equal(t, 0, res.code, res.err)
	assert.Equal(t, []string{"select one\n", "select one"}, srv.queries)

	res = e.run("  \n", "query", "shop")
	assert.Contains(t, res.err, "no query")
}

func TestQueryRejected(t *testing.T) {
	e, _ := loggedIn(t)
	res := e.run("", "query", "shop", "select email from users")
	assert.Equal(t, 1, res.code)
	assert.Equal(t, "error: query rejected: column \"email\" of public.users is PII\n", res.err)
	res = e.run("", "query", "hr", "select 1")
	assert.Contains(t, res.err, `no database "hr"`)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("stdin closed") }

func TestQueryIOFailures(t *testing.T) {
	e, _ := loggedIn(t)
	a := &app{opts: Options{Out: failingWriter{}, Err: &strings.Builder{}}}
	assert.ErrorContains(t, a.printResult(&clientResult, formatCSV), "disk full")

	cmd := newQueryCmd(&app{opts: Options{In: failingReader{}, Getenv: func(k string) string { return e.vars[k] }}})
	cmd.SetArgs([]string{"shop"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	assert.ErrorContains(t, cmd.Execute(), "stdin closed")
}

var clientResult = client.Result{Columns: []string{"a"}, Rows: [][]any{{"x"}}, RowCount: 1}
