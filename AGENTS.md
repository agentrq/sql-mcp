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
- **OAuth registrations, consents and codes are signed tokens, not rows.**
  Deleting a database revokes its grants because every token is checked
  against the database's current owner.
- **Vite builds with `base: ''`.** The server rewrites `./` to `/` in
  `index.html` (`app.spa`). Without that rewrite, a hard reload on a nested
  route such as `/databases/x` serves a blank page.
- **Content is full width**, like AgentRQ: views don't cap their width
  with `max-w-*`. Only prose paragraphs and modals are narrowed.
- **The UI reuses the class strings in `frontend/src/ui.js`.** Don't
  restyle buttons or inputs inline.

## Working here

- `make test` runs `go vet`, `go test` and vitest. The Postgres driver tests
  need `SQLMCP_TEST_PG_DSN` (see the README) and are skipped without it.
- Cover each change with tests on both sides. The target is 100% unit
  coverage of new lines. `internal/testutil` has a fake engine and a fake
  tmphost, and `internal/app/app_test.go` drives the whole stack over HTTP.
- On this project's dev machine, Go needs `export GOPATH=$HOME/go` because
  GOROOT is `~/go`.
