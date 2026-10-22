// Package crud is the app's business logic. Handlers call it and never the
// repository; every method that touches a person's data takes their user ID,
// and a database that is not theirs is reported as not found.
package crud

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/sql-mcp/backend/internal/data/entity"
	"github.com/agentrq/sql-mcp/backend/internal/data/model"
	"github.com/agentrq/sql-mcp/backend/internal/repository"
	"github.com/agentrq/sql-mcp/backend/internal/service/auth"
	"github.com/agentrq/sql-mcp/backend/internal/service/dbdriver"
	"github.com/agentrq/sql-mcp/backend/internal/service/security"
	"github.com/agentrq/sql-mcp/backend/internal/service/tmphost"
)

// Config is where the app is, and how queries run until an admin saves a
// configuration of their own.
type Config struct {
	BaseURL      string
	MaxRows      int
	InlineRows   int
	QueryTimeout time.Duration
}

// Params is what the controller needs. Allowlist is the environment's.
type Params struct {
	Repo      *repository.Repository
	Drivers   *dbdriver.Registry
	Sealer    *security.Sealer
	Allowlist *auth.Allowlist
	Tmphost   *tmphost.Client
	Config    Config
}

// Controller is the app's business logic.
type Controller struct {
	repo         *repository.Repository
	drivers      *dbdriver.Registry
	sealer       *security.Sealer
	envAllowlist *auth.Allowlist
	tmphost      *tmphost.Client
	cfg          Config
	current      atomic.Pointer[live]
	now          func() time.Time
}

// New is a controller running on the environment's configuration;
// LoadSetup puts a saved one in force.
func New(p Params) *Controller {
	c := &Controller{
		repo:         p.Repo,
		drivers:      p.Drivers,
		sealer:       p.Sealer,
		envAllowlist: p.Allowlist,
		tmphost:      p.Tmphost,
		cfg:          p.Config,
		now:          time.Now,
	}
	c.current.Store(c.envSetup())
	return c
}

// ParseID turns a base62 ID from a URL into the stored one, 0 when it is not
// one.
func ParseID(base62 string) int64 {
	if len(base62) != 11 {
		return 0
	}
	return monoflake.IDFromBase62(base62).Int64()
}

// FormatID is the base62 form of an ID.
func FormatID(id int64) string { return monoflake.ID(id).String() }

func mapErr(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return entity.ErrNotFound
	}
	return err
}

// ── Users ────────────────────────────────────────────────────────────────

// SignIn records someone a provider vouched for, if the allowlist admits them.
func (c *Controller) SignIn(ctx context.Context, email, name, picture string) (*entity.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !c.live().allowlist.Allows(email) {
		return nil, entity.ErrEmailNotAllowed
	}
	u, err := c.repo.UpsertUser(ctx, email, name, picture)
	if err != nil {
		return nil, err
	}
	return toUser(u), nil
}

// GetUser returns a signed-in person, refusing one the allowlist no longer
// admits: removing an address from ALLOWED_EMAILS ends its sessions at the
// next refresh.
func (c *Controller) GetUser(ctx context.Context, userID int64) (*entity.User, error) {
	u, err := c.repo.GetUser(ctx, userID)
	if err != nil {
		return nil, mapErr(err)
	}
	if !c.live().allowlist.Allows(u.Email) {
		return nil, entity.ErrEmailNotAllowed
	}
	return toUser(u), nil
}

func toUser(u *model.User) *entity.User {
	return &entity.User{ID: FormatID(u.ID), Email: u.Email, Name: u.Name, Picture: u.Picture}
}

// ── Databases ────────────────────────────────────────────────────────────

// Drivers lists every engine, available or not.
func (c *Controller) Drivers() []entity.Driver {
	var out []entity.Driver
	for _, d := range c.drivers.All() {
		out = append(out, entity.Driver{ID: d.ID(), Name: d.Name(), Available: d.Available()})
	}
	return out
}

// MCPURL is a database's MCP server.
func (c *Controller) MCPURL(id int64) string {
	return c.cfg.BaseURL + "/mcp/" + FormatID(id)
}

