// Package entity is what the controller hands the handlers: the app's data
// as the API and the MCP server need it, before view shapes it into JSON.
package entity

import (
	"errors"
	"time"
)

// Errors the handlers map to a status.
var (
	ErrNotFound        = errors.New("not found")
	ErrEmailNotAllowed = errors.New("this email is not allowed to sign in")
)

// InvalidError is a request the caller can fix; its message says how.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

// Invalid is an InvalidError.
func Invalid(message string) error { return &InvalidError{Message: message} }

// RejectedError is a query refused before it ran; its message says why.
type RejectedError struct{ Reason string }

func (e *RejectedError) Error() string { return e.Reason }

// QueryFailedError is a query the database itself refused or failed.
type QueryFailedError struct{ Message string }

func (e *QueryFailedError) Error() string { return e.Message }

// User is someone signed in.
type User struct {
	ID      string
	Email   string
	Name    string
	Picture string
}

// Driver is a database engine the app lists.
type Driver struct {
	ID        string
	Name      string
	Available bool
}

// Database is a registered database, without its DSN.
type Database struct {
	ID        string
	Name      string
	Driver    string
	DSNMasked string
	PIICount  int
	MCPURL    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PIIColumn is a column queries may not read.
type PIIColumn struct {
	Schema string
	Table  string
	Column string
}

// SchemaColumn is a column, and whether it is or looks like PII.
type SchemaColumn struct {
	Name         string
	DataType     string
	PII          bool
	SuggestedPII bool
}

// SchemaTable is a table or view with its columns.
type SchemaTable struct {
	Schema  string
	Name    string
	Kind    string
	Columns []SchemaColumn
}

// QueryResult is what a query returned.
type QueryResult struct {
	Columns   []string
	Rows      [][]any
	RowCount  int
	Truncated bool
	// ResultURL is the tmphost file holding every row, when it was stored.
	ResultURL       string
	ResultExpiresAt time.Time
	// StoreError says why storing on tmphost failed; the rows still came back.
	StoreError string
	DurationMs int64
}

// QueryLog is one query someone ran.
type QueryLog struct {
	ID         string
	Source     string
	SQL        string
	Status     string
	Reason     string
	RowCount   int
	ResultURL  string
	DurationMs int64
	CreatedAt  time.Time
}

// TmphostStatus is whether a person connected tmphost.
type TmphostStatus struct {
	Connected   bool
	Account     string
	ServerURL   string
	ConnectedAt time.Time
}

// Setup is the configuration an admin changes at runtime. Once saved it
// overrides the environment; Saved is false while the environment's values
// are in force.
type Setup struct {
	AllowedEmails   []string
	QueryMaxRows    int
	QueryInlineRows int
	QueryTimeout    time.Duration
	Saved           bool
	UpdatedAt       time.Time
}
