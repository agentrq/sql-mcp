package dbdriver

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unreachableDSN parses but has nothing listening behind it.
const unreachableDSN = "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=2"

func newTestPostgres(t *testing.T) *postgresDriver {
	t.Helper()
	d := NewPostgres().(*postgresDriver)
	t.Cleanup(func() {
		for _, p := range d.pools {
			p.pool.Close()
		}
	})
	return d
}

func poolKey(dsn string) string {
	sum := sha256.Sum256([]byte(dsn))
	return base64.RawStdEncoding.EncodeToString(sum[:])
}

// Pools are made lazily, so the cache can be exercised with no server.
func TestPoolCache(t *testing.T) {
	d := newTestPostgres(t)
	ctx := context.Background()
	dsn := func(i int) string { return fmt.Sprintf("%s&application_name=t%d", unreachableDSN, i) }

	first, err := d.pool(ctx, dsn(0))
	require.NoError(t, err)
	again, err := d.pool(ctx, dsn(0))
	require.NoError(t, err)
	assert.Same(t, first, again)
	assert.Equal(t, "sql-mcp", first.Config().ConnConfig.RuntimeParams["application_name"])
	assert.EqualValues(t, 4, first.Config().MaxConns)

	for i := 1; i < maxPools; i++ {
		_, err := d.pool(ctx, dsn(i))
		require.NoError(t, err)
	}
	require.Len(t, d.pools, maxPools)

	// The least recently used pool makes room for a new one.
	d.pools[poolKey(dsn(0))].used = time.Now() // touched last
	d.pools[poolKey(dsn(5))].used = time.Now().Add(-time.Hour)
	_, err = d.pool(ctx, dsn(maxPools))
	require.NoError(t, err)
	assert.Len(t, d.pools, maxPools)
	assert.NotContains(t, d.pools, poolKey(dsn(5)))
	assert.Contains(t, d.pools, poolKey(dsn(0)))
	assert.Contains(t, d.pools, poolKey(dsn(maxPools)))

	_, err = d.pool(ctx, "postgres://u@h:bad/db")
	assert.EqualError(t, err, "the DSN is not a valid PostgreSQL connection string")
}

func TestUnreachable(t *testing.T) {
	d := newTestPostgres(t)
	ctx := context.Background()

	_, err := d.Test(ctx, unreachableDSN)
	assert.ErrorContains(t, err, "could not connect")
	_, err = d.Schema(ctx, unreachableDSN)
	assert.ErrorContains(t, err, "could not read the schema")
	_, err = d.Query(ctx, unreachableDSN, "select 1", 10, time.Second)
	assert.ErrorContains(t, err, "could not start a read-only transaction")

	_, err = d.Schema(ctx, "postgres://u@h:bad/db")
	assert.Error(t, err)
	_, err = d.Query(ctx, "postgres://u@h:bad/db", "select 1", 10, time.Second)
	assert.Error(t, err)
}

// pgDSN is the database the integration tests run against, or skips them.
func pgDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("SQLMCP_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("SQLMCP_TEST_PG_DSN is not set")
	}
	return dsn
}

func TestPostgresTest(t *testing.T) {
	dsn := pgDSN(t)
	version, err := newTestPostgres(t).Test(context.Background(), dsn)
	require.NoError(t, err)
	assert.Regexp(t, `^PostgreSQL \d+`, version)
	assert.NotContains(t, version, " on ")
}

func TestPostgresSchema(t *testing.T) {
	dsn := pgDSN(t)
	ctx := context.Background()

	// A view in a schema of its own, dropped afterwards.
	schema := fmt.Sprintf("sqlmcp_test_%d", time.Now().UnixNano())
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, "create schema "+schema+"; create view "+schema+".v as select 1 as id, 'x'::text as email")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), "drop schema "+schema+" cascade") })

	tables, err := newTestPostgres(t).Schema(ctx, dsn)
	require.NoError(t, err)

	byName := map[string]Table{}
	for _, tb := range tables {
		byName[tb.Schema+"."+tb.Name] = tb
		assert.NotContains(t, []string{"pg_catalog", "information_schema"}, tb.Schema)
	}
	require.Contains(t, byName, "public.users")
	require.Contains(t, byName, "public.orders")
	require.Contains(t, byName, "billing.cards")

	users := byName["public.users"]
	assert.Equal(t, "table", users.Kind)
	require.NotEmpty(t, users.Columns)
	assert.Equal(t, Column{Name: "id", DataType: "integer"}, users.Columns[0], "in ordinal order")
	pii := map[string]bool{}
	for _, c := range users.Columns {
		pii[c.Name] = c.SuggestedPII
	}
	assert.True(t, pii["email"])
	assert.False(t, pii["created_at"])

	view := byName[schema+".v"]
	assert.Equal(t, "view", view.Kind)
	assert.Equal(t, []Column{{Name: "id", DataType: "integer"}, {Name: "email", DataType: "text", SuggestedPII: true}}, view.Columns)
}

