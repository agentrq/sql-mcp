package crud_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/agentrq/sql-mcp/backend/internal/controller/crud"
	"github.com/agentrq/sql-mcp/backend/internal/data/entity"
	"github.com/agentrq/sql-mcp/backend/internal/data/model"
	"github.com/agentrq/sql-mcp/backend/internal/service/dbdriver"
	"github.com/agentrq/sql-mcp/backend/internal/testutil"
)

var ctx = context.Background()

func signIn(t *testing.T, env *testutil.Env, email string) int64 {
	t.Helper()
	u, err := env.Ctrl.SignIn(ctx, email, "Dev", "")
	require.NoError(t, err)
	return crud.ParseID(u.ID)
}

func newDB(t *testing.T, env *testutil.Env, userID int64) int64 {
	t.Helper()
	d, err := env.Ctrl.CreateDatabase(ctx, userID, " shop ", "postgres", testutil.GoodDSN)
	require.NoError(t, err)
	return crud.ParseID(d.ID)
}

func TestSignInAndGetUser(t *testing.T) {
	env := testutil.New(t)
	_, err := env.Ctrl.SignIn(ctx, "intruder@evil.com", "x", "")
	assert.ErrorIs(t, err, entity.ErrEmailNotAllowed)

	u, err := env.Ctrl.SignIn(ctx, " Dev@Example.com ", "Dev", "p.png")
	require.NoError(t, err)
	assert.Equal(t, "dev@example.com", u.Email)

	again, err := env.Ctrl.SignIn(ctx, "dev@example.com", "Dev Renamed", "q.png")
	require.NoError(t, err)
	assert.Equal(t, u.ID, again.ID)

	got, err := env.Ctrl.GetUser(ctx, crud.ParseID(u.ID))
	require.NoError(t, err)
	assert.Equal(t, "Dev Renamed", got.Name)

	_, err = env.Ctrl.GetUser(ctx, 12345)
	assert.ErrorIs(t, err, entity.ErrNotFound)

	// An address dropped from the allowlist loses its session at refresh.
	_, err = env.Repo.UpsertUser(ctx, "old@gone.com", "Old", "")
	require.NoError(t, err)
	old, _ := env.Repo.UpsertUser(ctx, "old@gone.com", "", "")
	_, err = env.Ctrl.GetUser(ctx, old.ID)
	assert.ErrorIs(t, err, entity.ErrEmailNotAllowed)
}

func TestParseFormatID(t *testing.T) {
	assert.Equal(t, int64(0), crud.ParseID("short"))
	id := int64(987654321 << 22)
	assert.Equal(t, id, crud.ParseID(crud.FormatID(id)))
}

func TestDrivers(t *testing.T) {
	env := testutil.New(t)
	ds := env.Ctrl.Drivers()
	require.Len(t, ds, 2)
	assert.True(t, ds[0].Available)
	assert.Equal(t, "mysql", ds[1].ID)
	assert.True(t, ds[1].Available)
}

