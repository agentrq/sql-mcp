package cmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBLs(t *testing.T) {
	e, _ := loggedIn(t)
	res := e.run("", "db", "ls")
	require.Equal(t, 0, res.code, res.err)
	assert.Equal(t, "NAME  ID   ENGINE    PII  DSN\n"+
		"shop  db1  postgres  1    postgres://app:****@db/shop\n"+
		"crm   db2  mysql     0    \n"+
		"CRM   db3  mysql     0    \n", res.out)
	res = e.run("", "db", "ls", "--json")
	var v map[string][]map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.out), &v))
	assert.Len(t, v["databases"], 3)
}

func TestTables(t *testing.T) {
	e, _ := loggedIn(t)
	res := e.run("", "tables", "SHOP")
	require.Equal(t, 0, res.code, res.err)
	assert.Equal(t, "TABLE          KIND   COLUMNS  PII\n"+
		"public.users   table  3        email\n"+
		"public.orders  table  2        \n"+
		"audit.events   view   1        \n"+
		"audit.orders   table  1        \n", res.out)
	res = e.run("", "tables", "db1", "--json")
	assert.Contains(t, res.out, `"suggestedPii": true`)
}

func TestDescribe(t *testing.T) {
	e, _ := loggedIn(t)
	res := e.run("", "describe", "shop", "users")
	require.Equal(t, 0, res.code, res.err)
	assert.Equal(t, "public.users (table)\n"+
		"COLUMN  TYPE     PII\n"+
		"id      integer  \n"+
		"email   text     yes\n"+
		"phone   text     suggested\n", res.out)
	res = e.run("", "describe", "shop", "AUDIT.Orders")
	assert.Contains(t, res.out, "audit.orders (table)")
	res = e.run("", "describe", "shop", "orders")
	assert.Contains(t, res.err, "orders is in more than one schema: name it as schema.orders")
	res = e.run("", "describe", "shop", "nope")
	assert.Contains(t, res.err, `there is no table "nope"`)
	res = e.run("", "describe", "shop", "users", "--json")
	assert.Contains(t, res.out, `"dataType": "integer"`)
}

func TestDatabaseRefs(t *testing.T) {
	e, _ := loggedIn(t)
	res := e.run("", "tables", "crm")
	assert.Contains(t, res.err, `2 databases are named "crm": use an ID (db2, db3)`)
	res = e.run("", "tables", "hr")
	assert.Contains(t, res.err, `no database "hr" among those this login may open`)
}

// Every database command fails alike when it is not logged in, when the
// database list fails and when the database call itself fails.
func TestDatabaseCommandFailures(t *testing.T) {
	commands := map[string][]string{
		"db ls":       {"db", "ls"},
		"tables":      {"tables", "shop"},
		"describe":    {"describe", "shop", "users"},
		"query":       {"query", "shop", "select id from orders"},
		"pii ls":      {"pii", "ls", "shop"},
		"pii add":     {"pii", "add", "shop", "users.phone"},
		"pii suggest": {"pii", "suggest", "shop"},
		"log":         {"log", "shop"},
	}
	ownCall := map[string]string{
		"db ls": "GET /databases", "tables": "GET /databases/db1/schema", "describe": "GET /databases/db1/schema",
		"query": "POST /databases/db1/query", "pii ls": "GET /databases/db1/pii", "pii add": "GET /databases/db1/schema",
		"pii suggest": "GET /databases/db1/schema", "log": "GET /databases/db1/queries",
	}
	for name, args := range commands {
		t.Run(name, func(t *testing.T) {
			srv := newServer(t)
			e := newEnv(t, srv)
			res := e.run("", append(args, "--server", srv.URL)...)
			assert.Contains(t, res.err, "not logged in", "not logged in")

			e, srv = loggedIn(t)
			srv.fail[ownCall[name]] = true
			res = e.run("", args...)
			assert.Equal(t, 1, res.code)
			assert.Contains(t, res.err, "internal server error")

			if name != "db ls" {
				delete(srv.fail, ownCall[name])
				srv.fail["GET /databases"] = true
				res = e.run("", args...)
				assert.Contains(t, res.err, "internal server error")
			}
		})
	}
}
