// Package repository reads and writes the app's tables. Every method that
// touches a person's data takes their user ID and scopes the query by it, so
// a caller cannot reach another person's rows by passing the wrong ID.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"github.com/agentrq/sql-mcp/backend/internal/data/model"
)

// ErrNotFound is returned for a row that does not exist or is not the caller's.
var ErrNotFound = errors.New("not found")

// Repository is the app database.
type Repository struct {
	db    *gorm.DB
	newID func() int64
}

// Open connects to the app database and migrates it.
func Open(driver, dsn string, newID func() int64) (*Repository, error) {
	var dialector gorm.Dialector
	switch driver {
	case "postgres":
		dialector = postgres.Open(dsn)
	default:
		dialector = sqlite.Open(dsn + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(model.All()...); err != nil {
		return nil, err
	}
	return &Repository{db: db, newID: newID}, nil
}

func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

// UpsertUser finds the user with email, creating them or refreshing their
// profile.
func (r *Repository) UpsertUser(ctx context.Context, email, name, picture string) (*model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).Where("email = ?", email).First(&u).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		u = model.User{ID: r.newID(), Email: email, Name: name, Picture: picture}
		return &u, r.db.WithContext(ctx).Create(&u).Error
	case err != nil:
		return nil, err
	}
	if name != "" && (u.Name != name || u.Picture != picture) {
		u.Name, u.Picture = name, picture
		err = r.db.WithContext(ctx).Model(&u).Updates(map[string]any{"name": name, "picture": picture}).Error
	}
	return &u, err
}

// GetUser returns one user.
func (r *Repository) GetUser(ctx context.Context, id int64) (*model.User, error) {
	var u model.User
	if err := r.db.WithContext(ctx).First(&u, "id = ?", id).Error; err != nil {
		return nil, notFound(err)
	}
	return &u, nil
}

// ListDatabases returns a person's databases, newest first.
func (r *Repository) ListDatabases(ctx context.Context, userID int64) ([]model.Database, error) {
	var out []model.Database
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at desc").Find(&out).Error
	return out, err
}

// GetDatabase returns one of a person's databases.
func (r *Repository) GetDatabase(ctx context.Context, userID, id int64) (*model.Database, error) {
	var d model.Database
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&d).Error; err != nil {
		return nil, notFound(err)
	}
	return &d, nil
}

// SystemGetDatabase returns a database whoever owns it, for the MCP server
// to learn whose it is before it checks the caller.
func (r *Repository) SystemGetDatabase(ctx context.Context, id int64) (*model.Database, error) {
	var d model.Database
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&d).Error; err != nil {
		return nil, notFound(err)
	}
	return &d, nil
}

// CreateDatabase stores a new database, assigning its ID.
func (r *Repository) CreateDatabase(ctx context.Context, d *model.Database) error {
	d.ID = r.newID()
	return r.db.WithContext(ctx).Create(d).Error
}

// UpdateDatabase saves a database's name and DSN.
func (r *Repository) UpdateDatabase(ctx context.Context, d *model.Database) error {
	res := r.db.WithContext(ctx).Model(&model.Database{}).Where("id = ? AND user_id = ?", d.ID, d.UserID).
		Updates(map[string]any{"name": d.Name, "dsn_sealed": d.DSNSealed, "dsn_masked": d.DSNMasked, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAgentMarksPII turns on or off whether a database's MCP agent may add
// PII columns.
func (r *Repository) SetAgentMarksPII(ctx context.Context, userID, id int64, on bool) error {
	res := r.db.WithContext(ctx).Model(&model.Database{}).Where("id = ? AND user_id = ?", id, userID).
		Updates(map[string]any{"agent_marks_pii": on, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteDatabase removes a database with its PII columns and query log.
func (r *Repository) DeleteDatabase(ctx context.Context, userID, id int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND user_id = ?", id, userID).Delete(&model.Database{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		if err := tx.Where("database_id = ?", id).Delete(&model.PIIColumn{}).Error; err != nil {
			return err
		}
		return tx.Where("database_id = ?", id).Delete(&model.QueryLog{}).Error
	})
}

// ListPIIColumns returns a database's PII columns. The caller has checked
// the database is theirs.
func (r *Repository) ListPIIColumns(ctx context.Context, databaseID int64) ([]model.PIIColumn, error) {
	var out []model.PIIColumn
	err := r.db.WithContext(ctx).Where("database_id = ?", databaseID).
		Order("schema, table_name, column_name").Find(&out).Error
	return out, err
}

// CountPIIColumns counts each database's PII columns.
func (r *Repository) CountPIIColumns(ctx context.Context, databaseIDs []int64) (map[int64]int, error) {
	out := map[int64]int{}
	if len(databaseIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		DatabaseID int64
		N          int
	}
	err := r.db.WithContext(ctx).Model(&model.PIIColumn{}).Select("database_id, count(*) as n").
		Where("database_id IN ?", databaseIDs).Group("database_id").Scan(&rows).Error
	for _, row := range rows {
		out[row.DatabaseID] = row.N
	}
	return out, err
}

// ReplacePIIColumns makes columns the database's whole PII set.
func (r *Repository) ReplacePIIColumns(ctx context.Context, databaseID int64, columns []model.PIIColumn) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("database_id = ?", databaseID).Delete(&model.PIIColumn{}).Error; err != nil {
			return err
		}
		for i := range columns {
			columns[i].ID = r.newID()
			columns[i].DatabaseID = databaseID
		}
		if len(columns) == 0 {
			return nil
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&columns, piiBatch).Error
	})
}

