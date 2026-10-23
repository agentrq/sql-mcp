package sqlguard

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testGuard() *Guard {
	return New([]Column{
		{Table: "users", Column: "email"},
		{Schema: "public", Table: "Users", Column: "Phone"},
		{Schema: "billing", Table: "cards", Column: "number"},
	})
}

func TestCheckAllows(t *testing.T) {
	g := testGuard()
	for _, sql := range []string{
		"select id, created_at from users",
		"select u.id from users u where u.id > 10 order by 1",
		"select count(*) from users",
		"select o.total from orders o join users u on u.id = o.user_id",
		"select * from orders",
		"select number from public.cards", // only billing.cards has PII
		"with x as (select id from users) select x.* from x",
		"select s.id from (select id from users) s",
		"select id from users union all select id from orders",
		"select now()",
	} {
		t.Run(sql, func(t *testing.T) {
			assert.NoError(t, g.Check(sql))
		})
	}
}

func TestCheckRejects(t *testing.T) {
	g := testGuard()
	for sql, want := range map[string]string{
		"":                                "empty",
		"select from where":               "could not be parsed",
		"select 1; select 2":              "exactly one statement",
		"update users set email = ''":     "only SELECT",
		"select id into t from orders":    "SELECT INTO",
		"select id from users for update": "row locking",
		"with d as (delete from orders returning id) select * from d":                 "DELETE is not allowed",
		"select email from users":                                                     "users.email is marked as PII",
		"select EMAIL from USERS":                                                     "users.email",
		`select "phone" from public.users`:                                            "public.users.phone",
		"select u.email from users u":                                                 "users.email",
		"select users.email from users":                                               "users.email",
		"select public.users.email from public.users":                                 "users.email",
		"select * from users":                                                         "SELECT * is not allowed on users",
		"select u.* from users u":                                                     "u.* is not allowed",
		"table users":                                                                 "SELECT * is not allowed",
		"select to_json(u) from users u":                                              "whole rows of users",
		"select row_to_json(users) from users":                                        "whole rows of users",
		"select id from users where email like '%@x'":                                 "users.email",
		"select id from users order by phone":                                         "users.phone",
		"select s.email from (select id from orders) s join users on true":            "users.email",
		"select o.id from (select * from orders) o join users on true":                "SELECT * is not allowed",
		"select x.email from (select id from users) s, lateral (select 1) x":          "users.email",
		"select number from billing.cards":                                            "billing.cards.number",
		"select number from cards":                                                    "cards.number",
		"select query_to_xml('select email from users', true, true, '')":              "query_to_xml",
		"select pg_catalog.dblink('x', 'select 1')":                                   "dblink",
		"select (u).email from users u":                                               "whole rows of users",
		"select id from users where id in (select id from users where email is null)": "users.email",
	} {
		t.Run(sql, func(t *testing.T) {
			err := g.Check(sql)
			require.Error(t, err)
			assert.True(t, IsRejection(err), "%v", err)
			assert.Contains(t, err.Error(), want)
		})
	}
}

func TestNoPIIAllowsStar(t *testing.T) {
	assert.NoError(t, New(nil).Check("select * from users"))
}

func TestIsRejection(t *testing.T) {
	assert.False(t, IsRejection(errors.New("boom")))
	assert.True(t, IsRejection(reject("no")))
}

func TestHelpers(t *testing.T) {
	assert.Equal(t, "", funcName(nil))
	_, ok := stringNode("x")
	assert.False(t, ok)
	_, ok = stringNode(map[string]any{})
	assert.False(t, ok)
	assert.False(t, isStar("x"))
	w := &walker{}
	w.addRelation("not a map")
	assert.Empty(t, w.relations)
}

func TestUnreadableColumnRef(t *testing.T) {
	w := &walker{}
	w.addRef(map[string]any{"fields": []any{}})
	assert.Empty(t, w.refs)
	w.addRef(map[string]any{"fields": []any{map[string]any{"A_Const": map[string]any{}}}})
	require.Error(t, w.err)
	assert.Contains(t, w.err.Error(), "cannot read")
}

func TestPIIWithNoSchemaMatchesEverySchema(t *testing.T) {
	pii := indexPII([]Column{{Table: "users", Column: "email"}}, "")
	err := checkRefs(pii, []relation{{schema: "shop", name: "users"}}, []ref{{name: "email"}}, false)
	assert.ErrorContains(t, err, "shop.users.email")
}
