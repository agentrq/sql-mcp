// Package view is the REST API's JSON: the only structs that define what the
// API sends and accepts. Every field is camelCase.
package view

import (
	"time"

	"github.com/agentrq/sql-mcp/backend/internal/data/entity"
)

type Error struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

type AuthConfig struct {
	GoogleLoginEnabled bool   `json:"googleLoginEnabled"`
	RootLoginEnabled   bool   `json:"rootLoginEnabled"`
	BasePath           string `json:"basePath"`
}

type RootLoginRequest struct {
	RootToken string `json:"rootToken"`
}

type Status struct {
	Status string `json:"status"`
}

type User struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

type Driver struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

type DriversResponse struct {
	Drivers []Driver `json:"drivers"`
}

type Database struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Driver         string    `json:"driver"`
	DSNMasked      string    `json:"dsnMasked"`
	PIIColumnCount int       `json:"piiColumnCount"`
	AgentMarksPII  bool      `json:"agentMarksPii"`
	MCPURL         string    `json:"mcpUrl"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type DatabaseInput struct {
	Name   string `json:"name"`
	Driver string `json:"driver"`
	DSN    string `json:"dsn"`
}

type DatabaseRequest struct {
	Database DatabaseInput `json:"database"`
}

type DatabaseResponse struct {
	Database Database `json:"database"`
}

type DatabasesResponse struct {
	Databases []Database `json:"databases"`
}

type TestConnectionResponse struct {
	OK            bool   `json:"ok"`
	ServerVersion string `json:"serverVersion"`
}

type PIIColumn struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	Column string `json:"column"`
}

type PIIColumnsRequest struct {
	PIIColumns []PIIColumn `json:"piiColumns"`
}

type AgentPIIRequest struct {
	AgentMarksPII bool `json:"agentMarksPii"`
}

type PIIColumnsResponse struct {
	PIIColumns []PIIColumn `json:"piiColumns"`
}

type SchemaColumn struct {
	Name         string `json:"name"`
	DataType     string `json:"dataType"`
	PII          bool   `json:"pii"`
	SuggestedPII bool   `json:"suggestedPii"`
}

type SchemaTable struct {
	Schema  string         `json:"schema"`
	Name    string         `json:"name"`
	Kind    string         `json:"kind"`
	Columns []SchemaColumn `json:"columns"`
}

type SchemaResponse struct {
	Tables []SchemaTable `json:"tables"`
}

type QueryRequest struct {
	SQL string `json:"sql"`
}

type QueryResponse struct {
	Columns    []string `json:"columns"`
	Rows       [][]any  `json:"rows"`
	RowCount   int      `json:"rowCount"`
	Truncated  bool     `json:"truncated"`
	DurationMs int64    `json:"durationMs"`
}

type QueryLog struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	SQL        string    `json:"sql"`
	Status     string    `json:"status"`
	Reason     string    `json:"reason"`
	RowCount   int       `json:"rowCount"`
	ResultURL  string    `json:"resultUrl"`
	DurationMs int64     `json:"durationMs"`
	CreatedAt  time.Time `json:"createdAt"`
}

type QueryLogsResponse struct {
	Queries []QueryLog `json:"queries"`
}

type TmphostStatus struct {
	Connected   bool       `json:"connected"`
	Account     string     `json:"account,omitempty"`
	ServerURL   string     `json:"serverUrl"`
	ConnectedAt *time.Time `json:"connectedAt,omitempty"`
}

// ── Mapping ───────────────────────────────────────────────────────────────

func FromUser(u *entity.User) User {
	return User{ID: u.ID, Email: u.Email, Name: u.Name, Picture: u.Picture}
}

func FromDrivers(in []entity.Driver) DriversResponse {
	out := DriversResponse{Drivers: []Driver{}}
	for _, d := range in {
		out.Drivers = append(out.Drivers, Driver{ID: d.ID, Name: d.Name, Available: d.Available})
	}
	return out
}

func FromDatabase(d *entity.Database) Database {
	return Database{
		ID: d.ID, Name: d.Name, Driver: d.Driver, DSNMasked: d.DSNMasked, PIIColumnCount: d.PIICount,
		AgentMarksPII: d.AgentMarksPII, MCPURL: d.MCPURL, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

func FromDatabases(in []entity.Database) DatabasesResponse {
	out := DatabasesResponse{Databases: []Database{}}
	for i := range in {
		out.Databases = append(out.Databases, FromDatabase(&in[i]))
	}
	return out
}

func FromPIIColumns(in []entity.PIIColumn) PIIColumnsResponse {
	out := PIIColumnsResponse{PIIColumns: []PIIColumn{}}
	for _, c := range in {
		out.PIIColumns = append(out.PIIColumns, PIIColumn{Schema: c.Schema, Table: c.Table, Column: c.Column})
	}
	return out
}

func ToPIIColumns(in []PIIColumn) []entity.PIIColumn {
	out := make([]entity.PIIColumn, 0, len(in))
	for _, c := range in {
		out = append(out, entity.PIIColumn{Schema: c.Schema, Table: c.Table, Column: c.Column})
	}
	return out
}

func FromSchema(in []entity.SchemaTable) SchemaResponse {
	out := SchemaResponse{Tables: []SchemaTable{}}
	for _, t := range in {
		st := SchemaTable{Schema: t.Schema, Name: t.Name, Kind: t.Kind, Columns: []SchemaColumn{}}
		for _, c := range t.Columns {
			st.Columns = append(st.Columns, SchemaColumn{Name: c.Name, DataType: c.DataType, PII: c.PII, SuggestedPII: c.SuggestedPII})
		}
		out.Tables = append(out.Tables, st)
	}
	return out
}

func FromQueryResult(r *entity.QueryResult) QueryResponse {
	rows := r.Rows
	if rows == nil {
		rows = [][]any{}
	}
	cols := r.Columns
	if cols == nil {
		cols = []string{}
	}
	return QueryResponse{Columns: cols, Rows: rows, RowCount: r.RowCount, Truncated: r.Truncated, DurationMs: r.DurationMs}
}

func FromQueryLogs(in []entity.QueryLog) QueryLogsResponse {
	out := QueryLogsResponse{Queries: []QueryLog{}}
	for _, q := range in {
		out.Queries = append(out.Queries, FromQueryLog(q))
	}
	return out
}

func FromQueryLog(q entity.QueryLog) QueryLog {
	return QueryLog{
		ID: q.ID, Source: q.Source, SQL: q.SQL, Status: q.Status, Reason: q.Reason,
		RowCount: q.RowCount, ResultURL: q.ResultURL, DurationMs: q.DurationMs, CreatedAt: q.CreatedAt,
	}
}

func FromTmphostStatus(s *entity.TmphostStatus) TmphostStatus {
	out := TmphostStatus{Connected: s.Connected, Account: s.Account, ServerURL: s.ServerURL}
	if s.Connected {
		t := s.ConnectedAt
		out.ConnectedAt = &t
	}
	return out
}

// ── Admin ────────────────────────────────────────────────────────────────

type AdminConfig struct {
	Enabled bool `json:"enabled"`
}

type AdminSession struct {
	Token     string    `json:"token,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// AdminServer is how the server was started; read-only.
