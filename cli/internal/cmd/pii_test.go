package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPIILs(t *testing.T) {
	e, _ := loggedIn(t)
	res := e.run("", "pii", "ls", "shop")
	require.Equal(t, 0, res.code, res.err)
	assert.Equal(t, "public.users.email\n", res.out)
	res = e.run("", "pii", "ls", "shop", "--json")
	assert.Contains(t, res.out, `"column": "email"`)
}

func TestPIIAdd(t *testing.T) {
	e, srv := loggedIn(t)
	res := e.run("", "pii", "add", "shop", "USERS.Phone", "public.users.email", "public.orders.total")
	require.Equal(t, 0, res.code, res.err)
	assert.Equal(t, "marked   public.users.phone\nmarked   public.orders.total\nalready  public.users.email\n", res.out)
	assert.Equal(t, []map[string]string{
		{"schema": "public", "table": "users", "column": "email"},
		{"schema": "public", "table": "users", "column": "phone"},
		{"schema": "public", "table": "orders", "column": "total"},
	}, srv.pii, "names are sent as the schema spells them")

	res = e.run("", "pii", "add", "shop", "users.email", "--json")
	assert.Contains(t, res.out, `"already": [`)

	for ref, want := range map[string]string{
		"email":           `"email" is not a column`,
		".email":          `".email" is not a column`,
		"users.":          `"users." is not a column`,
		"nope.email":      `there is no table "nope"`,
		"orders.id":       "orders is in more than one schema",
		"users.password":  `public.users has no column "password"`,
		"audit.events.id": "",
	} {
		res = e.run("", "pii", "add", "shop", ref)
		if want == "" {
			assert.Equal(t, 0, res.code, res.err)
			continue
		}
		assert.Contains(t, res.err, want, ref)
	}

	srv.fail["POST /databases/db1/pii"] = true
	res = e.run("", "pii", "add", "shop", "public.orders.id")
	assert.Contains(t, res.err, "internal server error")
}

func TestPIISuggest(t *testing.T) {
	e, _ := loggedIn(t)
	res := e.run("", "pii", "suggest", "shop")
	require.Equal(t, 0, res.code, res.err)
	assert.Equal(t, "public.users.phone\n\nTo mark them: sqlmcp pii add shop public.users.phone\n", res.out)
	res = e.run("", "pii", "suggest", "shop", "--json")
	assert.JSONEq(t, `{"suggested":["public.users.phone"]}`, res.out)

	require.Equal(t, 0, e.run("", "pii", "add", "shop", "users.phone").code)
	schema[0]["columns"].([]map[string]any)[2]["pii"] = true
	t.Cleanup(func() { delete(schema[0]["columns"].([]map[string]any)[2], "pii") })
	res = e.run("", "pii", "suggest", "shop")
	assert.Equal(t, "No unmarked column looks like PII by its name.\n", res.out)
}

func TestLog(t *testing.T) {
	e, _ := loggedIn(t)
	res := e.run("", "log", "shop", "-n", "5")
	require.Equal(t, 0, res.code, res.err)
	when := now.Local().Format("2006-01-02 15:04:05")
	assert.Equal(t, "TIME                 SOURCE  STATUS    SQL                      RESULT\n"+
		when+"  cli     ok        select id from orders    1 row\n"+
		when+"  mcp     rejected  select email from users  email is PII\n", res.out)
	res = e.run("", "log", "shop", "--json")
	assert.Contains(t, res.out, `"source": "mcp"`)
}
