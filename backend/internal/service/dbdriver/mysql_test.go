package dbdriver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentrq/sql-mcp/backend/internal/service/sqlguard"
)

// unreachableMySQL parses but has nothing listening behind it.
const unreachableMySQL = "mysql://u:p@127.0.0.1:1/db?timeout=2s"

func newTestMySQL(t *testing.T) *mysqlDriver {
	t.Helper()
	d := NewMySQL().(*mysqlDriver)
	t.Cleanup(d.pools.closeAll)
	return d
}

func TestMySQLBasics(t *testing.T) {
	d := NewMySQL()
	assert.Equal(t, "mysql", d.ID())
	assert.Equal(t, "MySQL", d.Name())
	assert.True(t, d.Available())
	assert.ErrorContains(t, d.Guard([]sqlguard.Column{{Table: "users", Column: "email"}}).Check("select email from users"), "PII")
	assert.NoError(t, d.Guard(nil).Check("select 1"))
}

func TestMySQLValidateDSN(t *testing.T) {
	d := NewMySQL()
	for _, dsn := range []string{
		"mysql://u:p@localhost:3306/db?tls=true",
		"mysql://localhost/db",
		"mysql://u@[::1]:3306/",
		"u:p@tcp(localhost:3306)/db?parseTime=true",
		"u:p@unix(/tmp/mysql.sock)/db",
		"/db",
	} {
		assert.NoError(t, d.ValidateDSN(dsn), dsn)
	}
	for dsn, want := range map[string]string{
		"":                       "empty",
		"   ":                    "empty",
		"mysql://":               "not a valid MySQL",
		"mysql://h/db?tls=bogus": "not a valid MySQL",
		"postgres://u@h/db":      "not a valid MySQL",
		"u:p@tcp(h:3306/db":      "not a valid MySQL",
		"mysql://h/%zz":          "not a valid MySQL",
	} {
		assert.ErrorContains(t, d.ValidateDSN(dsn), want, dsn)
	}
}

func TestParseMySQLURL(t *testing.T) {
	cfg, err := parseMySQLDSN(" mysql://app:p%40ss:w@db.internal/shop%2Fx?tls=skip-verify&time_zone=%27%2B00%3A00%27 ")
	require.NoError(t, err)
	assert.Equal(t, "app", cfg.User)
	assert.Equal(t, "p@ss:w", cfg.Passwd)
	assert.Equal(t, "tcp", cfg.Net)
	assert.Equal(t, "db.internal:3306", cfg.Addr)
	assert.Equal(t, "shop/x", cfg.DBName)
	assert.Equal(t, "skip-verify", cfg.TLSConfig)
	assert.Equal(t, "'+00:00'", cfg.Params["time_zone"])
}

func TestMySQLMaskDSN(t *testing.T) {
	d := NewMySQL()
	for _, tc := range []struct{ in, want string }{
		{"mysql://u:secret@h:3306/db?tls=true", "mysql://u:****@h:3306/db?tls=true"},
		{"mysql://u@h/db", "mysql://u@h/db"},
		{"mysql://h/db", "mysql://h/db"},
		{"u:secret@tcp(h:3306)/db", "u:****@tcp(h:3306)/db"},
		{"u:se:c@r/et@tcp(h:3306)/db?x=1", "u:****@tcp(h:3306)/db?x=1"},
		{" u@tcp(h)/db ", "u@tcp(h)/db"},
		{"not a dsn", "****"},
	} {
		assert.Equal(t, tc.want, d.MaskDSN(tc.in), tc.in)
	}
}

func TestMySQLDefaultSchema(t *testing.T) {
	d := NewMySQL()
	assert.Equal(t, "shop", d.DefaultSchema("mysql://u@h/shop"))
	assert.Equal(t, "shop", d.DefaultSchema("u@tcp(h)/shop"))
	assert.Equal(t, "", d.DefaultSchema("mysql://u@h/"))
	assert.Equal(t, "", d.DefaultSchema("bad"))
	assert.Equal(t, "public", NewPostgres().DefaultSchema("postgres://h/db"))
}

func TestMySQLVersion(t *testing.T) {
	assert.Equal(t, "MySQL 8.4.3", mysqlVersion("8.4.3"))
	assert.Equal(t, "MySQL 5.7.44", mysqlVersion("5.7.44-log"))
	assert.Equal(t, "MariaDB 11.4.2", mysqlVersion("11.4.2-MariaDB-ubu2404"))
}

