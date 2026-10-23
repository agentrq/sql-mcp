package dbdriver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/agentrq/sql-mcp/backend/internal/service/sqlguard"
)

// mysqlSQLMode is the sql_mode every connection runs with: MySQL 8's default.
// It must hold neither ANSI_QUOTES nor NO_BACKSLASH_ESCAPES, or the server
// would read a query differently from the guard (see sqlguard.MySQLGuard).
const mysqlSQLMode = "'ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION'"

// errUnknownSystemVariable is MySQL's ER_UNKNOWN_SYSTEM_VARIABLE.
const errUnknownSystemVariable = 1193

var errInvalidMySQLDSN = errors.New("the DSN is not a valid MySQL connection string")

type mysqlDriver struct {
	pools *poolCache[*sql.DB]
}

// NewMySQL is the MySQL engine, which also serves MariaDB.
func NewMySQL() Driver {
	return &mysqlDriver{pools: newPoolCache(func(db *sql.DB) { _ = db.Close() })}
}

func (*mysqlDriver) ID() string      { return "mysql" }
func (*mysqlDriver) Name() string    { return "MySQL" }
func (*mysqlDriver) Available() bool { return true }

// parseMySQLDSN reads either form people paste: a URL
// (mysql://user:pass@host:3306/db?tls=true) or the Go driver's own
// (user:pass@tcp(host:3306)/db?tls=true).
func parseMySQLDSN(dsn string) (*mysql.Config, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, errors.New("the DSN is empty")
	}
	if !strings.HasPrefix(dsn, "mysql://") {
		cfg, err := mysql.ParseDSN(dsn)
		if err != nil {
			return nil, errInvalidMySQLDSN
		}
		return cfg, nil
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Host == "" {
		return nil, errInvalidMySQLDSN
	}
	native := "tcp(" + u.Host + ")/" + strings.TrimPrefix(u.EscapedPath(), "/")
	if u.RawQuery != "" {
		native += "?" + u.RawQuery
	}
	cfg, err := mysql.ParseDSN(native)
	if err != nil {
		return nil, errInvalidMySQLDSN
	}
	cfg.User = u.User.Username()
	cfg.Passwd, _ = u.User.Password()
	return cfg, nil
}

