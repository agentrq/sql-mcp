// Package model is the app's own tables. Nothing here is part of the API:
// handlers answer with the view package, mapped from these.
package model

import "time"

// User is someone who signed in. IDs are monoflake IDs, shown as base62.
type User struct {
	ID        int64  `gorm:"primaryKey;autoIncrement:false"`
	Email     string `gorm:"uniqueIndex;not null"`
	Name      string
	Picture   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Database is a connection a person registered; it has its own MCP server.
type Database struct {
	ID     int64  `gorm:"primaryKey;autoIncrement:false"`
	UserID int64  `gorm:"index;not null"`
	Name   string `gorm:"not null"`
	Driver string `gorm:"not null"`
	// DSNSealed is the DSN, sealed with ENCRYPTION_KEY. It is opened only to
	// connect, and never returned by the API.
	DSNSealed string `gorm:"not null"`
	// DSNMasked is the DSN with its password replaced, safe to show.
	DSNMasked string
	// AgentMarksPII lets the database's MCP agent add PII columns. It can
	// never remove one; only the owner can.
	AgentMarksPII bool `gorm:"not null;default:false"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// PIIColumn is a column queries may not read.
type PIIColumn struct {
	ID         int64  `gorm:"primaryKey;autoIncrement:false"`
	DatabaseID int64  `gorm:"uniqueIndex:idx_pii_column;not null"`
	Schema     string `gorm:"uniqueIndex:idx_pii_column;not null"`
	Table      string `gorm:"column:table_name;uniqueIndex:idx_pii_column;not null"`
	Column     string `gorm:"column:column_name;uniqueIndex:idx_pii_column;not null"`
	CreatedAt  time.Time
}

// TmphostConnection is the OAuth grant a person gave this server on
// tmphost.io, where query results are stored temporarily. Both tokens are
// sealed with ENCRYPTION_KEY.
type TmphostConnection struct {
	UserID        int64  `gorm:"primaryKey;autoIncrement:false"`
	AccessSealed  string `gorm:"not null"`
	RefreshSealed string
	ExpiresAt     time.Time
	Account       string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Setting is one server-wide value, such as the client_id tmphost issued
// this server.
type Setting struct {
	Key       string `gorm:"primaryKey"`
	Value     string
	UpdatedAt time.Time
}

// Query statuses.
const (
	QueryOK       = "ok"
	QueryRejected = "rejected"
	QueryError    = "error"
)

// Query sources.
const (
	SourceMCP     = "mcp"
	SourceConsole = "console"
	SourceCLI     = "cli"
)

// QueryLog is one query someone ran, allowed or not.
type QueryLog struct {
	ID         int64 `gorm:"primaryKey;autoIncrement:false"`
	DatabaseID int64 `gorm:"index:idx_query_log_db_created,priority:1;not null"`
	UserID     int64 `gorm:"not null"`
	Source     string
	SQL        string
	Status     string
	Reason     string
	RowCount   int
	ResultURL  string
	DurationMs int64
	CreatedAt  time.Time `gorm:"index:idx_query_log_db_created,priority:2"`
}

// All is every table, for migration.
func All() []any {
	return []any{&User{}, &Database{}, &PIIColumn{}, &TmphostConnection{}, &QueryLog{}, &Setting{}}
}