func TestMySQLValue(t *testing.T) {
	assert.Equal(t, json.RawMessage(`{"k":[1]}`), mysqlValue([]byte(`{"k":[1]}`), "JSON"))
	assert.Equal(t, "{bad", mysqlValue([]byte(`{bad`), "JSON"))
	assert.Equal(t, `{"k":[1]}`, mysqlValue([]byte(`{"k":[1]}`), "VARCHAR"))
	assert.Equal(t, int64(1), mysqlValue(int64(1), "INT"))
}

// Pools are opened lazily, so the cache can be exercised with no server.
func TestMySQLPoolCache(t *testing.T) {
	d := newTestMySQL(t)
	first, err := d.pool(unreachableMySQL)
	require.NoError(t, err)
	again, err := d.pool(unreachableMySQL)
	require.NoError(t, err)
	assert.Same(t, first, again)
	assert.Equal(t, 4, first.Stats().MaxOpenConnections)

	_, err = d.pool("mysql://")
	assert.EqualError(t, err, "the DSN is not a valid MySQL connection string")
	assert.Len(t, d.pools.pools, 1)

	d.pools.closeAll()
	assert.Empty(t, d.pools.pools)
}

func TestMySQLUnreachable(t *testing.T) {
	d := newTestMySQL(t)
	ctx := context.Background()

	_, err := d.Test(ctx, unreachableMySQL)
	assert.ErrorContains(t, err, "could not connect")
	_, err = d.Schema(ctx, unreachableMySQL)
	assert.ErrorContains(t, err, "could not read the schema")
	_, err = d.Query(ctx, unreachableMySQL, "select 1", 10, time.Second)
	assert.ErrorContains(t, err, "could not start a read-only transaction")

	for _, call := range []func() error{
		func() error { _, err := d.Test(ctx, "bad"); return err },
		func() error { _, err := d.Schema(ctx, "bad"); return err },
		func() error { _, err := d.Query(ctx, "bad", "select 1", 1, time.Second); return err },
	} {
		assert.ErrorContains(t, call(), "not a valid MySQL")
	}
}

// mysqlServer is a server the integration tests run against.
type mysqlServer struct {
	name, dsn string
}

// mysqlServers are the servers SQLMCP_TEST_MYSQL_DSN and
// SQLMCP_TEST_MARIADB_DSN name, each seeded with testdata/shop_mysql.sql. The
// tests skip without either.
func mysqlServers(t *testing.T) []mysqlServer {
	t.Helper()
	var out []mysqlServer
	for _, s := range []mysqlServer{{"MySQL", os.Getenv("SQLMCP_TEST_MYSQL_DSN")}, {"MariaDB", os.Getenv("SQLMCP_TEST_MARIADB_DSN")}} {
		if s.dsn != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		t.Skip("neither SQLMCP_TEST_MYSQL_DSN nor SQLMCP_TEST_MARIADB_DSN is set")
	}
	return out
}

// eachMySQL runs fn as a subtest against every server.
func eachMySQL(t *testing.T, fn func(t *testing.T, s mysqlServer)) {
	for _, s := range mysqlServers(t) {
		t.Run(s.name, func(t *testing.T) { fn(t, s) })
	}
}

// withParams adds query parameters to a DSN.
func withParams(dsn, params string) string {
	if strings.Contains(dsn, "?") {
		return dsn + "&" + params
	}
	return dsn + "?" + params
}

func TestMySQLTest(t *testing.T) {
	eachMySQL(t, func(t *testing.T, s mysqlServer) {
		version, err := newTestMySQL(t).Test(context.Background(), s.dsn)
		require.NoError(t, err)
		assert.Regexp(t, `^`+s.name+` \d+\.\d+`, version)
		assert.NotContains(t, version, "-")
	})
}

