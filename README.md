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
| Engines | PostgreSQL. MySQL is listed but not available yet. |
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

1. In the app, add a database: give it a name, choose PostgreSQL and enter the
   DSN. The connection is tested before it is saved. The DSN is encrypted
   (AES-256-GCM) and only ever shown masked.
2. On the **PII columns** tab, tick the columns to protect. Columns whose
   names look like PII (email, phone, ssn…) are suggested.
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
| `help` | `{topic?}` | usage |

The **Query** tab runs the same checks from the browser, and **Activity** logs
every query with whether it ran, was rejected or failed.

## What the PII guard does, and what it doesn't

Each query is parsed with PostgreSQL's own parser
([libpg_query](https://github.com/pganalyze/libpg_query), compiled to WASM)
and refused when:

- it is anything other than a single `SELECT`. That rules out writes,
  `SELECT … INTO`, `FOR UPDATE`, and a write hidden in a CTE.
- it names a PII column anywhere: in the select list, `WHERE`, `JOIN`,
  `ORDER BY`, `GROUP BY`, a subquery or a function argument. Columns are
  resolved through table aliases, and a column name that matches a PII column
  of any table in the query is also refused.
- it uses `*`, `t.*` or a whole-row reference such as `to_json(u)` on a table
  that has PII columns.
- it calls a function that runs SQL from text or reads the server's files
  (`query_to_xml`, `dblink`, `pg_read_file`, `lo_*`, `set_config`…).

Queries run in a `READ ONLY` transaction with a statement timeout.

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

## Develop

```sh
make test       # go vet + go test, and vitest
```

The PostgreSQL driver's tests need a database. Without one they are skipped:

```sh
docker run -d --name sqlmcp-pg -e POSTGRES_PASSWORD=secret -e POSTGRES_DB=shop -p 55432:5432 postgres:18
SQLMCP_TEST_PG_DSN='postgres://postgres:secret@localhost:55432/shop?sslmode=disable' make test-backend
```

The REST API is documented in [`docs/API.md`](docs/API.md). Read
[`AGENTS.md`](AGENTS.md) before changing code.

## License

Apache-2.0. See [LICENSE](LICENSE).