func TestPostgresQuery(t *testing.T) {
	dsn := pgDSN(t)
	d := newTestPostgres(t)
	res, err := d.Query(context.Background(), dsn, `
		select 1::int as i, null::text as n, 'a'::text as s, true as b, 1.5::numeric as num,
		       '00112233-4455-6677-8899-aabbccddeeff'::uuid as u,
		       '2026-01-02T03:04:05Z'::timestamptz as ts,
		       '\xff00'::bytea as bin, 'hi'::bytea as txt,
		       '{"k":[1]}'::jsonb as j, array[1,2] as arr`, 10, 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, []string{"i", "n", "s", "b", "num", "u", "ts", "bin", "txt", "j", "arr"}, res.Columns)
	require.Len(t, res.Rows, 1)
	assert.False(t, res.Truncated)

	encoded, err := json.Marshal(res.Rows[0])
	require.NoError(t, err)
	assert.JSONEq(t, `[1, null, "a", true, 1.5, "00112233-4455-6677-8899-aabbccddeeff",
		"2026-01-02T03:04:05Z", "/wA=", "hi", {"k":[1]}, [1,2]]`, string(encoded))
}

func TestPostgresQueryRows(t *testing.T) {
	dsn := pgDSN(t)
	d := newTestPostgres(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name      string
		sql       string
		maxRows   int
		rows      int
		truncated bool
	}{
		{"under the limit", "select x from generate_series(1, 3) x", 10, 3, false},
		{"at the limit", "select x from generate_series(1, 3) x", 3, 3, false},
		{"over the limit", "select x from generate_series(1, 10) x", 3, 3, true},
		{"no rows", "select id from users where false", 10, 0, false},
		{"real table", "select id from orders order by id", 1000, 400, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := d.Query(ctx, dsn, tc.sql, tc.maxRows, 5*time.Second)
			require.NoError(t, err)
			assert.Len(t, res.Rows, tc.rows)
			assert.NotNil(t, res.Rows)
			assert.Equal(t, tc.truncated, res.Truncated)
		})
	}
}

func TestPostgresQueryRefuses(t *testing.T) {
	dsn := pgDSN(t)
	d := newTestPostgres(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		sql     string
		timeout time.Duration
		want    string
	}{
		{"insert", "insert into public.orders (id) values (-1)", 5 * time.Second, "read-only transaction"},
		{"update in a CTE", "with x as (update users set country = country returning id) select * from x", 5 * time.Second, "read-only transaction"},
		{"ddl", "create table nope (id int)", 5 * time.Second, "read-only transaction"},
		{"set off read-only", "set transaction read write", 5 * time.Second, ""},
		{"statement timeout", "select pg_sleep(5)", 200 * time.Millisecond, "statement timeout"},
		{"syntax", "selec 1", 5 * time.Second, "syntax error"},
		{"error mid-stream", "select 1 / (3 - x) from generate_series(1, 5) x", 5 * time.Second, "division by zero"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			_, err := d.Query(ctx, dsn, tc.sql, 100, tc.timeout)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Less(t, time.Since(start), 4*time.Second)
		})
	}

	// Nothing was written.
	res, err := d.Query(ctx, dsn, "select count(*) from public.orders where id = -1", 1, 5*time.Second)
	require.NoError(t, err)
	assert.EqualValues(t, 0, res.Rows[0][0])
}

func TestPostgresPoolPerDSN(t *testing.T) {
	dsn := pgDSN(t)
	d := newTestPostgres(t)
	ctx := context.Background()
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	variants := []string{dsn, dsn + sep + "application_name=other", dsn}
	for _, v := range variants {
		res, err := d.Query(ctx, v, "select current_setting('application_name')", 1, 5*time.Second)
		require.NoError(t, err)
		// The pool names itself, whatever the DSN asked for.
		assert.Equal(t, "sql-mcp", res.Rows[0][0])
	}
	assert.Len(t, d.pools, 2)
}

func TestPostgresReaderRole(t *testing.T) {
	dsn := pgDSN(t)
	cfg, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	reader := fmt.Sprintf("postgres://reader:reader@%s:%d/%s?sslmode=disable", cfg.Host, cfg.Port, cfg.Database)

	d := newTestPostgres(t)
	if _, err := d.Test(context.Background(), reader); err != nil {
		t.Skipf("no reader role: %v", err)
	}
	res, err := d.Query(context.Background(), reader, "select count(*) from users", 1, 5*time.Second)
	require.NoError(t, err)
	assert.EqualValues(t, 120, res.Rows[0][0])
}