func TestMySQLSchema(t *testing.T) {
	eachMySQL(t, func(t *testing.T, s mysqlServer) {
		ctx := context.Background()

		// A view in a database of its own, dropped afterwards.
		schema := fmt.Sprintf("sqlmcp_test_%d", time.Now().UnixNano())
		admin := openAdmin(t, s.dsn)
		for _, stmt := range []string{"create database " + schema, "create view " + schema + ".v as select 1 as id, 'x' as email"} {
			_, err := admin.ExecContext(ctx, stmt)
			require.NoError(t, err)
		}
		t.Cleanup(func() { _, _ = admin.ExecContext(context.Background(), "drop database "+schema) })

		tables, err := newTestMySQL(t).Schema(ctx, s.dsn)
		require.NoError(t, err)

		byName := map[string]Table{}
		for _, tb := range tables {
			byName[tb.Schema+"."+tb.Name] = tb
			assert.NotContains(t, []string{"mysql", "information_schema", "performance_schema", "sys"}, tb.Schema)
		}
		require.Contains(t, byName, "shop.users")
		require.Contains(t, byName, "shop.orders")
		require.Contains(t, byName, "billing.cards")

		users := byName["shop.users"]
		assert.Equal(t, "table", users.Kind)
		require.NotEmpty(t, users.Columns)
		assert.Equal(t, "id", users.Columns[0].Name, "in ordinal order")
		assert.Regexp(t, `^int(\(\d+\))?$`, users.Columns[0].DataType, "MariaDB keeps the display width")
		pii := map[string]bool{}
		for _, c := range users.Columns {
			pii[c.Name] = c.SuggestedPII
		}
		assert.True(t, pii["email"])
		assert.False(t, pii["created_at"])

		view := byName[schema+".v"]
		assert.Equal(t, "view", view.Kind)
		require.Len(t, view.Columns, 2)
		assert.Equal(t, "email", view.Columns[1].Name)
		assert.True(t, view.Columns[1].SuggestedPII)
	})
}

// openAdmin is a plain connection, for setting up what a test needs.
func openAdmin(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	connector, err := mysqlConnector(dsn)
	require.NoError(t, err)
	db := sql.OpenDB(connector)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMySQLQuery(t *testing.T) {
	eachMySQL(t, func(t *testing.T, s mysqlServer) {
		d := newTestMySQL(t)
		res, err := d.Query(context.Background(), s.dsn, `
			select 1 as i, null as n, 'a' as s, true as b, cast(1.5 as decimal(3, 1)) as num,
			       2.5e0 as f, timestamp('2026-01-02 03:04:05') as ts, date('2026-01-02') as d,
			       x'ff00' as bin, json_object('k', json_array(1)) as j`, 10, 5*time.Second)
		require.NoError(t, err)
		assert.Equal(t, []string{"i", "n", "s", "b", "num", "f", "ts", "d", "bin", "j"}, res.Columns)
		require.Len(t, res.Rows, 1)
		assert.False(t, res.Truncated)

		row := res.Rows[0]
		j := row[9]
		encoded, err := json.Marshal(row[:9])
		require.NoError(t, err)
		assert.JSONEq(t, `[1, null, "a", 1, "1.5", 2.5, "2026-01-02T03:04:05Z", "2026-01-02T00:00:00Z", "/wA="]`, string(encoded))
		if s.name == "MySQL" {
			assert.Equal(t, json.RawMessage(`{"k": [1]}`), j, "a JSON column is JSON")
		} else {
			assert.JSONEq(t, `{"k": [1]}`, j.(string), "MariaDB's JSON is text")
		}
	})
}

func TestMySQLQueryRows(t *testing.T) {
	eachMySQL(t, func(t *testing.T, s mysqlServer) {
		d := newTestMySQL(t)
		ctx := context.Background()
		series := func(n int) string {
			return fmt.Sprintf("with recursive s (x) as (select 1 union all select x + 1 from s where x < %d) select x from s", n)
		}
		for _, tc := range []struct {
			name      string
			sql       string
			maxRows   int
			rows      int
			truncated bool
		}{
			{"under the limit", series(3), 10, 3, false},
			{"at the limit", series(3), 3, 3, false},
			{"over the limit", series(10), 3, 3, true},
			{"no rows", "select id from users where false", 10, 0, false},
			{"real table", "select id from orders order by id", 1000, 400, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				res, err := d.Query(ctx, s.dsn, tc.sql, tc.maxRows, 5*time.Second)
				require.NoError(t, err)
				assert.Len(t, res.Rows, tc.rows)
				assert.NotNil(t, res.Rows)
				assert.Equal(t, tc.truncated, res.Truncated)
			})
		}

		// A truncated query leaves the pool usable.
		res, err := d.Query(ctx, s.dsn, "select count(*) from orders", 1, 5*time.Second)
		require.NoError(t, err)
		assert.EqualValues(t, 400, res.Rows[0][0])
	})
}