// piiBatch keeps one INSERT under SQLite's limit of 32766 bound variables
// (six per column), so the largest PII set still saves.
const piiBatch = 1000

// AddPIIColumns adds columns to the database's PII set, keeping every column
// already in it.
func (r *Repository) AddPIIColumns(ctx context.Context, databaseID int64, columns []model.PIIColumn) error {
	for i := range columns {
		columns[i].ID = r.newID()
		columns[i].DatabaseID = databaseID
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&columns, piiBatch).Error
}

// GetTmphost returns a person's tmphost connection.
func (r *Repository) GetTmphost(ctx context.Context, userID int64) (*model.TmphostConnection, error) {
	var c model.TmphostConnection
	if err := r.db.WithContext(ctx).First(&c, "user_id = ?", userID).Error; err != nil {
		return nil, notFound(err)
	}
	return &c, nil
}

// SaveTmphost creates or replaces a person's tmphost connection.
func (r *Repository) SaveTmphost(ctx context.Context, c *model.TmphostConnection) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"access_sealed", "refresh_sealed", "expires_at", "account", "created_at", "updated_at"}),
	}).Create(c).Error
}

// UpdateTmphostTokens saves refreshed tokens, keeping when it was connected.
func (r *Repository) UpdateTmphostTokens(ctx context.Context, c *model.TmphostConnection) error {
	return r.db.WithContext(ctx).Model(&model.TmphostConnection{}).Where("user_id = ?", c.UserID).
		Updates(map[string]any{"access_sealed": c.AccessSealed, "refresh_sealed": c.RefreshSealed, "expires_at": c.ExpiresAt, "updated_at": time.Now()}).Error
}

// GetSetting returns a server-wide value, "" when it is unset.
func (r *Repository) GetSetting(ctx context.Context, key string) (string, error) {
	var s model.Setting
	err := r.db.WithContext(ctx).First(&s, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return s.Value, err
}

// SetSetting stores a server-wide value.
func (r *Repository) SetSetting(ctx context.Context, key, value string) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&model.Setting{Key: key, Value: value, UpdatedAt: time.Now()}).Error
}

// DeleteTmphost removes a person's tmphost connection.
func (r *Repository) DeleteTmphost(ctx context.Context, userID int64) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&model.TmphostConnection{}).Error
}

// AddQueryLog records a query.
func (r *Repository) AddQueryLog(ctx context.Context, q *model.QueryLog) error {
	q.ID = r.newID()
	return r.db.WithContext(ctx).Create(q).Error
}

// ListQueryLogs returns a database's most recent queries.
func (r *Repository) ListQueryLogs(ctx context.Context, databaseID int64, limit int) ([]model.QueryLog, error) {
	var out []model.QueryLog
	err := r.db.WithContext(ctx).Where("database_id = ?", databaseID).
		Order("created_at desc, id desc").Limit(limit).Find(&out).Error
	return out, err
}