func TestDatabaseLifecycle(t *testing.T) {
	env := testutil.New(t)
	alice := signIn(t, env, "alice@example.com")
	bob := signIn(t, env, "bob@example.com")

	for name, tc := range map[string]struct{ name, driver, dsn, want string }{
		"no name":       {"", "postgres", testutil.GoodDSN, "name is required"},
		"long name":     {strings.Repeat("x", 81), "postgres", testutil.GoodDSN, "at most 80"},
		"control char":  {"a\u202eb", "postgres", testutil.GoodDSN, "control characters"},
		"bad mysql dsn": {"m", "mysql", "x", "not a valid MySQL"},
		"unknown":       {"m", "oracle", "x", "unknown database engine"},
		"bad dsn":       {"m", "postgres", "mysql://x", "not a valid"},
		"unreachable":   {"m", "postgres", "postgres://x", "connection refused"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := env.Ctrl.CreateDatabase(ctx, alice, tc.name, tc.driver, tc.dsn)
			var invalid *entity.InvalidError
			require.ErrorAs(t, err, &invalid)
			assert.Contains(t, invalid.Error(), tc.want)
		})
	}

	d, err := env.Ctrl.CreateDatabase(ctx, alice, "shop", "postgres", testutil.GoodDSN)
	require.NoError(t, err)
	assert.Equal(t, "postgres://app:****@db.internal:5432/shop", d.DSNMasked)
	assert.Equal(t, "http://sqlmcp.test/mcp/"+d.ID, d.MCPURL)
	id := crud.ParseID(d.ID)

	stored, err := env.Repo.GetDatabase(ctx, alice, id)
	require.NoError(t, err)
	assert.NotContains(t, stored.DSNSealed, "secret", "the DSN is sealed at rest")

	list, err := env.Ctrl.ListDatabases(ctx, alice)
	require.NoError(t, err)
	require.Len(t, list, 1)
	empty, err := env.Ctrl.ListDatabases(ctx, bob)
	require.NoError(t, err)
	assert.Empty(t, empty)

	_, err = env.Ctrl.GetDatabase(ctx, bob, id)
	assert.ErrorIs(t, err, entity.ErrNotFound, "another person's database is not found")

	up, err := env.Ctrl.UpdateDatabase(ctx, alice, id, "shop-2", "")
	require.NoError(t, err)
	assert.Equal(t, "shop-2", up.Name)
	_, err = env.Ctrl.UpdateDatabase(ctx, alice, id, "shop-3", "postgres://bad")
	assert.Error(t, err)
	_, err = env.Ctrl.UpdateDatabase(ctx, alice, id, "", "")
	assert.Error(t, err)
	_, err = env.Ctrl.UpdateDatabase(ctx, bob, id, "mine", "")
	assert.ErrorIs(t, err, entity.ErrNotFound)
	up, err = env.Ctrl.UpdateDatabase(ctx, alice, id, "shop-4", testutil.GoodDSN)
	require.NoError(t, err)
	assert.Equal(t, "shop-4", up.Name)

	owner, err := env.Ctrl.SystemDatabaseOwner(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, crud.DatabaseOwner{UserID: alice, Name: "shop-4", Driver: "postgres"}, owner)
	_, err = env.Ctrl.SystemDatabaseOwner(ctx, 1)
	assert.ErrorIs(t, err, entity.ErrNotFound)

	version, err := env.Ctrl.TestConnection(ctx, "postgres", testutil.GoodDSN)
	require.NoError(t, err)
	assert.Equal(t, "PostgreSQL 18.0", version)

	assert.ErrorIs(t, env.Ctrl.DeleteDatabase(ctx, bob, id), entity.ErrNotFound)
	require.NoError(t, env.Ctrl.DeleteDatabase(ctx, alice, id))
	assert.ErrorIs(t, env.Ctrl.DeleteDatabase(ctx, alice, id), entity.ErrNotFound)
}

