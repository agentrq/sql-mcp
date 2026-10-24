# sql-mcp REST API

Every route lives under `/api/v1`. Every JSON field is `camelCase`. The browser
authenticates with the HttpOnly `at` cookie (24h access JWT); `rt` (refresh JWT,
2 days idle) is scoped to `/api/v1/auth/refresh`. A 401 from any route means
"call `POST /auth/refresh` once, then replay".

Errors always look like:

```json
{ "error": { "message": "human readable reason", "code": 400 } }
```

## Auth (public)

| Method | Path | Notes |
|---|---|---|
| GET | `/auth/config` | `{ "googleLoginEnabled": true, "rootLoginEnabled": false, "basePath": "" }` |
| GET | `/auth/google/login?redirect_url=` | 302 to Google |
| GET | `/auth/google/callback` | sets cookies, 302 to `redirect_url`. An email outside `ALLOWED_EMAILS` is sent to `/login?error=email_not_allowed` |
| POST | `/auth/root/login` | `{ "rootToken": "..." }` → `{ "status": "ok" }` (only when `ROOT_LOGIN_ENABLED`); root's email must still pass the allowlist |
| POST | `/auth/refresh` | reissues both cookies, `{ "status": "ok" }` or 401 |

## Auth (signed in)

| Method | Path | Response |
|---|---|---|
| GET | `/auth/user` | `{ "id", "email", "name", "picture" }` |
| POST | `/auth/logout` | 204 |

## Databases

A database is one connection a person registered; it gets its own MCP server.

```json
{
  "id": "base62",
  "name": "analytics",
  "driver": "postgres",
  "dsnMasked": "postgres://app:****@db.internal:5432/analytics?sslmode=require",
  "piiColumnCount": 3,
  "agentMarksPii": false,
  "mcpUrl": "https://sqlmcp.example.com/mcp/<id>",
  "createdAt": "RFC3339",
  "updatedAt": "RFC3339"
}
```

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/drivers` | | `{ "drivers": [{ "id": "postgres", "name": "PostgreSQL", "available": true }, { "id": "mysql", "name": "MySQL", "available": true }] }` |
| GET | `/databases` | | `{ "databases": [Database] }` |
| POST | `/databases` | `{ "database": { "name", "driver", "dsn" } }` | 201 `{ "database": Database }` (the DSN is tested before it is saved) |
| POST | `/databases/test` | `{ "database": { "driver", "dsn" } }` | `{ "ok": true, "serverVersion": "PostgreSQL 17.2" }` or error |
| GET | `/databases/:id` | | `{ "database": Database }` |
| PUT | `/databases/:id` | `{ "database": { "name", "dsn" } }` — empty `dsn` keeps the stored one | `{ "database": Database }` |
| DELETE | `/databases/:id` | | 204 |
| GET | `/databases/:id/schema` | | `{ "tables": [{ "schema": "public", "name": "users", "columns": [{ "name": "email", "dataType": "text", "pii": true, "suggestedPii": true }] }] }` |
| GET | `/databases/:id/pii` | | `{ "piiColumns": [{ "schema": "public", "table": "users", "column": "email" }] }` |
| PUT | `/databases/:id/pii` | `{ "piiColumns": [PiiColumn] }` (replaces the whole set; an empty `schema` is the engine's default: `public`, or the DSN's database on MySQL) | `{ "piiColumns": [PiiColumn] }` |
| PUT | `/databases/:id/pii/agent` | `{ "agentMarksPii": true }` (lets the MCP agent add PII columns with `markPii`; it can never remove one) | `{ "database": Database }` |
| POST | `/databases/:id/query` | `{ "sql": "select ..." }` | `{ "columns": ["id"], "rows": [[1]], "rowCount": 1, "truncated": false }`; a query touching a PII column is 422 with the reason |
| GET | `/databases/:id/queries?limit=50` | | `{ "queries": [{ "id", "source": "mcp" \| "console", "sql", "status": "ok" \| "rejected" \| "error", "reason", "rowCount", "resultUrl", "durationMs", "createdAt" }] }` |
| GET | `/databases/:id/queries/stream` | | `text/event-stream`: one `event: query` per query as it is recorded, `data` a query log entry as above and `id` its id; `: ping` comments every 25 s. 401 without a session, 404 for someone else's database |

The query stream ends when the session's `at` cookie expires or the database
is deleted; reconnecting then is refused, so the browser renews the session
(`POST /auth/refresh`) and opens it again. A slow reader misses queries
rather than holding them up, and each (re)connect should reload
`/queries` to catch up. It is served by the standard mux, not Fiber (whose
adaptor buffers the whole response), and only by the process that ran the
query: run one replica, or the stream shows only that replica's queries.

## Settings

tmphost.io is connected with OAuth (the server registers itself with
tmphost by RFC 7591 dynamic client registration and uses PKCE), so there is no
token to paste: the person clicks Connect and approves on tmphost.

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/settings/tmphost` | | `{ "connected": true, "account": "dev@example.com", "serverUrl": "https://app.tmphost.io", "connectedAt": "RFC3339" }` (`account`/`connectedAt` only when connected) |
| GET | `/settings/tmphost/connect` | | **browser navigation** (not fetch): 302 to tmphost's consent page |
| GET | `/settings/tmphost/callback` | | tmphost redirects here; 302 to `/settings?tmphost=connected` or `/settings?tmphost=error&message=...` |
| DELETE | `/settings/tmphost` | | 204 |