func (c *Controller) toDatabase(d *model.Database, piiCount int) *entity.Database {
	return &entity.Database{
		ID:        FormatID(d.ID),
		Name:      d.Name,
		Driver:    d.Driver,
		DSNMasked: d.DSNMasked,
		PIICount:  piiCount,
		MCPURL:    c.MCPURL(d.ID),
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
	}
}

// ListDatabases returns a person's databases.
func (c *Controller) ListDatabases(ctx context.Context, userID int64) ([]entity.Database, error) {
	rows, err := c.repo.ListDatabases(ctx, userID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(rows))
	for i, d := range rows {
		ids[i] = d.ID
	}
	counts, err := c.repo.CountPIIColumns(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]entity.Database, 0, len(rows))
	for i := range rows {
		out = append(out, *c.toDatabase(&rows[i], counts[rows[i].ID]))
	}
	return out, nil
}

// GetDatabase returns one of a person's databases.
func (c *Controller) GetDatabase(ctx context.Context, userID, id int64) (*entity.Database, error) {
	d, err := c.repo.GetDatabase(ctx, userID, id)
	if err != nil {
		return nil, mapErr(err)
	}
	counts, err := c.repo.CountPIIColumns(ctx, []int64{id})
	if err != nil {
		return nil, err
	}
	return c.toDatabase(d, counts[id]), nil
}

func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", entity.Invalid("name is required")
	}
	if utf8.RuneCountInString(name) > 80 {
		return "", entity.Invalid("name must be at most 80 characters")
	}
	if _, err := auth.ValidateDisplayName(name); err != nil {
		return "", entity.Invalid("name must not contain control characters")
	}
	return name, nil
}

func (c *Controller) driver(id string) (dbdriver.Driver, error) {
	d, err := c.drivers.Get(id)
	if err != nil {
		return nil, entity.Invalid(err.Error())
	}
	return d, nil
}

// TestConnection connects with a DSN, returning the server's version.
func (c *Controller) TestConnection(ctx context.Context, driverID, dsn string) (string, error) {
	d, err := c.driver(driverID)
	if err != nil {
		return "", err
	}
	if err := d.ValidateDSN(dsn); err != nil {
		return "", entity.Invalid(err.Error())
	}
	version, err := d.Test(ctx, dsn)
	if err != nil {
		return "", entity.Invalid(err.Error())
	}
	return version, nil
}

// CreateDatabase registers a database once its DSN connects.
func (c *Controller) CreateDatabase(ctx context.Context, userID int64, name, driverID, dsn string) (*entity.Database, error) {
	name, err := validateName(name)
	if err != nil {
		return nil, err
	}
	if _, err := c.TestConnection(ctx, driverID, dsn); err != nil {
		return nil, err
	}
	d, _ := c.drivers.Get(driverID)
	sealed, err := c.sealer.Seal(dsn)
	if err != nil {
		return nil, err
	}
	row := &model.Database{UserID: userID, Name: name, Driver: driverID, DSNSealed: sealed, DSNMasked: d.MaskDSN(dsn)}
	if err := c.repo.CreateDatabase(ctx, row); err != nil {
		return nil, err
	}
	return c.toDatabase(row, 0), nil
}

// UpdateDatabase renames a database and, when dsn is not empty, replaces its
// DSN once the new one connects.
func (c *Controller) UpdateDatabase(ctx context.Context, userID, id int64, name, dsn string) (*entity.Database, error) {
	row, err := c.repo.GetDatabase(ctx, userID, id)
	if err != nil {
		return nil, mapErr(err)
	}
	if row.Name, err = validateName(name); err != nil {
		return nil, err
	}
	if strings.TrimSpace(dsn) != "" {
		if _, err := c.TestConnection(ctx, row.Driver, dsn); err != nil {
			return nil, err
		}
		d, _ := c.drivers.Get(row.Driver)
		if row.DSNSealed, err = c.sealer.Seal(dsn); err != nil {
			return nil, err
		}
		row.DSNMasked = d.MaskDSN(dsn)
	}
	if err := c.repo.UpdateDatabase(ctx, row); err != nil {
		return nil, mapErr(err)
	}
	return c.GetDatabase(ctx, userID, id)
}