func TestMySQLQueryRefuses(t *testing.T) {
	eachMySQL(t, func(t *testing.T, s mysqlServer) {
		d := newTestMySQL(t)
		ctx := context.Background()
		for _, tc := range []struct {
			name    string
			sql     string
			timeout time.Duration
			want    string
		}{
			{"insert", "insert into orders (id) values (-1)", 5 * time.Second, "READ ONLY transaction"},
			{"update", "update users set country = country", 5 * time.Second, "READ ONLY transaction"},
			{"statement timeout", "select count(*) from orders a, orders b, orders c where a.total + b.total > c.total", 200 * time.Millisecond, "interrupted"},
			{"syntax", "selec 1", 5 * time.Second, "SQL syntax"},
			{"two statements", "select 1; select 2", 5 * time.Second, "SQL syntax"},
			{"error mid-stream", "select (select id from orders where id <= if(o.id = 50, 2, 1)) from orders o order by o.id", 5 * time.Second, "more than 1 row"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				start := time.Now()
				_, err := d.Query(ctx, s.dsn, tc.sql, 100, tc.timeout)
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.want)
				assert.Less(t, time.Since(start), 4*time.Second)
			})
		}

		// Nothing was written.
		res, err := d.Query(ctx, s.dsn, "select count(*) from orders where id = -1", 1, 5*time.Second)
		require.NoError(t, err)
		assert.EqualValues(t, 0, res.Rows[0][0])
	})
}

// The DSN cannot loosen what the guard relies on.
func TestMySQLForcedSettings(t *testing.T) {
	eachMySQL(t, func(t *testing.T, s mysqlServer) {
		d := newTestMySQL(t)
		dsn := withParams(s.dsn, "multiStatements=true&charset=latin1&character_set_client=gbk&sql_mode=%27ANSI_QUOTES,NO_BACKSLASH_ESCAPES%27")
		res, err := d.Query(context.Background(), dsn, "select @@session.sql_mode, @@session.character_set_client", 1, 5*time.Second)
		require.NoError(t, err)
		mode := fmt.Sprint(res.Rows[0][0])
		assert.NotContains(t, mode, "ANSI_QUOTES")
		assert.NotContains(t, mode, "NO_BACKSLASH_ESCAPES")
		assert.Contains(t, mode, "STRICT_TRANS_TABLES")
		assert.Equal(t, "utf8mb4", res.Rows[0][1])

		_, err = d.Query(context.Background(), dsn, "select 1; select 2", 1, 5*time.Second)
		assert.ErrorContains(t, err, "SQL syntax")

		// "x" is a string, as the guard reads it, and \' does not end one.
		res, err = d.Query(context.Background(), dsn, `select "email", 'it\'s'`, 1, 5*time.Second)
		require.NoError(t, err)
		assert.Equal(t, []any{"email", "it's"}, res.Rows[0])
	})
}

func TestMySQLPoolPerDSN(t *testing.T) {
	eachMySQL(t, func(t *testing.T, s mysqlServer) {
		d := newTestMySQL(t)
		for _, v := range []string{s.dsn, withParams(s.dsn, "time_zone=%27%2B01%3A00%27"), s.dsn} {
			_, err := d.Query(context.Background(), v, "select 1", 1, 5*time.Second)
			require.NoError(t, err)
		}
		assert.Len(t, d.pools.pools, 2)
	})
}

func TestMySQLReaderUser(t *testing.T) {
	eachMySQL(t, func(t *testing.T, s mysqlServer) {
		cfg, err := parseMySQLDSN(s.dsn)
		require.NoError(t, err)
		reader := fmt.Sprintf("mysql://reader:reader@%s/%s", cfg.Addr, cfg.DBName)

		d := newTestMySQL(t)
		if _, err := d.Test(context.Background(), reader); err != nil {
			t.Skipf("no reader user: %v", err)
		}
		res, err := d.Query(context.Background(), reader, "select count(*) from users", 1, 5*time.Second)
		require.NoError(t, err)
		assert.EqualValues(t, 120, res.Rows[0][0])

		_, err = d.Query(context.Background(), reader, "select count(*) from billing.cards", 1, 5*time.Second)
		var me *mysql.MySQLError
		require.ErrorAs(t, err, &me)
	})
}
