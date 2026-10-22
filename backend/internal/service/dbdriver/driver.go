// Package dbdriver connects to the databases people register: testing a DSN,
// reading the schema, and running a query the guard allowed.
package dbdriver

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/agentrq/sql-mcp/backend/internal/service/sqlguard"
)

// Column is one column of a table, as the schema reports it.
type Column struct {
	Name         string
	DataType     string
	SuggestedPII bool
}

// Table is a table or view.
type Table struct {
	Schema  string
	Name    string
	Kind    string // "table" or "view"
	Columns []Column
}

// Result is what a query returned, at most the row limit it was run with.
type Result struct {
	Columns   []string
	Rows      [][]any
	Truncated bool
}

// Checker refuses a query that may not run.
type Checker interface {
	Check(sql string) error
}

// Driver is one database engine.
type Driver interface {
	ID() string
	Name() string
	// Available is false for an engine that is listed but not supported yet.
	Available() bool
	ValidateDSN(dsn string) error
	MaskDSN(dsn string) string
	Test(ctx context.Context, dsn string) (serverVersion string, err error)
	Schema(ctx context.Context, dsn string) ([]Table, error)
	// Query runs sql in a read-only transaction that ends within timeout,
	// returning at most maxRows rows.
	Query(ctx context.Context, dsn, sql string, maxRows int, timeout time.Duration) (*Result, error)
	Guard(pii []sqlguard.Column) Checker
}

// ErrUnavailable is returned for an engine that is not supported yet.
var ErrUnavailable = errors.New("this database engine is not available yet")

// Registry is every engine, in the order the app lists them.
type Registry struct {
	drivers []Driver
}

// NewRegistry lists drivers.
func NewRegistry(drivers ...Driver) *Registry {
	return &Registry{drivers: drivers}
}

// All returns every engine.
func (r *Registry) All() []Driver { return r.drivers }

// Get returns an available engine by ID.
func (r *Registry) Get(id string) (Driver, error) {
	for _, d := range r.drivers {
		if d.ID() == id {
			if !d.Available() {
				return nil, ErrUnavailable
			}
			return d, nil
		}
	}
	return nil, errors.New("unknown database engine")
}

// piiNamePattern matches column names that usually hold personal data. It
// only suggests: the person decides what is PII.
var piiNamePattern = regexp.MustCompile(`(?i)(^|_)(e_?mail|phone|mobile|msisdn|ssn|social_security|tax_?id|national_?id|passport|driver_?licen[cs]e|birth|dob|birthday|address|street|city|zip|postal|post_?code|first_?name|last_?name|full_?name|surname|middle_?name|ip_?addr(ess)?|ip|credit_?card|card_?number|cc_?num|iban|account_?number|password|passwd|token|secret|salary|gender|latitude|longitude|geo|location)($|_)`)

// SuggestPII reports whether a column's name looks like it holds PII.
func SuggestPII(column string) bool {
	return piiNamePattern.MatchString(strings.TrimSpace(column))
}

// mysqlDriver is listed so the app can show it, but cannot be used yet.
type mysqlDriver struct{}

// NewMySQL is the not-yet-available MySQL engine.
func NewMySQL() Driver { return mysqlDriver{} }

func (mysqlDriver) ID() string                      { return "mysql" }
func (mysqlDriver) Name() string                    { return "MySQL" }
func (mysqlDriver) Available() bool                 { return false }
func (mysqlDriver) ValidateDSN(string) error        { return ErrUnavailable }
func (mysqlDriver) MaskDSN(dsn string) string       { return "" }
func (mysqlDriver) Guard([]sqlguard.Column) Checker { return denyAll{} }
func (mysqlDriver) Test(context.Context, string) (string, error) {
	return "", ErrUnavailable
}
func (mysqlDriver) Schema(context.Context, string) ([]Table, error) { return nil, ErrUnavailable }
func (mysqlDriver) Query(context.Context, string, string, int, time.Duration) (*Result, error) {
	return nil, ErrUnavailable
}

type denyAll struct{}

func (denyAll) Check(string) error { return ErrUnavailable }