func TestSchemaAndPII(t *testing.T) {
	env := testutil.New(t)
	alice := signIn(t, env, "alice@example.com")
	bob := signIn(t, env, "bob@example.com")
	id := newDB(t, env, alice)

	cols, err := env.Ctrl.SetPII(ctx, alice, id, []entity.PIIColumn{
		{Table: "users", Column: "email"},
		{Schema: "public", Table: "USERS", Column: "EMAIL"}, // the same column
		{Schema: "billing", Table: "cards", Column: "number"},
	})
	require.NoError(t, err)
	require.Len(t, cols, 2)

	tables, err := env.Ctrl.Schema(ctx, alice, id)
	require.NoError(t, err)
	require.Len(t, tables, 2)
	assert.True(t, tables[0].Columns[1].PII)
	assert.True(t, tables[0].Columns[1].SuggestedPII)
	assert.False(t, tables[0].Columns[0].PII)

	list, err := env.Ctrl.ListDatabases(ctx, alice)
	require.NoError(t, err)
	assert.Equal(t, 2, list[0].PIICount)

	_, err = env.Ctrl.Schema(ctx, bob, id)
	assert.ErrorIs(t, err, entity.ErrNotFound)
	_, err = env.Ctrl.ListPII(ctx, bob, id)
	assert.ErrorIs(t, err, entity.ErrNotFound)
	_, err = env.Ctrl.SetPII(ctx, bob, id, nil)
	assert.ErrorIs(t, err, entity.ErrNotFound)

	_, err = env.Ctrl.SetPII(ctx, alice, id, []entity.PIIColumn{{Table: "users"}})
	assert.Error(t, err)
	_, err = env.Ctrl.SetPII(ctx, alice, id, []entity.PIIColumn{{Table: strings.Repeat("t", 129), Column: "c"}})
	assert.Error(t, err)
	_, err = env.Ctrl.SetPII(ctx, alice, id, make([]entity.PIIColumn, 10001))
	assert.Error(t, err)

	cleared, err := env.Ctrl.SetPII(ctx, alice, id, nil)
	require.NoError(t, err)
	assert.Empty(t, cleared)

	env.Driver.SchemaErr = errors.New("permission denied")
	_, err = env.Ctrl.Schema(ctx, alice, id)
	var failed *entity.QueryFailedError
	assert.ErrorAs(t, err, &failed)
}

func TestAgentMarksPII(t *testing.T) {
	env := testutil.New(t)
	alice := signIn(t, env, "alice@example.com")
	bob := signIn(t, env, "bob@example.com")
	id := newDB(t, env, alice)

	_, err := env.Ctrl.AgentMarkPII(ctx, alice, id, "", "users", []string{"email"})
	assert.ErrorIs(t, err, crud.ErrAgentMarksPIIOff, "off until the owner turns it on")

	_, err = env.Ctrl.SetAgentMarksPII(ctx, bob, id, true)
	assert.ErrorIs(t, err, entity.ErrNotFound)
	d, err := env.Ctrl.SetAgentMarksPII(ctx, alice, id, true)
	require.NoError(t, err)
	assert.True(t, d.AgentMarksPII)
	owner, err := env.Ctrl.SystemDatabaseOwner(ctx, id)
	require.NoError(t, err)
	assert.True(t, owner.AgentMarksPII)

	_, err = env.Ctrl.AgentMarkPII(ctx, bob, id, "", "users", []string{"email"})
	assert.ErrorIs(t, err, entity.ErrNotFound)

	_, err = env.Ctrl.SetPII(ctx, alice, id, []entity.PIIColumn{{Table: "orders", Column: "total"}})
	require.NoError(t, err)

	got, err := env.Ctrl.AgentMarkPII(ctx, alice, id, "", "USERS", []string{" EMAIL ", "email"})
	require.NoError(t, err)
	assert.Equal(t, &entity.PIIMarked{Table: "public.users", Marked: []string{"email"}, Already: []string{}}, got)
	got, err = env.Ctrl.AgentMarkPII(ctx, alice, id, "public", "orders", []string{"total", "id"})
	require.NoError(t, err)
	assert.Equal(t, &entity.PIIMarked{Table: "public.orders", Marked: []string{"id"}, Already: []string{"total"}}, got)
	got, err = env.Ctrl.AgentMarkPII(ctx, alice, id, "", "orders", []string{"total"})
	require.NoError(t, err)
	assert.Empty(t, got.Marked, "nothing new to mark")

	cols, err := env.Ctrl.ListPII(ctx, alice, id)
	require.NoError(t, err)
	assert.Equal(t, []entity.PIIColumn{
		{Schema: "public", Table: "orders", Column: "id"},
		{Schema: "public", Table: "orders", Column: "total"},
		{Schema: "public", Table: "users", Column: "email"},
	}, cols, "the agent only adds; the owner's column stays")

	env.Driver.Extra = []dbdriver.Table{{Schema: "billing", Name: "users", Kind: "table", Columns: []dbdriver.Column{{Name: "plan"}}}}
	for name, tc := range map[string]struct {
		schema, table string
		columns       []string
		want          string
	}{
		"no table":     {"", " ", []string{"email"}, "needs a table"},
		"no columns":   {"", "users", nil, "at least one column"},
		"unknown":      {"", "nope", []string{"email"}, "there is no table nope"},
		"wrong schema": {"billing", "orders", []string{"id"}, "there is no table orders"},
		"ambiguous":    {"", "users", []string{"email"}, "more than one schema"},
		"no column":    {"public", "users", []string{"email", "ssn"}, "public.users has no column ssn"},
	} {
		_, err := env.Ctrl.AgentMarkPII(ctx, alice, id, tc.schema, tc.table, tc.columns)
		var invalid *entity.InvalidError
		require.ErrorAs(t, err, &invalid, name)
		assert.Contains(t, invalid.Message, tc.want, name)
	}
	got, err = env.Ctrl.AgentMarkPII(ctx, alice, id, "billing", "users", []string{"plan"})
	require.NoError(t, err)
	assert.Equal(t, "billing.users", got.Table)

	env.Driver.SchemaErr = errors.New("permission denied")
	_, err = env.Ctrl.AgentMarkPII(ctx, alice, id, "", "orders", []string{"id"})
	var failed *entity.QueryFailedError
	assert.ErrorAs(t, err, &failed)
	env.Driver.SchemaErr = nil

	d, err = env.Ctrl.SetAgentMarksPII(ctx, alice, id, false)
	require.NoError(t, err)
	assert.False(t, d.AgentMarksPII)
	_, err = env.Ctrl.AgentMarkPII(ctx, alice, id, "", "users", []string{"id"})
	assert.ErrorIs(t, err, crud.ErrAgentMarksPIIOff)
}

