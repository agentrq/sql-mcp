# SQL MCP

Turn a database into an MCP server that an AI agent can query without reading
personal data.

You add a database (its engine and DSN) and mark the columns that hold PII.
SQL MCP then gives that database its own MCP URL behind OAuth, with dynamic
client registration. You paste the URL into Claude, Cursor or any MCP client,
approve the client once on a consent page, and the agent can list tables,
describe them and run read-only `SELECT`s. A query that touches a PII column is
rejected with the reason, and the agent rewrites it.

Large results don't fill up the agent's context. They are saved as CSV on your
[tmphost.io](https://app.tmphost.io) account for 30 minutes, and the agent gets
a preview plus the URL.

| | |
|---|---|
| Engines | PostgreSQL, MySQL 8+ (MariaDB works too) |
| Sign-in | Google, or a root token, restricted to `ALLOWED_EMAILS` |
| MCP auth | OAuth 2.1 with PKCE (S256), RFC 7591 registration, RFC 8414/9728 discovery |
| TLS | Let's Encrypt through Cloudflare DNS-01, on any port |

## Run it

You need Go 1.27+ and Node 24+.

```sh
make dev        # backend on :3000, web app on http://localhost:5173
```

On the first run `make dev` writes `backend/.env` with fresh development
secrets and root-token sign-in, and prints the root token. Edit that file to
add Google sign-in or more allowed emails.

For production:

```sh
make build      # backend/bin/sql-mcp, serving backend/public
cd backend && ./bin/sql-mcp
```

Or with Docker:

```sh
docker build -t sql-mcp .
docker run --env-file backend/.env -p 3000:3000 -p 3443:3443 -v sqlmcp:/data sql-mcp
```

Every setting is described in [`.env.example`](.env.example).

### Who can sign in

`ALLOWED_EMAILS` is a comma-separated list. Each entry can be:

- an exact address: `alice@example.com`
- a glob, where `*` matches anything: `*@agentrq.com`
- a regular expression prefixed with `re:`: `re:^[a-z]+\.ops@example\.net$`

Matching ignores case and has to cover the whole address. An empty list lets
nobody in. The allowlist is checked again on every session refresh, so if you
remove an address, that person is signed out within 24 hours. The
[admin setup](#admin-setup) can change the list without a restart.

### Admin setup

With `ROOT_TOKEN` set, `/admin` opens the server setup. Sign in with the
root token to change the allowed emails and the query limits (max rows,
inline rows, timeout) while the server runs. A saved configuration overrides
`.env`, takes effect immediately, and is stored in the app database encrypted
with `ENCRYPTION_KEY`. **Reset to environment** brings back the `.env`
values. An admin session lasts one hour, is separate from any person's
session, and cannot see anyone's databases. The API is documented under
[Admin](docs/API.md#admin-adminapiv1).

### HTTPS

Set `SSL_ENABLED=true`, `APP_DOMAIN`, `LETSENCRYPT_EMAIL` and
`CLOUDFLARE_API_TOKEN` (with Zone:DNS:Edit). The certificate is issued with
the DNS-01 challenge, so Let's Encrypt never connects to the server. That
means `HTTPS_PORT` and `HTTP_PORT` can be any ports, and `HTTP_PORT` only
redirects. The certificate is cached in `SSL_CACHE_DIR` and renewed 30 days
before it expires, without a restart. Set `APP_BASE_URL` to the https URL
including the port, for example `https://sql.example.com:3443`.

## Connect an agent

1. In the app, add a database: give it a name, choose the engine and enter the
   DSN. The connection is tested before it is saved. The DSN is encrypted
   (AES-256-GCM) and only ever shown masked.

   | Engine | DSN |
   |---|---|
   | PostgreSQL | `postgres://user:pass@host:5432/db?sslmode=require`, or `host=h user=u dbname=db` |
   | MySQL | `mysql://user:pass@host:3306/db?tls=true`, or the Go driver's form `user:pass@tcp(host:3306)/db?tls=true` |

   A MySQL DSN takes the [driver's parameters](https://github.com/go-sql-driver/mysql#parameters).
   Its database is where a table with no schema is looked up, and a PII
   column marked with no schema lives there.
2. On the **PII columns** tab, tick the columns to protect. Columns whose
   names look like PII (email, phone, ssn…) are suggested. Turn on **Let the
   agent mark PII columns** to have MCP clients mark more as they find them;
   an agent can only add marks, and only you can remove one.
3. On the **Connect** tab, copy the MCP URL (`<APP_BASE_URL>/mcp/<id>`) and
   add it to your client, for example
   `claude mcp add --transport http shop https://sql.example.com/mcp/<id>`.
   The client registers itself and opens the consent page. Only the
   database's owner can approve it.
4. Optional: under **Settings**, connect tmphost.io so large results can be
   stored. This uses OAuth too; SQL MCP registers itself with tmphost.

The MCP server has one tool, `sql`, which takes `{action, payload}`:

| action | payload | |
|---|---|---|
| `listTables` | none | every table and view, with its PII columns |
| `describeTable` | `{table, schema?}` | columns, types, PII flags |
| `query` | `{sql, store?}` | one read-only `SELECT` |
| `markPii` | `{table, schema?, columns}` | mark columns as PII; offered only when the owner allows it, and it never unmarks |
| `help` | `{topic?}` | usage |

The **Query** tab runs the same checks from the browser, and **Activity** logs
every query with whether it ran, was rejected or failed.

## Query from the terminal

The `sqlmcp` CLI runs the same tool from a terminal, against the server:

```sh
curl -fsSL https://raw.githubusercontent.com/tmphost/sql-mcp/main/install.sh | sh   # Windows: install.ps1
sqlmcp login --server https://sql.example.com     # pick the databases in the browser
sqlmcp tables shop
sqlmcp query shop "select id, total from orders limit 10"
sqlmcp pii add shop users.phone
```

On the consent page you tick the databases this computer may open; the CLI
sees no other. Its tokens are saved sealed with a key derived from the
machine's ID and the OS user, so the config file opens nowhere else. Results
come back whole to the terminal and are never stored on tmphost. The CLI can
mark PII columns but never unmark one, nor change a database. See
[`cli/README.md`](cli/README.md).

## What the PII guard does, and what it doesn't

Each query is parsed by a parser for its engine: PostgreSQL's own
([libpg_query](https://github.com/pganalyze/libpg_query), compiled to WASM),
or for MySQL [TiDB's](https://github.com/pingcap/tidb/tree/master/pkg/parser)
(pure Go). It is refused when:

- it is anything other than a single `SELECT`. That rules out writes,
  `SELECT … INTO`, `FOR UPDATE`, and a write hidden in a CTE.
- it names a PII column anywhere: in the select list, `WHERE`, `JOIN`,
  `ORDER BY`, `GROUP BY`, a subquery or a function argument. Columns are
  resolved through table aliases, and a column name that matches a PII column
  of any table in the query is also refused.
- it uses `*`, `t.*` or a whole-row reference such as `to_json(u)` on a table
  that has PII columns.
- it calls a function that runs SQL from text or reads the server's files
  (`query_to_xml`, `dblink`, `pg_read_file`, `lo_*`, `set_config`…; on
  MySQL `LOAD_FILE`, `SLEEP`, `BENCHMARK`, `GET_LOCK`…).
- on MySQL, it has an executable comment (`/*! … */`, `/*M! … */`): whether
  the server runs it depends on its version, which the guard can't know.

Queries run in a `READ ONLY` transaction with a statement timeout
(`max_execution_time` on MySQL, `max_statement_time` on MariaDB).

MySQL connections are opened with what the guard relies on, whatever the DSN
says: one statement per query, `utf8mb4`, and MySQL 8's default `sql_mode`.
Without `ANSI_QUOTES`, `"email"` is a string as the guard reads it, and
without `NO_BACKSLASH_ESCAPES` a `\'` can't end a string early.

On MySQL, `READ ONLY` doesn't stop DDL: `CREATE`, `DROP` and the like commit
the transaction first. The guard refuses them, but the user's grants are what
stop them on the server.

The guard only sees the query text. A **view** or a **user-defined function**
that reads a PII column is invisible to it, so mark the view's columns as PII
too, or keep such views out of the role's reach. The real boundary is the
database role. Connect with a role that can only `SELECT` what the agent
should see:

```sql
CREATE ROLE sqlmcp LOGIN PASSWORD '…';
GRANT USAGE ON SCHEMA public TO sqlmcp;
GRANT SELECT ON public.orders, public.products TO sqlmcp;
-- or column by column:
GRANT SELECT (id, created_at, country) ON public.users TO sqlmcp;
```

On MySQL:

```sql
CREATE USER 'sqlmcp'@'%' IDENTIFIED BY '…';
GRANT SELECT ON shop.orders TO 'sqlmcp'@'%';
GRANT SELECT (id, created_at, country) ON shop.users TO 'sqlmcp'@'%';
```

## Develop

```sh
make test       # go vet + go test (backend and CLI), and vitest
make lint       # golangci-lint (backend/ and cli/) and ESLint
make cli        # bin/sqlmcp
make cover      # the coverage gates CI enforces; BASE=main also checks new lines
```

The drivers' tests need databases seeded from
`backend/internal/service/dbdriver/testdata/`. Without them they are skipped:

```sh
docker run -d --name sqlmcp-pg -e POSTGRES_PASSWORD=secret -e POSTGRES_DB=shop -p 55432:5432 postgres:18
docker exec -i sqlmcp-pg psql -U postgres -d shop -v ON_ERROR_STOP=1 -q < backend/internal/service/dbdriver/testdata/shop.sql
docker run -d --name sqlmcp-mysql -e MYSQL_ROOT_PASSWORD=secret -p 53306:3306 mysql:8.4
docker exec -i sqlmcp-mysql mysql -uroot -psecret < backend/internal/service/dbdriver/testdata/shop_mysql.sql
docker run -d --name sqlmcp-mariadb -e MARIADB_ROOT_PASSWORD=secret -p 53307:3306 mariadb:11.4
docker exec -i sqlmcp-mariadb mariadb -uroot -psecret < backend/internal/service/dbdriver/testdata/shop_mysql.sql
SQLMCP_TEST_PG_DSN='postgres://postgres:secret@localhost:55432/shop?sslmode=disable' \
SQLMCP_TEST_MYSQL_DSN='mysql://root:secret@127.0.0.1:53306/shop' \
SQLMCP_TEST_MARIADB_DSN='mysql://root:secret@127.0.0.1:53307/shop' make test-backend
```

(Wait for each server to accept connections before seeding it.)

### CI

GitHub Actions run on every pull request: Backend (vet, race tests against
Postgres, MySQL and MariaDB services, coverage), Frontend (vitest thresholds, build), Lint, CLI
(cross-builds, race tests at 100% coverage, lint, install.sh), a Docker build, and the
case-collision check. Backend coverage may not fall below
`COVER_MIN` in `.github/workflows/backend.yml` (95%; today 95.8%), and every Go line a pull
request adds must be run by a test.

A push to `main` publishes `<user>/sql-mcp:latest` and `:<sha>` to Docker Hub
for `linux/amd64` and `linux/arm64`; a `v*` tag publishes `:<version>` and
`:<major>.<minor>`. Both need the repository secrets `DOCKERHUB_USERNAME` and
`DOCKERHUB_TOKEN` (a Docker Hub access token with write scope). A `v*` tag
also attaches the CLI's `sqlmcp_<os>_<arch>` archives and `checksums.txt` to
the GitHub release, which the install scripts download.

The REST API is documented in [`docs/API.md`](docs/API.md). Read
[`AGENTS.md`](AGENTS.md) before changing code.

## License

Apache-2.0. See [LICENSE](LICENSE).
