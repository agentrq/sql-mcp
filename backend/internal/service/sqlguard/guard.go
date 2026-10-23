// Package sqlguard decides whether a query an agent sent may run against a
// database: it must be one read-only SELECT, and it must not read a column the
// owner marked as PII.
//
// The query is parsed by libpg_query, PostgreSQL's own parser (as WebAssembly,
// so the binary needs no cgo), and the whole
// tree is walked as JSON. Walking JSON rather than the typed nodes is the
// point: every node type the parser knows is covered, including ones added in
// a later PostgreSQL release, so a reference cannot hide inside a node this
// package forgot to visit.
//
// The check is conservative. When a reference cannot be tied to one table it
// is compared against every PII table the query names, and anything the guard
// does not understand is refused rather than allowed.
package sqlguard

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	pg "github.com/wasilibs/go-pgquery"
)

// Column is one PII column. Schema defaults to "public".
type Column struct {
	Schema string
	Table  string
	Column string
}

// Rejection is why a query was refused. Its message is safe to show the agent.
type Rejection struct {
	Reason string
}

func (r *Rejection) Error() string { return r.Reason }

// IsRejection reports whether err is a refusal rather than a failure.
func IsRejection(err error) bool {
	var r *Rejection
	return errors.As(err, &r)
}

func reject(format string, args ...any) error {
	return &Rejection{Reason: fmt.Sprintf(format, args...)}
}

// writeNodes are statements that change data or schema. Any of them anywhere
// in the tree — a data-modifying CTE included — refuses the query.
var writeNodes = map[string]string{
	"InsertStmt": "INSERT", "UpdateStmt": "UPDATE", "DeleteStmt": "DELETE",
	"MergeStmt": "MERGE", "CopyStmt": "COPY", "CallStmt": "CALL", "DoStmt": "DO",
}

// deniedFunctionPrefixes run SQL handed to them as text, or reach outside the
// database. Text SQL is invisible to the parser, so query_to_xml('select email
// from users', ...) would read a PII column without one appearing in the tree.
var deniedFunctionPrefixes = []string{
	"query_to_xml", "table_to_xml", "cursor_to_xml", "schema_to_xml", "database_to_xml",
	"dblink", "pg_read_file", "pg_read_binary_file", "pg_ls_", "pg_stat_file",
	"lo_", "set_config", "pg_terminate_backend", "pg_cancel_backend", "pg_reload_conf",
}

// Guard holds one database's PII columns.
type Guard struct {
	// pii maps a lower-case table name to its schemas' PII columns.
	pii map[string][]Column
}

// New is a guard for these PII columns.
func New(columns []Column) *Guard {
	return &Guard{pii: indexPII(columns, "public")}
}

// indexPII maps each lower-case table name to its PII columns, giving a
// column with no schema defaultSchema.
func indexPII(columns []Column, defaultSchema string) map[string][]Column {
	pii := map[string][]Column{}
	for _, c := range columns {
		c.Schema = strings.ToLower(c.Schema)
		if c.Schema == "" {
			c.Schema = defaultSchema
		}
		c.Table = strings.ToLower(c.Table)
		c.Column = strings.ToLower(c.Column)
		pii[c.Table] = append(pii[c.Table], c)
	}
	return pii
}

// relation is a table the query reads, as the query named it.
type relation struct {
	schema string // "" when the query left it to the search_path
	name   string
	alias  string
}

// ref is a column reference: qualifier.name, or qualifier.* when star.
type ref struct {
	qualifier []string // lower case; empty when unqualified
	name      string   // lower case; "" when star
	star      bool
}

// piiColumns are the PII columns of rel, every schema's when the query did
// not name one: the search_path decides which it reads, so all are assumed.
// A column marked with no schema matches every schema.
func piiColumns(pii map[string][]Column, rel relation) []Column {
	var out []Column
	for _, c := range pii[rel.name] {
		if rel.schema == "" || c.Schema == "" || rel.schema == c.Schema {
			out = append(out, c)
		}
	}
	return out
}

func hasColumn(cols []Column, name string) bool {
	return slices.ContainsFunc(cols, func(c Column) bool { return c.Column == name })
}

func label(rel relation) string {
	if rel.schema != "" {
		return rel.schema + "." + rel.name
	}
	return rel.name
}

// Check returns nil when sql may run, a *Rejection when it may not, and a
// plain error only when the parser itself fails in an unexpected way.
func (g *Guard) Check(sql string) error {
	if strings.TrimSpace(sql) == "" {
		return reject("the query is empty")
	}
	tree, err := pg.ParseToJSON(sql)
	if err != nil {
		return reject("the query could not be parsed: %v", err)
	}
	var root struct {
		Stmts []struct {
			Stmt map[string]any `json:"stmt"`
		} `json:"stmts"`
	}
	if err := json.Unmarshal([]byte(tree), &root); err != nil {
		return fmt.Errorf("sqlguard: decode parse tree: %w", err)
	}
	if len(root.Stmts) != 1 {
		return reject("send exactly one statement; this query has %d", len(root.Stmts))
	}
	stmt := root.Stmts[0].Stmt
	sel, ok := stmt["SelectStmt"].(map[string]any)
	if !ok {
		return reject("only SELECT queries are allowed")
	}
	if _, ok := sel["intoClause"]; ok {
		return reject("SELECT INTO creates a table and is not allowed")
	}

	w := &walker{}
	w.walk(stmt)
	if w.err != nil {
		return w.err
	}
	return checkRefs(g.pii, w.relations, w.refs, true)
}

// walker collects what the query reads, and stops at the first thing that
// refuses it outright.
type walker struct {
	relations []relation
	refs      []ref
	err       error
}

