package dbdriver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSQL is an in-memory database/sql server: every query returns cols and
// rows, so the MySQL driver's paths run with no server.
type fakeSQL struct {
	mu    sync.Mutex
	execs []string
	// execErr fails an Exec whose statement contains the key.
	execErr map[string]error
	cols    []string
	types   []string
	rows    [][]driver.Value
}

func (f *fakeSQL) Connect(context.Context) (driver.Conn, error) { return &fakeConn{f}, nil }
func (f *fakeSQL) Driver() driver.Driver                        { return nil }

type fakeConn struct{ f *fakeSQL }

func (*fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not supported") }
func (*fakeConn) Close() error                        { return nil }
func (*fakeConn) Begin() (driver.Tx, error)           { return fakeTx{}, nil }
func (*fakeConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return fakeTx{}, nil
}

func (c *fakeConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	c.f.execs = append(c.f.execs, query)
	for key, err := range c.f.execErr {
		if strings.Contains(query, key) {
			return nil, err
		}
	}
	return driver.RowsAffected(0), nil
}

func (c *fakeConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &fakeRows{f: c.f}, nil
}

type fakeTx struct{}

func (fakeTx) Commit() error   { return nil }
func (fakeTx) Rollback() error { return nil }

type fakeRows struct {
	f *fakeSQL
	i int
}

func (r *fakeRows) Columns() []string { return r.f.cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) ColumnTypeDatabaseTypeName(i int) string {
	return r.f.types[i]
}

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.i == len(r.f.rows) {
		return io.EOF
	}
	copy(dest, r.f.rows[r.i])
	r.i++
	return nil
}

// withFake makes dsn's pool the fake.
func withFake(t *testing.T, d *mysqlDriver, dsn string, f *fakeSQL) {
	t.Helper()
	_, err := d.pools.get(dsn, func() (*sql.DB, error) { return sql.OpenDB(f), nil })
	require.NoError(t, err)
}

func TestMySQLQueryOnFake(t *testing.T) {
	d := newTestMySQL(t)
	f := &fakeSQL{
		cols:  []string{"id", "doc", "at"},
		types: []string{"INT", "JSON", "DATETIME"},
		rows: [][]driver.Value{
			{int64(1), []byte(`{"a":1}`), time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
			{int64(2), nil, nil},
			{int64(3), nil, nil},
		},
	}
	withFake(t, d, "fake", f)
	ctx := context.Background()

	res, err := d.Query(ctx, "fake", "select id, doc, at from t", 10, 1500*time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, []string{"id", "doc", "at"}, res.Columns)
	assert.Equal(t, []any{int64(1), json.RawMessage(`{"a":1}`), "2026-01-02T03:04:05Z"}, res.Rows[0])
	assert.Equal(t, []any{int64(3), nil, nil}, res.Rows[2])
	assert.False(t, res.Truncated)
	assert.Equal(t, []string{"set session max_execution_time = 1500"}, f.execs)

	res, err = d.Query(ctx, "fake", "select id from t", 2, time.Second)
	require.NoError(t, err)
	assert.Len(t, res.Rows, 2)
	assert.True(t, res.Truncated)
}

func TestMySQLTimeoutOnFake(t *testing.T) {
	d := newTestMySQL(t)
	ctx := context.Background()

	// MariaDB has no max_execution_time.
	mariadb := &fakeSQL{
		execErr: map[string]error{"max_execution_time": &mysql.MySQLError{Number: errUnknownSystemVariable}},
		cols:    []string{"x"}, types: []string{"INT"},
	}
	withFake(t, d, "mariadb", mariadb)
	_, err := d.Query(ctx, "mariadb", "select 1", 1, 2500*time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, []string{"set session max_execution_time = 2500", "set session max_statement_time = 2.500"}, mariadb.execs)

	// Any other failure is the query's.
	broken := &fakeSQL{execErr: map[string]error{"max_execution_time": errors.New("gone away")}}
	withFake(t, d, "broken", broken)
	_, err = d.Query(ctx, "broken", "select 1", 1, time.Second)
	assert.EqualError(t, err, "gone away")
}

func TestMySQLSchemaOnFake(t *testing.T) {
	d := newTestMySQL(t)
	f := &fakeSQL{
		cols:  []string{"table_schema", "table_name", "table_type", "column_name", "column_type"},
		types: []string{"VARCHAR", "VARCHAR", "VARCHAR", "VARCHAR", "TEXT"},
		rows: [][]driver.Value{
			{"shop", "users", "BASE TABLE", "id", "int"},
			{"shop", "users", "BASE TABLE", "email", "varchar(255)"},
			{"shop", "v", "VIEW", "id", "int"},
		},
	}
	withFake(t, d, "fake", f)
	tables, err := d.Schema(context.Background(), "fake")
	require.NoError(t, err)
	assert.Equal(t, []Table{
		{Schema: "shop", Name: "users", Kind: "table", Columns: []Column{
			{Name: "id", DataType: "int"}, {Name: "email", DataType: "varchar(255)", SuggestedPII: true},
		}},
		{Schema: "shop", Name: "v", Kind: "view", Columns: []Column{{Name: "id", DataType: "int"}}},
	}, tables)

	// A NULL where the listing expects text.
	f.rows = [][]driver.Value{{"shop", "users", "BASE TABLE", nil, "int"}}
	_, err = d.Schema(context.Background(), "fake")
	assert.ErrorContains(t, err, "converting NULL to string")
}