When connected, an MCP query whose result is larger than the inline limit (or
that asks for `store: true`) is uploaded to a 30-minute tmphost folder as CSV,
and the query log records its `resultUrl`.

## MCP (one server per database)

`/mcp/{databaseId}` — Streamable HTTP, OAuth 2.1 with RFC 7591 dynamic client
registration, RFC 8414/9728 discovery and a server-rendered consent page:

- `/.well-known/oauth-protected-resource/mcp/{id}`
- `/.well-known/oauth-authorization-server/mcp/{id}`
- `/mcp/{id}/oauth2/register`, `/mcp/{id}/oauth2/authorize`, `/mcp/{id}/oauth2/token`

One tool, `sql`, taking `{ action, payload }`: `listTables`, `describeTable`,
`query`, `help`, and `markPii` when the database's `agentMarksPii` is on (the
tool is then no longer annotated read-only, and the instructions ask the agent
to mark the personal data it finds).

Answers are [TOON](https://toonformat.dev) text (spec v3, comma-delimited),
which spends fewer tokens than JSON: a table names its columns once, then
gives one line per row. `help` answers in prose; an error is `error: <message>`
with `isError` set.

```
listTables     tables[N]{table,kind,columns}:   then   pii[M]{table,column}:
describeTable  table: public.users
               kind: table
               columns[N]{name,type,pii}:
                 id,integer,false
query          rowCount: 120
               rowsShown: 25                     (when cut to the inline limit)
               truncated: false                  (true when QUERY_MAX_ROWS cut it)
               durationMs: 4
               resultUrl: "https://..."          (the whole result as CSV, when stored)
               resultExpiresAt: "RFC3339"
               note: ...                         (why it could not be stored)
               rows[25]{id,name}:
                 1,alice
                 2,null
markPii        table: public.users               ({table, schema?, columns: [...]})
               marked: 1
               columns[2]{name,pii}:
                 phone,marked
                 email,already
```

`markPii` only adds: every column must exist in the table, a column already
marked is reported as `already`, and nothing an agent sends unmarks one. The
setting is checked on every call, so turning it off stops an agent at once.

A query result shows at most the inline limit (`QUERY_INLINE_ROWS`, default 25)
of its rows.

## Admin (`/admin/api/v1`)

The server setup, for whoever holds `ROOT_TOKEN`; disabled while it is unset.
Signing in mints an admin JWT (audience `actor:admin`, 1 hour), set as the
HttpOnly, SameSite=Strict `admin_at` cookie on path `/admin`, and returned in
the body for scripts, which send it as `Authorization: Bearer <token>`. An
admin token is nobody's session, and a person's session is not an admin
token. There is no refresh: sign in again after an hour.

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/config` | | `{ "enabled": true }` (public) |
| POST | `/login` | `{ "rootToken": "..." }` | `{ "token", "expiresAt" }` + cookie; 401 wrong token, 403 disabled (public) |
| POST | `/logout` | | 204, clears the cookie (public) |
| GET | `/session` | | `{ "expiresAt" }` |
| GET | `/settings` | | `{ "allowedEmails": [], "queryMaxRows", "queryInlineRows", "queryTimeoutSeconds", "source": "env" \| "saved", "updatedAt"?, "server": { "baseUrl", "sslEnabled", "domain", "httpsPort", "googleLoginEnabled", "rootLoginEnabled", "appDbDriver", "tmphostApiUrl" } }` |
| PUT | `/settings` | `{ "allowedEmails": ["*@agentrq.com"], "queryMaxRows": 1000, "queryInlineRows": 25, "queryTimeoutSeconds": 30 }` | the settings; 400 with the reason when invalid |
| DELETE | `/settings` | | the settings, back to the environment's (`source: "env"`) |
| POST | `/settings/check-email` | `{ "email", "allowedEmails": [] }` | `{ "allowed", "matchedEntry"? }`, tested against the list sent, saved or not |

A saved configuration overrides the environment's `ALLOWED_EMAILS` and
`QUERY_*` values and applies at once, with no restart. It is stored in the app
database as the `configuration.json` setting, a JSON document sealed with
`ENCRYPTION_KEY` (AES-256-GCM). The server refuses to start when it cannot
decrypt it, rather than falling back to the environment's allowlist.
Limits: 1–500 allowlist entries of up to 320 characters, max rows
1–100000, inline rows 1–max rows, timeout 1–600 seconds.

The web pages are `/admin/login` and `/admin`.
