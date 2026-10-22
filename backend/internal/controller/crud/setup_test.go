package crud_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentrq/sql-mcp/backend/internal/controller/crud"
	"github.com/agentrq/sql-mcp/backend/internal/data/entity"
	"github.com/agentrq/sql-mcp/backend/internal/data/model"
	"github.com/agentrq/sql-mcp/backend/internal/service/auth"
	"github.com/agentrq/sql-mcp/backend/internal/testutil"
)

func setup(emails ...string) entity.Setup {
	return entity.Setup{AllowedEmails: emails, QueryMaxRows: 10, QueryInlineRows: 2, QueryTimeout: 5 * time.Second}
}

func TestSetupStartsFromTheEnvironment(t *testing.T) {
	env := testutil.New(t)
	s := env.Ctrl.Setup()
	assert.Equal(t, []string{"*@example.com", "root@sqlmcp.local"}, s.AllowedEmails)
	assert.Equal(t, 100, s.QueryMaxRows)
	assert.Equal(t, 5, s.QueryInlineRows)
	assert.Equal(t, time.Second, s.QueryTimeout)
	assert.False(t, s.Saved)
	require.NoError(t, env.Ctrl.LoadSetup(ctx), "nothing saved yet")
	assert.False(t, env.Ctrl.Setup().Saved)
}

func TestUpdateSetupAppliesLiveAndIsSealed(t *testing.T) {
	env := testutil.New(t)
	saved, err := env.Ctrl.UpdateSetup(ctx, setup(" *@agentrq.com ", "*@AGENTRQ.com", "", "re:^ops\\d+@example\\.org$"))
	require.NoError(t, err)
	assert.True(t, saved.Saved)
	assert.False(t, saved.UpdatedAt.IsZero())
	assert.Equal(t, []string{"*@agentrq.com", "re:^ops\\d+@example\\.org$"}, saved.AllowedEmails, "trimmed, blanks and duplicates dropped")

	_, err = env.Ctrl.SignIn(ctx, "dev@example.com", "", "")
	assert.ErrorIs(t, err, entity.ErrEmailNotAllowed, "the old allowlist is gone at once")
	_, err = env.Ctrl.SignIn(ctx, "ops7@example.org", "", "")
	require.NoError(t, err)
	u, err := env.Ctrl.SignIn(ctx, "dev@agentrq.com", "", "")
	require.NoError(t, err)

	// The new row limits apply to the next query.
	env.Driver.Rows = 50
	id := newDB(t, env, crud.ParseID(u.ID))
	res, err := env.Ctrl.RunQuery(ctx, crud.QueryRequest{UserID: crud.ParseID(u.ID), DatabaseID: id, SQL: "select id from orders", Source: model.SourceMCP})
	require.NoError(t, err)
	assert.Equal(t, 10, res.RowCount)
	assert.Len(t, res.Rows, 2)

	stored, err := env.Repo.GetSetting(ctx, "configuration.json")
	require.NoError(t, err)
	assert.NotContains(t, stored, "agentrq", "the configuration is encrypted at rest")
	plain, err := env.Sealer.Open(stored)
	require.NoError(t, err)
	assert.Contains(t, plain, `"allowedEmails":["*@agentrq.com"`)

	// A restart loads it back.
	envList, _ := auth.NewAllowlist([]string{"*@example.com"})
	fresh := crud.New(crud.Params{Repo: env.Repo, Sealer: env.Sealer, Allowlist: envList})
	require.NoError(t, fresh.LoadSetup(ctx))
	assert.Equal(t, saved, fresh.Setup())

	reset, err := env.Ctrl.ResetSetup(ctx)
	require.NoError(t, err)
	assert.False(t, reset.Saved)
	assert.Contains(t, reset.AllowedEmails, "*@example.com")
	_, err = env.Ctrl.SignIn(ctx, "dev@example.com", "", "")
	assert.NoError(t, err)
	require.NoError(t, env.Ctrl.LoadSetup(ctx))
	assert.False(t, env.Ctrl.Setup().Saved)
}

func TestUpdateSetupRefusesInvalid(t *testing.T) {
	env := testutil.New(t)
	for name, tc := range map[string]struct {
		in   entity.Setup
		want string
	}{
		"empty":        {setup(" ", ""), "at least one entry"},
		"long entry":   {setup(strings.Repeat("a", 321)), "at most 320"},
		"bad regex":    {setup("re:("), "not a valid pattern"},
		"max rows":     {entity.Setup{AllowedEmails: []string{"a@b.c"}, QueryMaxRows: 0, QueryInlineRows: 1, QueryTimeout: time.Second}, "max rows"},
		"inline rows":  {entity.Setup{AllowedEmails: []string{"a@b.c"}, QueryMaxRows: 5, QueryInlineRows: 6, QueryTimeout: time.Second}, "inline rows"},
		"timeout":      {entity.Setup{AllowedEmails: []string{"a@b.c"}, QueryMaxRows: 5, QueryInlineRows: 1, QueryTimeout: time.Hour}, "timeout"},
		"many entries": {setup(manyEntries(501)...), "at most 500"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := env.Ctrl.UpdateSetup(ctx, tc.in)
			var invalid *entity.InvalidError
			require.ErrorAs(t, err, &invalid)
			assert.Contains(t, invalid.Message, tc.want)
		})
	}
	assert.False(t, env.Ctrl.Setup().Saved, "nothing invalid is applied")
}

func manyEntries(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = strings.Repeat("x", i%7+1) + "@" + strings.Repeat("y", i/7+1) + ".com"
	}
	return out
}

func TestLoadSetupRefusesWhatItCannotRead(t *testing.T) {
	env := testutil.New(t)
	require.NoError(t, env.Repo.SetSetting(ctx, "configuration.json", "not-sealed"))
	assert.ErrorContains(t, env.Ctrl.LoadSetup(ctx), "cannot be decrypted")

	sealed, _ := env.Sealer.Seal("{")
	require.NoError(t, env.Repo.SetSetting(ctx, "configuration.json", sealed))
	assert.ErrorContains(t, env.Ctrl.LoadSetup(ctx), "corrupt")

	sealed, _ = env.Sealer.Seal(`{"allowedEmails":[],"queryMaxRows":1,"queryInlineRows":1,"queryTimeoutMs":1000}`)
	require.NoError(t, env.Repo.SetSetting(ctx, "configuration.json", sealed))
	assert.ErrorContains(t, env.Ctrl.LoadSetup(ctx), "invalid")
}

func TestCheckEmail(t *testing.T) {
	m, err := crud.CheckEmail([]string{"a@x.com", "*@agentrq.com"}, " Dev@AgentRQ.com ")
	require.NoError(t, err)
	assert.Equal(t, "*@agentrq.com", m)
	m, err = crud.CheckEmail([]string{"*@agentrq.com"}, "dev@agentrq.com.evil.com")
	require.NoError(t, err)
	assert.Empty(t, m)
	_, err = crud.CheckEmail([]string{"re:("}, "a@b.c")
	assert.Error(t, err)
}
