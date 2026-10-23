package dbdriver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentrq/sql-mcp/backend/internal/service/sqlguard"
)

type postgresDriver struct {
	pools *poolCache[*pgxpool.Pool]
}

// NewPostgres is the PostgreSQL engine.
func NewPostgres() Driver {
	return &postgresDriver{pools: newPoolCache((*pgxpool.Pool).Close)}
}

func (*postgresDriver) ID() string      { return "postgres" }
func (*postgresDriver) Name() string    { return "PostgreSQL" }
func (*postgresDriver) Available() bool { return true }

func (*postgresDriver) DefaultSchema(string) string { return "public" }

func (*postgresDriver) ValidateDSN(dsn string) error {
	if strings.TrimSpace(dsn) == "" {
		return errors.New("the DSN is empty")
	}
	if _, err := pgxpool.ParseConfig(dsn); err != nil {
		return errors.New("the DSN is not a valid PostgreSQL connection string")
	}
	return nil
}

// MaskDSN hides the password of a URL or key=value DSN.
func (*postgresDriver) MaskDSN(dsn string) string {
	if u, err := url.Parse(dsn); err == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") {
		if _, ok := u.User.Password(); ok {
			u.User = url.UserPassword(u.User.Username(), "____")
		}
		q := u.Query()
		if q.Has("password") {
			q.Set("password", "____")
			u.RawQuery = q.Encode()
		}
		return strings.ReplaceAll(u.String(), "____", "****")
	}
	return kvPassword.ReplaceAllString(dsn, "${1}****")
}

// kvPassword is a key=value DSN's password, as libpq reads it: spaces may
// surround the =, and a quoted value may hold spaces and \' escapes.
var kvPassword = regexp.MustCompile(`(?i)((?:^|\s)password\s*=\s*)('(?:\\.|[^'\\])*'?|\S*)`)

func (d *postgresDriver) pool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return d.pools.get(dsn, func() (*pgxpool.Pool, error) {
		cfg, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			return nil, errors.New("the DSN is not a valid PostgreSQL connection string")
		}
		cfg.MaxConns = 4
		cfg.MaxConnIdleTime = 5 * time.Minute
		cfg.ConnConfig.RuntimeParams["application_name"] = "sql-mcp"
		return pgxpool.NewWithConfig(ctx, cfg)
	})
}

func (d *postgresDriver) Test(ctx context.Context, dsn string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return "", fmt.Errorf("could not connect: %w", err)
	}
	defer conn.Close(context.Background())
	var version string
	if err := conn.QueryRow(ctx, "select version()").Scan(&version); err != nil {
		return "", fmt.Errorf("could not query the server version: %w", err)
	}
	// "PostgreSQL 17.2 on x86_64-pc-linux-gnu, ..." → "PostgreSQL 17.2"
	if i := strings.Index(version, " on "); i > 0 {
		version = version[:i]
	}
	return version, nil
}

const schemaSQL = `
select c.table_schema, c.table_name, coalesce(t.table_type, ''), c.column_name, c.data_type
from information_schema.columns c
left join information_schema.tables t on t.table_schema = c.table_schema and t.table_name = c.table_name
where c.table_schema not in ('pg_catalog', 'information_schema') and c.table_schema not like 'pg_toast%'
order by c.table_schema, c.table_name, c.ordinal_position
limit 20000`

func (d *postgresDriver) Schema(ctx context.Context, dsn string) ([]Table, error) {
	pool, err := d.pool(ctx, dsn)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, schemaSQL)
	if err != nil {
		return nil, fmt.Errorf("could not read the schema: %w", err)
	}
	defer rows.Close()
	var tables []Table
	for rows.Next() {
		var schema, table, kind, column, dataType string
		if err := rows.Scan(&schema, &table, &kind, &column, &dataType); err != nil {
			return nil, err
		}
		if n := len(tables); n == 0 || tables[n-1].Schema != schema || tables[n-1].Name != table {
			k := "table"
			if kind == "VIEW" {
				k = "view"
			}
			tables = append(tables, Table{Schema: schema, Name: table, Kind: k})
		}
		t := &tables[len(tables)-1]
		t.Columns = append(t.Columns, Column{Name: column, DataType: dataType, SuggestedPII: SuggestPII(column)})
	}
	return tables, rows.Err()
}

func (d *postgresDriver) Query(ctx context.Context, dsn, sql string, maxRows int, timeout time.Duration) (*Result, error) {
	pool, err := d.pool(ctx, dsn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
	defer cancel()

	// Read-only on the server too: the guard is the first line, the
	// transaction mode the second.
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("could not start a read-only transaction: %w", err)
	}
	// Read-only, so rolling back is how every transaction ends.
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, "select set_config('statement_timeout', $1, true)", strconv.FormatInt(timeout.Milliseconds(), 10)); err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := &Result{Rows: [][]any{}}
	for _, f := range rows.FieldDescriptions() {
		res.Columns = append(res.Columns, f.Name)
	}
	for rows.Next() {
		if len(res.Rows) == maxRows {
			res.Truncated = true
			break
		}
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		for i, v := range values {
			values[i] = JSONValue(v)
		}
		res.Rows = append(res.Rows, values)
	}
	if !res.Truncated {
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return res, nil
}

func (*postgresDriver) Guard(pii []sqlguard.Column) Checker {
	return sqlguard.New(pii)
}

// JSONValue makes a value pgx returned encodable as JSON the way a person
// reads it: a UUID as text, a timestamp as RFC 3339, bytes as text when they
// are UTF-8 and base64 otherwise.
func JSONValue(v any) any {
	switch x := v.(type) {
	case nil, bool, string, int, int8, int16, int32, int64, uint8, uint16, uint32, uint64, float32, float64:
		return x
	case [16]byte:
		return fmt.Sprintf("%x-%x-%x-%x-%x", x[0:4], x[4:6], x[6:8], x[8:10], x[10:16])
	case time.Time:
		return x.Format(time.RFC3339Nano)
	case []byte:
		if utf8.Valid(x) {
			return string(x)
		}
		return base64.StdEncoding.EncodeToString(x)
	case json.Marshaler:
		if b, err := x.MarshalJSON(); err == nil {
			return json.RawMessage(b)
		}
	case fmt.Stringer:
		return x.String()
	}
	if _, err := json.Marshal(v); err == nil {
		return v
	}
	return fmt.Sprint(v)
}