type AdminServer struct {
	BaseURL            string `json:"baseUrl"`
	SSLEnabled         bool   `json:"sslEnabled"`
	Domain             string `json:"domain"`
	HTTPSPort          int    `json:"httpsPort"`
	GoogleLoginEnabled bool   `json:"googleLoginEnabled"`
	RootLoginEnabled   bool   `json:"rootLoginEnabled"`
	AppDBDriver        string `json:"appDbDriver"`
	TmphostAPIURL      string `json:"tmphostApiUrl"`
}

type AdminSettings struct {
	AllowedEmails       []string    `json:"allowedEmails"`
	QueryMaxRows        int         `json:"queryMaxRows"`
	QueryInlineRows     int         `json:"queryInlineRows"`
	QueryTimeoutSeconds int         `json:"queryTimeoutSeconds"`
	Source              string      `json:"source"`
	UpdatedAt           *time.Time  `json:"updatedAt,omitempty"`
	Server              AdminServer `json:"server"`
}

type AdminSettingsRequest struct {
	AllowedEmails       []string `json:"allowedEmails"`
	QueryMaxRows        int      `json:"queryMaxRows"`
	QueryInlineRows     int      `json:"queryInlineRows"`
	QueryTimeoutSeconds int      `json:"queryTimeoutSeconds"`
}

type CheckEmailRequest struct {
	Email         string   `json:"email"`
	AllowedEmails []string `json:"allowedEmails"`
}

type CheckEmailResponse struct {
	Allowed      bool   `json:"allowed"`
	MatchedEntry string `json:"matchedEntry,omitempty"`
}

// FromSetup is the settings page's document; Source is "saved" once an
// admin saved a configuration, "env" while the environment's is in force.
func FromSetup(s entity.Setup, server AdminServer) AdminSettings {
	out := AdminSettings{
		AllowedEmails:       s.AllowedEmails,
		QueryMaxRows:        s.QueryMaxRows,
		QueryInlineRows:     s.QueryInlineRows,
		QueryTimeoutSeconds: int(s.QueryTimeout / time.Second),
		Source:              "env",
		Server:              server,
	}
	if out.AllowedEmails == nil {
		out.AllowedEmails = []string{}
	}
	if s.Saved {
		out.Source = "saved"
		updated := s.UpdatedAt
		out.UpdatedAt = &updated
	}
	return out
}

// ToSetup is the configuration an admin sent.
func (r AdminSettingsRequest) ToSetup() entity.Setup {
	return entity.Setup{
		AllowedEmails:   r.AllowedEmails,
		QueryMaxRows:    r.QueryMaxRows,
		QueryInlineRows: r.QueryInlineRows,
		QueryTimeout:    time.Duration(r.QueryTimeoutSeconds) * time.Second,
	}
}
