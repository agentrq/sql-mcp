package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/agentrq/sql-mcp/backend/internal/data/entity"
	"github.com/agentrq/sql-mcp/backend/internal/service/auth"
)

// setupKey is where the admin's configuration is stored, sealed with
// ENCRYPTION_KEY: the allowlist says who works where.
const setupKey = "configuration.json"

// Limits on what an admin can set.
const (
	maxAllowlistEntries = 500
	maxAllowlistEntry   = 320
	maxQueryRowsLimit   = 100000
	minQueryTimeout     = time.Second
	maxQueryTimeout     = 10 * time.Minute
)

// live is the configuration in force, swapped whole so a request never sees
// half of an update.
type live struct {
	setup     entity.Setup
	allowlist *auth.Allowlist
}

// storedSetup is the sealed JSON document.
type storedSetup struct {
	AllowedEmails   []string  `json:"allowedEmails"`
	QueryMaxRows    int       `json:"queryMaxRows"`
	QueryInlineRows int       `json:"queryInlineRows"`
	QueryTimeoutMs  int64     `json:"queryTimeoutMs"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func (c *Controller) live() *live { return c.current.Load() }

// envSetup is the configuration the environment gives.
func (c *Controller) envSetup() *live {
	return &live{
		setup: entity.Setup{
			AllowedEmails:   c.envAllowlist.Entries(),
			QueryMaxRows:    c.cfg.MaxRows,
			QueryInlineRows: c.cfg.InlineRows,
			QueryTimeout:    c.cfg.QueryTimeout,
		},
		allowlist: c.envAllowlist,
	}
}

// LoadSetup puts the saved configuration in force, when there is one. A
// saved configuration that cannot be opened stops the server rather than
// silently falling back to the environment's allowlist.
func (c *Controller) LoadSetup(ctx context.Context) error {
	sealed, err := c.repo.GetSetting(ctx, setupKey)
	if err != nil {
		return err
	}
	if sealed == "" {
		c.current.Store(c.envSetup())
		return nil
	}
	raw, err := c.sealer.Open(sealed)
	if err != nil {
		return fmt.Errorf("the saved configuration cannot be decrypted with ENCRYPTION_KEY: %w", err)
	}
	var st storedSetup
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return fmt.Errorf("the saved configuration is corrupt: %w", err)
	}
	l, err := newLive(entity.Setup{
		AllowedEmails:   st.AllowedEmails,
		QueryMaxRows:    st.QueryMaxRows,
		QueryInlineRows: st.QueryInlineRows,
		QueryTimeout:    time.Duration(st.QueryTimeoutMs) * time.Millisecond,
		Saved:           true,
		UpdatedAt:       st.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("the saved configuration is invalid: %w", err)
	}
	c.current.Store(l)
	return nil
}

// Setup is the configuration in force.
func (c *Controller) Setup() entity.Setup {
	s := c.live().setup
	s.AllowedEmails = append([]string(nil), s.AllowedEmails...)
	return s
}

// UpdateSetup validates, saves and applies an admin's configuration.
func (c *Controller) UpdateSetup(ctx context.Context, in entity.Setup) (entity.Setup, error) {
	in.Saved, in.UpdatedAt = true, c.now().UTC()
	l, err := newLive(in)
	if err != nil {
		return entity.Setup{}, err
	}
	raw, err := json.Marshal(storedSetup{
		AllowedEmails:   l.setup.AllowedEmails,
		QueryMaxRows:    l.setup.QueryMaxRows,
		QueryInlineRows: l.setup.QueryInlineRows,
		QueryTimeoutMs:  l.setup.QueryTimeout.Milliseconds(),
		UpdatedAt:       l.setup.UpdatedAt,
	})
	if err != nil {
		return entity.Setup{}, err
	}
	sealed, err := c.sealer.Seal(string(raw))
	if err != nil {
		return entity.Setup{}, err
	}
	if err := c.repo.SetSetting(ctx, setupKey, sealed); err != nil {
		return entity.Setup{}, err
	}
	c.current.Store(l)
	return c.Setup(), nil
}

// ResetSetup drops the saved configuration, back to the environment's.
func (c *Controller) ResetSetup(ctx context.Context) (entity.Setup, error) {
	if err := c.repo.SetSetting(ctx, setupKey, ""); err != nil {
		return entity.Setup{}, err
	}
	c.current.Store(c.envSetup())
	return c.Setup(), nil
}

// CheckEmail reports which entry of allowedEmails admits email, "" for none.
func CheckEmail(allowedEmails []string, email string) (string, error) {
	a, err := auth.NewAllowlist(allowedEmails)
	if err != nil {
		return "", &entity.InvalidError{Message: err.Error()}
	}
	return a.Match(strings.ToLower(strings.TrimSpace(email))), nil
}

func newLive(s entity.Setup) (*live, error) {
	invalid := func(format string, args ...any) (*live, error) {
		return nil, &entity.InvalidError{Message: fmt.Sprintf(format, args...)}
	}
	var entries []string
	seen := map[string]bool{}
	for _, e := range s.AllowedEmails {
		e = strings.TrimSpace(e)
		if e == "" || seen[strings.ToLower(e)] {
			continue
		}
		if len(e) > maxAllowlistEntry {
			return invalid("an allowed email entry is at most %d characters", maxAllowlistEntry)
		}
		seen[strings.ToLower(e)] = true
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return invalid("allowed emails needs at least one entry, or nobody can sign in")
	}
	if len(entries) > maxAllowlistEntries {
		return invalid("at most %d allowed email entries", maxAllowlistEntries)
	}
	allowlist, err := auth.NewAllowlist(entries)
	if err != nil {
		return invalid("%s", strings.TrimPrefix(err.Error(), "ALLOWED_EMAILS: "))
	}
	if s.QueryMaxRows < 1 || s.QueryMaxRows > maxQueryRowsLimit {
		return invalid("max rows must be between 1 and %d", maxQueryRowsLimit)
	}
	if s.QueryInlineRows < 1 || s.QueryInlineRows > s.QueryMaxRows {
		return invalid("inline rows must be between 1 and max rows")
	}
	if s.QueryTimeout < minQueryTimeout || s.QueryTimeout > maxQueryTimeout {
		return invalid("the query timeout must be between %s and %s", minQueryTimeout, maxQueryTimeout)
	}
	s.AllowedEmails = entries
	return &live{setup: s, allowlist: allowlist}, nil
}