func TestAgentPIIWhenTheStoreFails(t *testing.T) {
	env := testutil.New(t)
	alice := signIn(t, env, "alice@example.com")
	id := newDB(t, env, alice)
	_, err := env.Ctrl.SetAgentMarksPII(ctx, alice, id, true)
	require.NoError(t, err)

	db, err := gorm.Open(sqlite.Open(env.DBPath), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TRIGGER no_pii BEFORE INSERT ON pii_columns BEGIN SELECT RAISE(FAIL, 'read-only'); END").Error)
	_, err = env.Ctrl.AgentMarkPII(ctx, alice, id, "", "users", []string{"email"})
	assert.ErrorContains(t, err, "read-only")

	require.NoError(t, db.Exec("DROP TABLE pii_columns").Error)
	_, err = env.Ctrl.AgentMarkPII(ctx, alice, id, "", "users", []string{"email"})
	assert.Error(t, err)

	require.NoError(t, db.Exec("DROP TABLE databases").Error)
	_, err = env.Ctrl.SetAgentMarksPII(ctx, alice, id, false)
	assert.ErrorContains(t, err, "no such table")
}

func TestTooManyAgentPIIColumns(t *testing.T) {
	env := testutil.New(t)
	alice := signIn(t, env, "alice@example.com")
	id := newDB(t, env, alice)
	_, err := env.Ctrl.SetAgentMarksPII(ctx, alice, id, true)
	require.NoError(t, err)
	full := make([]entity.PIIColumn, 10000)
	for i := range full {
		full[i] = entity.PIIColumn{Table: "t", Column: "c" + strconv.Itoa(i)}
	}
	_, err = env.Ctrl.SetPII(ctx, alice, id, full)
	require.NoError(t, err)
	_, err = env.Ctrl.AgentMarkPII(ctx, alice, id, "", "users", []string{"email"})
	assert.ErrorContains(t, err, "too many PII columns")
}

func TestRunQuery(t *testing.T) {
	env := testutil.New(t)
	alice := signIn(t, env, "alice@example.com")
	id := newDB(t, env, alice)
	_, err := env.Ctrl.SetPII(ctx, alice, id, []entity.PIIColumn{{Table: "users", Column: "email"}})
	require.NoError(t, err)

	_, err = env.Ctrl.RunQuery(ctx, crud.QueryRequest{UserID: alice, DatabaseID: id, SQL: "select email from users", Source: model.SourceConsole})
	var rejected *entity.RejectedError
	require.ErrorAs(t, err, &rejected)
	assert.Contains(t, rejected.Reason, "users.email")

	res, err := env.Ctrl.RunQuery(ctx, crud.QueryRequest{UserID: alice, DatabaseID: id, SQL: "select id from users", Source: model.SourceConsole})
	require.NoError(t, err)
	assert.Equal(t, 3, res.RowCount)

	long := "select id from users -- " + strings.Repeat("x", 30000)
	_, err = env.Ctrl.RunQuery(ctx, crud.QueryRequest{UserID: alice, DatabaseID: id, SQL: long, Source: model.SourceConsole})
	require.NoError(t, err)

	env.Driver.QueryErr = errors.New(`relation "nope" does not exist`)
	_, err = env.Ctrl.RunQuery(ctx, crud.QueryRequest{UserID: alice, DatabaseID: id, SQL: "select 1 from nope", Source: model.SourceConsole})
	var failed *entity.QueryFailedError
	require.ErrorAs(t, err, &failed)

	_, err = env.Ctrl.RunQuery(ctx, crud.QueryRequest{UserID: alice + 1, DatabaseID: id, SQL: "select 1"})
	assert.ErrorIs(t, err, entity.ErrNotFound)

	logs, err := env.Ctrl.ListQueries(ctx, alice, id, 0)
	require.NoError(t, err)
	require.Len(t, logs, 4)
	assert.Equal(t, model.QueryError, logs[0].Status)
	assert.Len(t, logs[1].SQL, 20000, "a long statement is logged cut")
	assert.Equal(t, model.QueryOK, logs[2].Status)
	assert.Equal(t, model.QueryRejected, logs[3].Status)
	_, err = env.Ctrl.ListQueries(ctx, alice+1, id, 10)
	assert.ErrorIs(t, err, entity.ErrNotFound)
}

func TestMCPQueryStoresLargeResultsOnTmphost(t *testing.T) {
	env := testutil.New(t)
	alice := signIn(t, env, "alice@example.com")
	id := newDB(t, env, alice)
	env.Driver.Rows = 12

	rq := crud.QueryRequest{UserID: alice, DatabaseID: id, SQL: "select id from orders", Source: model.SourceMCP}
	res, err := env.Ctrl.RunQuery(ctx, rq)
	require.NoError(t, err)
	assert.Len(t, res.Rows, 5, "cut to the inline limit")
	assert.Equal(t, 12, res.RowCount)
	assert.Contains(t, res.StoreError, "not connected")

	connect(t, env, alice)
	res, err = env.Ctrl.RunQuery(ctx, rq)
	require.NoError(t, err)
	assert.Contains(t, res.ResultURL, "https://f1.file.tmphost.test/result-")
	assert.False(t, res.ResultExpiresAt.IsZero())
	assert.Equal(t, int32(1), env.Tmphost.Uploads.Load())
	assert.True(t, strings.HasPrefix(env.Tmphost.LastBody, "id\n1\n2\n"), env.Tmphost.LastBody)

	logs, _ := env.Ctrl.ListQueries(ctx, alice, id, 1)
	assert.Equal(t, res.ResultURL, logs[0].ResultURL)

	// Small results stay inline unless asked.
	env.Driver.Rows = 2
	res, err = env.Ctrl.RunQuery(ctx, rq)
	require.NoError(t, err)
	assert.Empty(t, res.ResultURL)
	rq.Store = true
	res, err = env.Ctrl.RunQuery(ctx, rq)
	require.NoError(t, err)
	assert.NotEmpty(t, res.ResultURL)

	env.Tmphost.FailUpload.Store(true)
	res, err = env.Ctrl.RunQuery(ctx, rq)
	require.NoError(t, err, "a failed upload still returns the rows")
	assert.Contains(t, res.StoreError, "insufficient credits")
}

// connect runs the tmphost OAuth flow for userID against the fake server.
func connect(t *testing.T, env *testutil.Env, userID int64) {
	t.Helper()
	authURL, flow, err := env.Ctrl.TmphostConnect(ctx, userID)
	require.NoError(t, err)
	u, err := url.Parse(authURL)
	require.NoError(t, err)
	q := u.Query()
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.Equal(t, "http://sqlmcp.test/api/v1/settings/tmphost/callback", q.Get("redirect_uri"))
	require.NoError(t, env.Ctrl.TmphostCallback(ctx, crud.TmphostCallbackRequest{
		UserID: userID, Flow: flow, State: q.Get("state"), Code: "good-code", Issuer: env.Tmphost.URL,
	}))
}

func TestTmphostConnect(t *testing.T) {
	env := testutil.New(t)
	alice := signIn(t, env, "alice@example.com")
	bob := signIn(t, env, "bob@example.com")

	st, err := env.Ctrl.TmphostStatus(ctx, alice)
	require.NoError(t, err)
	assert.False(t, st.Connected)
	assert.Equal(t, env.Tmphost.URL, st.ServerURL)

	authURL, flow, err := env.Ctrl.TmphostConnect(ctx, alice)
	require.NoError(t, err)
	u, _ := url.Parse(authURL)
	state := u.Query().Get("state")
	assert.NotContains(t, flow, state, "the flow cookie is sealed")

	for name, rq := range map[string]crud.TmphostCallbackRequest{
		"denied":       {UserID: alice, Error: "access_denied"},
		"server error": {UserID: alice, Error: "server_error", ErrorDescription: "boom"},
		"bad flow":     {UserID: alice, Flow: "garbage", State: state, Code: "good-code"},
		"wrong state":  {UserID: alice, Flow: flow, State: "other", Code: "good-code"},
		"other user":   {UserID: bob, Flow: flow, State: state, Code: "good-code"},
		"wrong issuer": {UserID: alice, Flow: flow, State: state, Code: "good-code", Issuer: "https://evil.test"},
		"bad code":     {UserID: alice, Flow: flow, State: state, Code: "bad"},
	} {
		t.Run(name, func(t *testing.T) {
			var invalid *entity.InvalidError
			assert.ErrorAs(t, env.Ctrl.TmphostCallback(ctx, rq), &invalid)
		})
	}

	connect(t, env, alice)
	assert.Equal(t, int32(1), env.Tmphost.Registered.Load(), "the client registers once and is reused")
	st, err = env.Ctrl.TmphostStatus(ctx, alice)
	require.NoError(t, err)
	assert.True(t, st.Connected)
	assert.Equal(t, "dev@tmphost.test", st.Account)

	conn, err := env.Repo.GetTmphost(ctx, alice)
	require.NoError(t, err)
	assert.NotContains(t, conn.AccessSealed, "access-1", "tokens are sealed at rest")

	require.NoError(t, env.Ctrl.DisconnectTmphost(ctx, alice))
	st, _ = env.Ctrl.TmphostStatus(ctx, alice)
	assert.False(t, st.Connected)
}

func TestTmphostRefreshesAnExpiringToken(t *testing.T) {
	env := testutil.New(t)
	alice := signIn(t, env, "alice@example.com")
	id := newDB(t, env, alice)
	connect(t, env, alice)

	conn, err := env.Repo.GetTmphost(ctx, alice)
	require.NoError(t, err)
	conn.ExpiresAt = time.Now().Add(-time.Minute)
	require.NoError(t, env.Repo.UpdateTmphostTokens(ctx, conn))

	res, err := env.Ctrl.RunQuery(ctx, crud.QueryRequest{UserID: alice, DatabaseID: id, SQL: "select id from orders", Source: model.SourceMCP, Store: true})
	require.NoError(t, err)
	assert.NotEmpty(t, res.ResultURL)
	assert.Equal(t, int32(1), env.Tmphost.Refreshed.Load())

	conn, _ = env.Repo.GetTmphost(ctx, alice)
	assert.True(t, conn.ExpiresAt.After(time.Now()))
	refresh, err := env.Sealer.Open(conn.RefreshSealed)
	require.NoError(t, err)
	assert.Equal(t, "refresh-1", refresh, "a refresh that returns no new refresh token keeps the old one")

	// A revoked grant asks the person to connect again.
	env.Tmphost.FailRefresh.Store(true)
	conn.ExpiresAt = time.Now().Add(-time.Minute)
	require.NoError(t, env.Repo.UpdateTmphostTokens(ctx, conn))
	res, err = env.Ctrl.RunQuery(ctx, crud.QueryRequest{UserID: alice, DatabaseID: id, SQL: "select id from orders", Source: model.SourceMCP, Store: true})
	require.NoError(t, err)
	assert.Contains(t, res.StoreError, "could not be refreshed")

	// Without a refresh token an expired grant has to be connected again.
	conn.RefreshSealed, conn.ExpiresAt = "", time.Now().Add(-time.Minute)
	require.NoError(t, env.Repo.SaveTmphost(ctx, conn))
	res, err = env.Ctrl.RunQuery(ctx, crud.QueryRequest{UserID: alice, DatabaseID: id, SQL: "select id from orders", Source: model.SourceMCP, Store: true})
	require.NoError(t, err)
	assert.Contains(t, res.StoreError, "connect tmphost again")
}

func TestToCSV(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	out, err := crud.ToCSV([]string{"a", "b", "c"}, [][]any{
		{nil, "x,y", json.RawMessage(`{"k":1}`)},
		{when, 1.5, []int{1}},
		{"short"},
	})
	require.NoError(t, err)
	assert.Equal(t, "a,b,c\n,\"x,y\",\"{\"\"k\"\":1}\"\n2026-01-02T03:04:05Z,1.5,[1]\nshort,,\n", string(out))
	out, err = crud.ToCSV([]string{"f"}, [][]any{{func() {}}})
	require.NoError(t, err)
	assert.Contains(t, string(out), "0x")
}

func TestWatchQueries(t *testing.T) {
	env := testutil.New(t)
	alice := signIn(t, env, "alice@example.com")
	bob := signIn(t, env, "bob@example.com")
	id := newDB(t, env, alice)

	_, _, err := env.Ctrl.WatchQueries(ctx, bob, id)
	assert.ErrorIs(t, err, entity.ErrNotFound)

	queries, stop, err := env.Ctrl.WatchQueries(ctx, alice, id)
	require.NoError(t, err)
	_, err = env.Ctrl.RunQuery(ctx, crud.QueryRequest{UserID: alice, DatabaseID: id, SQL: "select id from users", Source: model.SourceMCP})
	require.NoError(t, err)
	q := <-queries
	logs, err := env.Ctrl.ListQueries(ctx, alice, id, 1)
	require.NoError(t, err)
	assert.Equal(t, logs[0].ID, q.ID, "a watcher sees what the log lists")
	assert.True(t, logs[0].CreatedAt.Equal(q.CreatedAt))
	assert.Equal(t, "select id from users", q.SQL)
	assert.Equal(t, model.QueryOK, q.Status)

	require.NoError(t, env.Ctrl.DeleteDatabase(ctx, alice, id))
	_, open := <-queries
	assert.False(t, open, "deleting the database ends the watch")
	stop()
}

func TestAQueryThatCannotBeLoggedIsNotWatched(t *testing.T) {
	env := testutil.New(t)
	alice := signIn(t, env, "alice@example.com")
	id := newDB(t, env, alice)
	queries, stop, err := env.Ctrl.WatchQueries(ctx, alice, id)
	require.NoError(t, err)
	defer stop()

	db, err := gorm.Open(sqlite.Open(env.DBPath), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("DROP TABLE query_logs").Error)

	_, err = env.Ctrl.RunQuery(ctx, crud.QueryRequest{UserID: alice, DatabaseID: id, SQL: "select id from users", Source: model.SourceConsole})
	require.NoError(t, err, "a failure to log does not fail the query")
	assert.Empty(t, queries)
}