// DeleteDatabase removes a database; its MCP tokens stop working with it.
func (c *Controller) DeleteDatabase(ctx context.Context, userID, id int64) error {
	return mapErr(c.repo.DeleteDatabase(ctx, userID, id))
}

// open returns a person's database with its engine and opened DSN.
func (c *Controller) open(ctx context.Context, userID, id int64) (*model.Database, dbdriver.Driver, string, error) {
	row, err := c.repo.GetDatabase(ctx, userID, id)
	if err != nil {
		return nil, nil, "", mapErr(err)
	}
	d, err := c.driver(row.Driver)
	if err != nil {
		return nil, nil, "", err
	}
	dsn, err := c.sealer.Open(row.DSNSealed)
	if err != nil {
		return nil, nil, "", err
	}
	return row, d, dsn, nil
}

// ── Schema and PII ───────────────────────────────────────────────────────

func piiKey(schema, table, column string) string {
	return strings.ToLower(schema) + "\x00" + strings.ToLower(table) + "\x00" + strings.ToLower(column)
}

// Schema lists a database's tables and columns, marking the PII ones.
func (c *Controller) Schema(ctx context.Context, userID, id int64) ([]entity.SchemaTable, error) {
	_, d, dsn, err := c.open(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	tables, err := d.Schema(ctx, dsn)
	if err != nil {
		return nil, &entity.QueryFailedError{Message: err.Error()}
	}
	pii, err := c.repo.ListPIIColumns(ctx, id)
	if err != nil {
		return nil, err
	}
	marked := map[string]bool{}
	for _, p := range pii {
		marked[piiKey(p.Schema, p.Table, p.Column)] = true
	}
	out := make([]entity.SchemaTable, 0, len(tables))
	for _, t := range tables {
		st := entity.SchemaTable{Schema: t.Schema, Name: t.Name, Kind: t.Kind}
		for _, col := range t.Columns {
			st.Columns = append(st.Columns, entity.SchemaColumn{
				Name: col.Name, DataType: col.DataType, SuggestedPII: col.SuggestedPII,
				PII: marked[piiKey(t.Schema, t.Name, col.Name)],
			})
		}
		out = append(out, st)
	}
	return out, nil
}

// ListPII returns a database's PII columns.
func (c *Controller) ListPII(ctx context.Context, userID, id int64) ([]entity.PIIColumn, error) {
	if _, err := c.repo.GetDatabase(ctx, userID, id); err != nil {
		return nil, mapErr(err)
	}
	rows, err := c.repo.ListPIIColumns(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]entity.PIIColumn, 0, len(rows))
	for _, p := range rows {
		out = append(out, entity.PIIColumn{Schema: p.Schema, Table: p.Table, Column: p.Column})
	}
	return out, nil
}

// maxPIIColumns caps one database's PII set.
const maxPIIColumns = 10000

// SetPII replaces a database's PII columns.
func (c *Controller) SetPII(ctx context.Context, userID, id int64, columns []entity.PIIColumn) ([]entity.PIIColumn, error) {
	if _, err := c.repo.GetDatabase(ctx, userID, id); err != nil {
		return nil, mapErr(err)
	}
	if len(columns) > maxPIIColumns {
		return nil, entity.Invalid("too many PII columns")
	}
	seen := map[string]bool{}
	var rows []model.PIIColumn
	for _, col := range columns {
		schema := strings.TrimSpace(col.Schema)
		if schema == "" {
			schema = "public"
		}
		table, column := strings.TrimSpace(col.Table), strings.TrimSpace(col.Column)
		if table == "" || column == "" {
			return nil, entity.Invalid("every PII column needs a table and a column")
		}
		if len(schema) > 128 || len(table) > 128 || len(column) > 128 {
			return nil, entity.Invalid("schema, table and column names must be at most 128 characters")
		}
		key := piiKey(schema, table, column)
		if seen[key] {
			continue
		}
		seen[key] = true
		rows = append(rows, model.PIIColumn{Schema: schema, Table: table, Column: column})
	}
	if err := c.repo.ReplacePIIColumns(ctx, id, rows); err != nil {
		return nil, err
	}
	return c.ListPII(ctx, userID, id)
}