func (w *walker) walk(node any) {
	if w.err != nil {
		return
	}
	switch n := node.(type) {
	case []any:
		for _, item := range n {
			w.walk(item)
		}
	case map[string]any:
		for key, value := range n {
			if kind, ok := writeNodes[key]; ok {
				w.err = reject("%s is not allowed: queries are read-only", kind)
				return
			}
			switch key {
			case "lockingClause":
				w.err = reject("row locking (FOR UPDATE/SHARE) is not allowed")
				return
			case "RangeVar":
				w.addRelation(value)
			case "ColumnRef":
				w.addRef(value)
			case "FuncCall":
				if name := funcName(value); deniedFunction(name) {
					w.err = reject("the function %s is not allowed", name)
					return
				}
			}
			w.walk(value)
		}
	}
}

func (w *walker) addRelation(value any) {
	m, ok := value.(map[string]any)
	if !ok {
		return
	}
	rel := relation{
		schema: strings.ToLower(str(m["schemaname"])),
		name:   strings.ToLower(str(m["relname"])),
	}
	if alias, ok := m["alias"].(map[string]any); ok {
		rel.alias = strings.ToLower(str(alias["aliasname"]))
	}
	w.relations = append(w.relations, rel)
}

// addRef records a ColumnRef: its fields are String nodes, the last of which
// may be an A_Star.
func (w *walker) addRef(value any) {
	m, _ := value.(map[string]any)
	fields, _ := m["fields"].([]any)
	if len(fields) == 0 {
		return
	}
	var r ref
	for _, f := range fields[:len(fields)-1] {
		s, _ := stringNode(f)
		r.qualifier = append(r.qualifier, strings.ToLower(s))
	}
	last := fields[len(fields)-1]
	if isStar(last) {
		r.star = true
	} else if name, ok := stringNode(last); ok {
		r.name = strings.ToLower(name)
	} else {
		w.err = reject("the query has a column reference the guard cannot read")
		return
	}
	w.refs = append(w.refs, r)
}

func funcName(value any) string {
	m, _ := value.(map[string]any)
	parts, _ := m["funcname"].([]any)
	var names []string
	for _, p := range parts {
		if s, ok := stringNode(p); ok {
			names = append(names, strings.ToLower(s))
		}
	}
	return strings.Join(names, ".")
}

func deniedFunction(name string) bool {
	// pg_catalog.dblink is still dblink.
	base := name[strings.LastIndexByte(name, '.')+1:]
	return slices.ContainsFunc(deniedFunctionPrefixes, func(p string) bool { return strings.HasPrefix(base, p) })
}

// stringNode is the value of a {"String": {"sval": ...}} node.
func stringNode(node any) (string, bool) {
	m, ok := node.(map[string]any)
	if !ok {
		return "", false
	}
	s, ok := m["String"].(map[string]any)
	if !ok {
		return "", false
	}
	return str(s["sval"]), true
}

func isStar(node any) bool {
	m, ok := node.(map[string]any)
	if !ok {
		return false
	}
	_, ok = m["A_Star"]
	return ok
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// checkRefs refuses a column reference that reads PII: the column itself,
// a star over a table that has some, or, when wholeRows, the whole row of one
// (Postgres reads a bare table name or alias as its row).
func checkRefs(pii map[string][]Column, relations []relation, refs []ref, wholeRows bool) error {
	var piiRels []relation
	for _, rel := range relations {
		if len(piiColumns(pii, rel)) > 0 {
			piiRels = append(piiRels, rel)
		}
	}
	if len(piiRels) == 0 {
		return nil
	}

	for _, r := range refs {
		targets := resolve(piiRels, r.qualifier)

		if r.star {
			if len(r.qualifier) == 0 {
				return reject("SELECT * is not allowed on %s because it has PII columns; list the columns you need", label(piiRels[0]))
			}
			if len(targets) > 0 {
				return reject("%s.* is not allowed because %s has PII columns; list the columns you need", strings.Join(r.qualifier, "."), label(targets[0]))
			}
			// A subquery or CTE alias: its own select list was checked.
			continue
		}

		if len(r.qualifier) > 0 && len(targets) == 0 {
			// Qualified by something that is not a PII table (a subquery or
			// CTE alias): the name alone is compared, against every PII table.
			targets = piiRels
		}
		if len(r.qualifier) == 0 {
			if wholeRows {
				// The whole row of a PII table: to_json(u), row_to_json(users).
				for _, rel := range piiRels {
					if r.name == rel.alias || (rel.alias == "" && r.name == rel.name) {
						return reject("reading whole rows of %s is not allowed because it has PII columns", label(rel))
					}
				}
			}
			targets = piiRels
		}
		for _, rel := range targets {
			if hasColumn(piiColumns(pii, rel), r.name) {
				return reject("the column %s.%s is marked as PII and cannot be queried", label(rel), r.name)
			}
		}
	}
	return nil
}

// resolve finds the PII relations a qualifier names: an alias, a table name,
// or schema.table.
func resolve(rels []relation, qualifier []string) []relation {
	if len(qualifier) == 0 {
		return nil
	}
	table := qualifier[len(qualifier)-1]
	schema := ""
	if len(qualifier) >= 2 {
		schema = qualifier[len(qualifier)-2]
	}
	var out []relation
	for _, rel := range rels {
		switch {
		case rel.alias != "":
			if rel.alias == table && schema == "" {
				out = append(out, rel)
			}
		case rel.name == table && (schema == "" || rel.schema == "" || rel.schema == schema):
			out = append(out, rel)
		}
	}
	return out
}
