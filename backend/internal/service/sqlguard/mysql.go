package sqlguard

import (
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/ast"

	// The parser needs a driver for literal values; this is the standalone one.
	_ "github.com/pingcap/tidb/pkg/parser/test_driver"
)

// MySQLGuard is Guard for MySQL and MariaDB, on TiDB's MySQL parser (pure Go).
// It applies the same rules. Like Guard, it walks the whole tree rather than
// trusting the parser's visitor: by reflection, through every exported field,
// because Accept skips some children (a SELECT list's * is one).
//
// It parses the way the server reads the query only when the session's
// sql_mode has neither ANSI_QUOTES ("x" would be a column, not a string) nor
// NO_BACKSLASH_ESCAPES (a \' would end a string early). The MySQL driver sets
// sql_mode on every connection for that reason.
type MySQLGuard struct {
	pii map[string][]Column
}

// NewMySQL is a MySQL guard for these PII columns. A column with no schema is
// matched in every schema (database).
func NewMySQL(columns []Column) *MySQLGuard {
	return &MySQLGuard{pii: indexPII(columns, "")}
}

// executableComment is /*! ... */ (MySQL, MariaDB), /*M! ... */ (MariaDB) or
// /*T! ... */ (TiDB): text the server may run as SQL. Whether it does depends
// on the server's version, which the guard cannot know, and a comment that is
// skipped ends at a different */ than one that is run, so it is refused.
var executableComment = regexp.MustCompile(`(?i)/\*[mt]?!`)

// deniedMySQLFunctions read server files or hold the connection or locks.
var deniedMySQLFunctions = []string{
	"load_file", "sleep", "benchmark",
	"get_lock", "release_lock", "release_all_locks", "is_free_lock", "is_used_lock",
	"master_pos_wait", "source_pos_wait", "wait_for_executed_gtid_set", "wait_until_sql_thread_after_gtids",
	"sys_exec", "sys_eval",
}

// Check returns nil when sql may run and a *Rejection when it may not.
func (g *MySQLGuard) Check(sql string) error {
	if strings.TrimSpace(sql) == "" {
		return reject("the query is empty")
	}
	if executableComment.MatchString(sql) {
		return reject("executable comments (/*! ... */) are not allowed")
	}
	stmts, _, err := parser.New().ParseSQL(sql)
	if err != nil {
		return reject("the query could not be parsed: %v", err)
	}
	if len(stmts) != 1 {
		return reject("send exactly one statement; this query has %d", len(stmts))
	}
	switch stmts[0].(type) {
	case *ast.SelectStmt, *ast.SetOprStmt:
	default:
		return reject("only SELECT queries are allowed")
	}

	v := &mysqlVisitor{aliased: map[*ast.TableName]bool{}, seen: map[visit]bool{}}
	v.walk(reflect.ValueOf(stmts[0]))
	if v.err != nil {
		return v.err
	}
	return checkRefs(g.pii, v.relations, v.refs, false)
}

// mysqlVisitor collects what the query reads, and stops at the first thing
// that refuses it outright.
type mysqlVisitor struct {
	relations []relation
	refs      []ref
	// aliased are the tables a TableSource already recorded with their alias.
	aliased map[*ast.TableName]bool
	seen    map[visit]bool
	err     error
}

// visit is a struct already walked: its address and type, since an embedded
// first field shares its parent's address.
type visit struct {
	addr uintptr
	typ  reflect.Type
}

// walk visits every node reachable from x, each once, parents before their
// children.
func (v *mysqlVisitor) walk(x reflect.Value) {
	if v.err != nil {
		return
	}
	switch x.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !x.IsNil() {
			v.walk(x.Elem())
		}
	case reflect.Slice, reflect.Array:
		for i := range x.Len() {
			v.walk(x.Index(i))
		}
	case reflect.Map:
		for it := x.MapRange(); it.Next(); {
			v.walk(it.Value())
		}
	case reflect.Struct:
		if x.CanAddr() {
			key := visit{x.UnsafeAddr(), x.Type()}
			if v.seen[key] {
				return
			}
			v.seen[key] = true
			if n, ok := x.Addr().Interface().(ast.Node); ok {
				v.enter(n)
			}
		}
		for i := range x.NumField() {
			if x.Type().Field(i).IsExported() {
				v.walk(x.Field(i))
			}
		}
	}
}

func (v *mysqlVisitor) enter(n ast.Node) {
	switch n := n.(type) {
	case *ast.InsertStmt, *ast.UpdateStmt, *ast.DeleteStmt:
		v.err = reject("only SELECT queries are allowed: queries are read-only")
	case *ast.SelectStmt:
		switch {
		case n.SelectIntoOpt != nil:
			v.err = reject("SELECT INTO is not allowed")
		case n.LockInfo != nil && n.LockInfo.LockType != ast.SelectLockNone:
			v.err = reject("row locking (FOR UPDATE/SHARE) is not allowed")
		case n.Kind == ast.SelectStmtKindTable:
			// TABLE users is SELECT * FROM users.
			v.refs = append(v.refs, ref{star: true})
		}
	case *ast.TableSource:
		if t, ok := n.Source.(*ast.TableName); ok {
			v.addRelation(t, n.AsName.L)
			v.aliased[t] = true
		}
	case *ast.TableName:
		if !v.aliased[n] {
			v.addRelation(n, "")
		}
	case *ast.ColumnName:
		v.refs = append(v.refs, ref{qualifier: qualifier(n.Schema.L, n.Table.L), name: n.Name.L})
	case *ast.WildCardField:
		v.refs = append(v.refs, ref{qualifier: qualifier(n.Schema.L, n.Table.L), star: true})
	case *ast.FuncCallExpr:
		if slices.Contains(deniedMySQLFunctions, n.FnName.L) {
			v.err = reject("the function %s is not allowed", n.FnName.L)
		}
	}
}

func (v *mysqlVisitor) addRelation(t *ast.TableName, alias string) {
	v.relations = append(v.relations, relation{schema: t.Schema.L, name: t.Name.L, alias: alias})
}

// qualifier is [schema, table], [table] or none.
func qualifier(schema, table string) []string {
	switch {
	case schema != "":
		return []string{schema, table}
	case table != "":
		return []string{table}
	}
	return nil
}