// mysqlConnector connects with the DSN's configuration and what the app
// relies on forced: one statement per query, no local files, utf8mb4 and the
// sql_mode the guard parses with.
func mysqlConnector(dsn string) (driver.Connector, error) {
	cfg, err := parseMySQLDSN(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MultiStatements = false
	cfg.AllowAllFiles = false
	cfg.ParseTime = true
	cfg.ConnectionAttributes = "program_name:sql-mcp"
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	for k := range cfg.Params {
		// A multi-byte client charset can hide a quote from the guard.
		if lower := strings.ToLower(k); strings.HasPrefix(lower, "character_set") || strings.HasPrefix(lower, "collation") {
			delete(cfg.Params, k)
		}
	}
	cfg.Params["sql_mode"] = mysqlSQLMode
	_ = cfg.Apply(mysql.Charset("utf8mb4", "")) // setting a charset cannot fail
	return mysql.NewConnector(cfg)
}

func (*mysqlDriver) ValidateDSN(dsn string) error {
	_, err := parseMySQLDSN(dsn)
	return err
}

// MaskDSN hides the password of either DSN form.
func (*mysqlDriver) MaskDSN(dsn string) string {
	cfg, err := parseMySQLDSN(dsn)
	if err != nil {
		return "****"
	}
	dsn = strings.TrimSpace(dsn)
	if strings.HasPrefix(dsn, "mysql://") {
		u, _ := url.Parse(dsn)
		if _, ok := u.User.Password(); ok {
			u.User = url.UserPassword(u.User.Username(), "____")
		}
		return strings.Replace(u.String(), "____", "****", 1)
	}
	if cfg.Passwd == "" {
		return dsn
	}
	// The driver's form is user:password@..., the password up to the '@'.
	return cfg.User + ":****" + dsn[len(cfg.User)+1+len(cfg.Passwd):]
}

func (*mysqlDriver) DefaultSchema(dsn string) string {
	cfg, err := parseMySQLDSN(dsn)
	if err != nil {
		return ""
	}
	return cfg.DBName
}

func (d *mysqlDriver) pool(dsn string) (*sql.DB, error) {
	return d.pools.get(dsn, func() (*sql.DB, error) {
		connector, err := mysqlConnector(dsn)
		if err != nil {
			return nil, err
		}
		db := sql.OpenDB(connector)
		db.SetMaxOpenConns(4)
		db.SetConnMaxIdleTime(5 * time.Minute)
		return db, nil
	})
}

func (d *mysqlDriver) Test(ctx context.Context, dsn string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	connector, err := mysqlConnector(dsn)
	if err != nil {
		return "", err
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	var version string
	if err := db.QueryRowContext(ctx, "select version()").Scan(&version); err != nil {
		return "", fmt.Errorf("could not connect: %w", err)
	}
	return mysqlVersion(version), nil
}

// mysqlVersion names the server: "8.4.3" → "MySQL 8.4.3",
// "11.4.2-MariaDB-ubu2404" → "MariaDB 11.4.2".
func mysqlVersion(version string) string {
	name := "MySQL"
	if strings.Contains(version, "MariaDB") {
		name = "MariaDB"
	}
	number, _, _ := strings.Cut(version, "-")
	return name + " " + number
}

const mysqlSchemaSQL = `
select c.table_schema, c.table_name, coalesce(t.table_type, ''), c.column_name, c.column_type
from information_schema.columns c
left join information_schema.tables t on t.table_schema = c.table_schema and t.table_name = c.table_name
where c.table_schema not in ('mysql', 'information_schema', 'performance_schema', 'sys')
order by c.table_schema, c.table_name, c.ordinal_position
limit 20000`

func (d *mysqlDriver) Schema(ctx context.Context, dsn string) ([]Table, error) {
	db, err := d.pool(dsn)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, mysqlSchemaSQL)
	if err != nil {
		return nil, fmt.Errorf("could not read the schema: %w", err)
	}
	defer rows.Close()
	var tables []Table
	for err == nil && rows.Next() {
		var schema, table, kind, column, dataType string
		if err = rows.Scan(&schema, &table, &kind, &column, &dataType); err == nil {
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
	}
	if err == nil {
		err = rows.Err()
	}
	return tables, err
}

func (d *mysqlDriver) Query(ctx context.Context, dsn, query string, maxRows int, timeout time.Duration) (*Result, error) {
	db, err := d.pool(dsn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
	defer cancel()

	// Read-only on the server too: the guard is the first line, the
	// transaction mode the second.
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("could not start a read-only transaction: %w", err)
	}
	// Read-only, so rolling back is how every transaction ends.
	defer func() { _ = tx.Rollback() }()

	// One error path from here on: the steps a server can fail share it with
	// the ones only a broken connection can.
	var rows *sql.Rows
	if err = setMySQLTimeout(ctx, tx, timeout); err == nil {
		rows, err = tx.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	res := &Result{Rows: [][]any{}}
	for _, t := range types {
		res.Columns = append(res.Columns, t.Name())
	}
	for err == nil && rows.Next() {
		if len(res.Rows) == maxRows {
			res.Truncated = true
			// Closing the rows would read the rest of the result first;
			// cancelling drops the connection instead.
			cancel()
			break
		}
		values := make([]any, len(types))
		dest := make([]any, len(types))
		for i := range values {
			dest[i] = &values[i]
		}
		if err = rows.Scan(dest...); err == nil {
			for i, v := range values {
				values[i] = mysqlValue(v, types[i].DatabaseTypeName())
			}
			res.Rows = append(res.Rows, values)
		}
	}
	if err == nil && !res.Truncated {
		err = rows.Err()
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

// setMySQLTimeout ends the transaction's queries on the server after timeout:
// max_execution_time on MySQL, max_statement_time on MariaDB.
func setMySQLTimeout(ctx context.Context, tx *sql.Tx, timeout time.Duration) error {
	_, err := tx.ExecContext(ctx, "set session max_execution_time = "+strconv.FormatInt(timeout.Milliseconds(), 10))
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == errUnknownSystemVariable {
		_, err = tx.ExecContext(ctx, "set session max_statement_time = "+strconv.FormatFloat(timeout.Seconds(), 'f', 3, 64))
	}
	return err
}

// mysqlValue is JSONValue, reading a JSON column as JSON rather than text.
func mysqlValue(v any, databaseType string) any {
	if b, ok := v.([]byte); ok && databaseType == "JSON" && json.Valid(b) {
		return json.RawMessage(b)
	}
	return JSONValue(v)
}

func (*mysqlDriver) Guard(pii []sqlguard.Column) Checker {
	return sqlguard.NewMySQL(pii)
}
