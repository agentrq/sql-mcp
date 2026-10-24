# SQL MCP Codebase Notes

Read this before changing code. It records the decisions you can't see in the
source. Keep each note to a line or two: the rule, and what breaks without it.

## Layout

- `backend/`: Go module `github.com/agentrq/sql-mcp/backend`. Fiber v3 serves
  the REST API (`/api/v1`) and the admin API (`/admin/api/v1`) through one
  `adaptor.FiberApp`, mounted on a stdlib `http.ServeMux`. The mux also serves
  the MCP servers and their OAuth routes (`/mcp/{id}`, `/.well-known/...`) and
  the web app (`/`).
- `frontend/`: Vue 3, pinia, vue-router and Tailwind v4, built with Vite into
  `frontend/dist`, which `make build` copies to `backend/public`.
- `docs/API.md`: the REST contract. Change it in the same change as the code.

Backend layers, top to bottom: `handler/*` (HTTP only), `controller/crud`
(every business rule), `repository` (GORM), `service/*` (leaf packages with no
app state). A handler never calls the repository directly.

## Rules

- **Every user-data method takes the user ID.** Someone else's database is
  reported as *not found*, never forbidden, so that IDs can't be probed.
- **Tokens are told apart by audience** (`service/auth/jwt.go`). A new kind of
  token gets a new audience and its own Validate method. Never accept a token
  minted for another purpose.
- **The admin is not a person.** An admin JWT opens `/admin/api/v1` and
  nothing else. With `ROOT_TOKEN` unset, no admin token is accepted at all.
- **Secrets at rest are sealed with `security.Sealer`** (AES-256-GCM with
  `ENCRYPTION_KEY`): DSNs, tmphost tokens, and the admin configuration
  (`configuration.json` setting). If a saved configuration can't be opened,
  startup fails; silently falling back to the `.env` allowlist would re-admit
  people the admin removed.
- **The live configuration is swapped whole** (`crud.Controller.current`, an
  `atomic.Pointer`). Read it once per request (`c.live()`), so a request never
  mixes old and new limits.
- **The SQL guard is conservative on purpose** (`service/sqlguard`). A bare
  `*` anywhere is refused when any PII table is in the query, even if the
  scope proves it harmless. Don't loosen a rule without a test showing it
  still refuses every leaking form. The guard only sees query text, so views
  and functions are the database role's job (see the README).
- **Postgres is parsed with `wasilibs/go-pgquery`** (libpg_query as WASM),
  not cgo `pg_query_go`. The build is `CGO_ENABLED=0`.
- **MySQL is parsed with TiDB's parser, walked by reflection**
  (`sqlguard/mysql.go`). Its `Accept` skips children (a SELECT list's `*`), so
  don't switch to the visitor. The guard parses as the server reads only
  because the driver forces `sql_mode` (no `ANSI_QUOTES`,
  `NO_BACKSLASH_ESCAPES`) and `utf8mb4`; a DSN must never loosen those.
- **An agent can only add PII marks** (`markPii`, when the owner turns on
  `agentMarksPii`). Never give MCP a way to unmark; that would let an agent
  lift its own guard. The editor saves its diff onto the server's latest set,
  so a save doesn't drop what the agent marked meanwhile.
- **OAuth registrations, consents and codes are signed tokens, not rows.**
  Deleting a database revokes its grants because every token is checked
  against the database's current owner.
- **The MCP tool answers in TOON, not JSON** (`service/toon`, hand-written,
  no dependency), to spend fewer tokens. A new action builds a `toon.Object`;
  only `help` is prose.
- **Streams go on the stdlib mux, not Fiber** (`handler/live`). The Fiber
  adaptor buffers the whole response, so an SSE route on Fiber sends nothing
  until it ends. The live query feed (`service/feed`) is in-process only.
- **The image is `FROM scratch`.** It has only what the Dockerfile copies
  in: the binary, the web app, CA certs, `/etc/passwd` and `/data`. A new
  runtime need (`/tmp`, tzdata) must be copied too, or it fails only in Docker.
  It runs as UID 65532 so volumes chowned for the old distroless image work.
- **Vite builds with `base: ''`.** The server rewrites `./` to `/` in
  `index.html` (`app.spa`). Without that rewrite, a hard reload on a nested
  route such as `/databases/x` serves a blank page.
- **Content is full width**, like AgentRQ: views don't cap their width
  with `max-w-*`. Only prose paragraphs and modals are narrowed.
- **No two paths may differ only by letter case** (`App.test.js` next to
  `app.test.js`). On macOS and Windows they are the same file, so the
  checkout comes out broken. `make check-case`, part of `make test`, fails on
  such a pair.
- **The UI reuses the class strings in `frontend/src/ui.js`.** Don't
  restyle buttons or inputs inline.

## Working here

- `make test` runs `go vet`, `go test` and vitest. The driver tests need
  `SQLMCP_TEST_PG_DSN`, `SQLMCP_TEST_MYSQL_DSN` and `SQLMCP_TEST_MARIADB_DSN`
  (see the README) and are skipped without them.
- Cover each change with tests on both sides. The target is 100% unit
  coverage of new lines. `internal/testutil` has a fake engine and a fake
  tmphost, and `internal/app/app_test.go` drives the whole stack over HTTP.
- CI (`.github/workflows`) fails a PR on any added Go line no test runs
  (`scripts/coverage.sh`), on a backend total below `COVER_MIN`, and on any
  golangci-lint or ESLint finding, warnings included. Run `make lint` and
  `make cover BASE=main` before pushing. Raise `COVER_MIN`, never lower it.
- On this project's dev machine Go is `/usr/local/go/bin`, not on `PATH`;
  `golangci-lint` is in `~/go/bin`.
