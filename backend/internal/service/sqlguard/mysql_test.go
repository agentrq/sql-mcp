package sqlguard

import (
	"reflect"
	"testing"

	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testMySQLGuard() *MySQLGuard {
	return NewMySQL([]Column{
		{Schema: "shop", Table: "users", Column: "email"},
		{Schema: "SHOP", Table: "Users", Column: "Phone"},
		{Schema: "billing", Table: "cards", Column: "number"},
		{Table: "secrets", Column: "value"}, // no schema: every database's
	})
}

func TestMySQLCheckAllows(t *testing.T) {
	g := testMySQLGuard()
	for _, sql := range []string{
		"select id, created_at from users",
		"SELECT `id` FROM `shop`.`users` WHERE id > 10 ORDER BY 1 LIMIT 5",
		"select u.id from users u where u.id > 10 order by 1",
		"select count(*) from users",
		"select o.total from orders o join users u on u.id = o.user_id",
		"select * from orders",
		"select number from shop.cards", // only billing.cards has PII
		"select email from other.users", // only shop.users has PII
		"with x as (select id from users) select x.* from x",
		"select s.id from (select id from users) s",
		"select id from users union all select id from orders",
		"select now(), version(), @@version",
		`select id from users where name = "email"`, // a string, not a column
		"select id from users where note = 'it\\'s' -- email",
		"select id from users # email",
		"select /*+ MAX_EXECUTION_TIME(1000) */ id from users",
		"select id from users /* email */",
		"select json_extract(doc, '$.a') from orders",
		"values row(1, 2)",
	} {
		t.Run(sql, func(t *testing.T) {
			assert.NoError(t, g.Check(sql))
		})
	}
}

func TestMySQLCheckRejects(t *testing.T) {
	g := testMySQLGuard()
	for sql, want := range map[string]string{
		"":                                                        "empty",
		"   ":                                                     "empty",
		"select from where":                                       "could not be parsed",
		"select 1; select 2":                                      "exactly one statement",
		"update users set email = ''":                             "only SELECT",
		"delete from orders":                                      "only SELECT",
		"insert into orders select * from x":                      "only SELECT",
		"show tables":                                             "only SELECT",
		"explain select email from users":                         "only SELECT",
		"set @a = 1":                                              "only SELECT",
		"call p()":                                                "only SELECT",
		"select id from orders into @x":                           "could not be parsed",
		"select id from orders into outfile 'x'":                  "SELECT INTO",
		"select id from users for update":                         "row locking",
		"select id from users lock in share mode":                 "row locking",
		"select id from users for share":                          "row locking",
		"select email from users":                                 "the column users.email is marked as PII",
		"select EMAIL from USERS":                                 "users.email",
		"select `phone` from shop.users":                          "shop.users.phone",
		"select u.email from users u":                             "users.email",
		"select users.email from users":                           "users.email",
		"select shop.users.email from shop.users":                 "users.email",
		"select * from users":                                     "SELECT * is not allowed on users",
		"select u.* from users u":                                 "u.* is not allowed",
		"select shop.users.* from shop.users":                     "shop.users.* is not allowed",
		"table users":                                             "SELECT * is not allowed",
		"select id from users where email like '%@x'":             "users.email",
		"select id from users order by phone":                     "users.phone",
		"select id from users group by id having max(email) > ''": "users.email",
		"select s.email from (select id from orders) s join users on true":            "users.email",
		"select o.id from (select * from orders) o join users on true":                "SELECT * is not allowed",
		"select id from orders join users using (email)":                              "users.email",
		"select number from billing.cards":                                            "billing.cards.number",
		"select number from cards":                                                    "cards.number",
		"select value from anywhere.secrets":                                          "anywhere.secrets.value",
		"select id from users where id in (select id from users where email is null)": "users.email",
		"select (select email from users limit 1)":                                    "users.email",
		"select id from orders where exists (select 1 from users where email = 'x')":  "users.email",
		"select json_object('e', email) from users":                                   "users.email",
		"select group_concat(email) from users":                                       "users.email",
		"select row_number() over (order by email) from users":                        "users.email",
		"select id /*! , email */ from users":                                         "executable comments",
		"select id /*!80000 , email */ from users":                                    "executable comments",
		"select id /*M! , email */ from users":                                        "executable comments",
		"select id /*T! , email */ from users":                                        "executable comments",
		"select load_file('/etc/passwd')":                                             "load_file",
		"select sleep(10)":                                                            "sleep",
		"select BENCHMARK(1000000, md5('a'))":                                         "benchmark",
		"select get_lock('x', 10)":                                                    "get_lock",
	} {
		t.Run(sql, func(t *testing.T) {
			err := g.Check(sql)
			require.Error(t, err)
			assert.True(t, IsRejection(err), "%v", err)
			assert.Contains(t, err.Error(), want)
		})
	}
}

func TestMySQLNoPIIAllowsStar(t *testing.T) {
	assert.NoError(t, NewMySQL(nil).Check("select * from users"))
}

// Paths no MySQL SELECT reaches today, kept in case the parser's tree changes.
func TestMySQLVisitorDefences(t *testing.T) {
	newVisitor := func() *mysqlVisitor {
		return &mysqlVisitor{aliased: map[*ast.TableName]bool{}, seen: map[visit]bool{}}
	}

	v := newVisitor()
	v.enter(&ast.UpdateStmt{})
	assert.ErrorContains(t, v.err, "read-only")

	// A table outside any FROM, and nodes held in a map, are still seen.
	v = newVisitor()
	v.walk(reflect.ValueOf(map[string]ast.Node{
		"t": &ast.TableName{Schema: model.NewCIStr("Shop"), Name: model.NewCIStr("Users")},
		"c": &ast.ColumnName{Name: model.NewCIStr("Email")},
	}))
	assert.Equal(t, []relation{{schema: "shop", name: "users"}}, v.relations)
	assert.Equal(t, []ref{{name: "email"}}, v.refs)
}
