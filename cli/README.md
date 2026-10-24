# sqlmcp CLI

Query the databases you registered on a SQL MCP server from the terminal,
with the same PII guard as the MCP tool. The commands mirror the `sql` tool's
actions: `tables` is `listTables`, `describe` is `describeTable`, `query` is
`query`, and `pii add` is `markPii`.

## Install

macOS and Linux (amd64, arm64):

```sh
curl -fsSL https://raw.githubusercontent.com/tmphost/sql-mcp/main/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/tmphost/sql-mcp/main/install.ps1 | iex
```

Both download the latest release, check it against `checksums.txt` and
install `sqlmcp` to `/usr/local/bin` when writable or `~/.local/bin`
(Windows: `%LOCALAPPDATA%\sqlmcp\bin`, added to your user PATH). Set
`SQLMCP_VERSION=v1.2.3` for a given release or `SQLMCP_INSTALL_DIR` for
another directory.

From source: `cd cli && go build -o sqlmcp .` (or `make cli`, which writes
`bin/sqlmcp`).

## Sign in

```sh
sqlmcp login --server https://sql.example.com
```

`login` opens the browser (the URL is printed too) on the server's consent
page, which lists your databases. Tick the ones this computer may open: the
CLI's tokens carry only those, and every other database is *not found* to
it. Run `login` again to pick again; `logout` forgets the login.

The tokens are saved in `$SQLMCP_CONFIG`, or `sqlmcp/config.json` under your
user config directory, readable only by you, and sealed (AES-256-GCM) with a
key derived from this machine's ID and your OS user: `/etc/machine-id` on
Linux, `IOPlatformUUID` on macOS, `MachineGuid` on Windows. A copy of the file
opens on no other machine or account. A machine with no ID (some containers)
gets a random one, kept in `sqlmcp/machine-id` under your cache directory, out
of the config's way. Access tokens last an hour and are refreshed on their
own; a refresh checks the server's allowlist again.

`--server` or `SQLMCP_SERVER` picks the server; tokens are only ever sent to
the server that issued them.

## Usage

```sh
sqlmcp db ls                                      # the databases this login opens
sqlmcp tables shop                                # tables and views, with their PII columns
sqlmcp describe shop users                        # or public.users
sqlmcp query shop "select id, total from orders limit 10"
sqlmcp query shop --format csv < report.sql > report.csv
sqlmcp pii ls shop
sqlmcp pii suggest shop                           # names that look like PII, unmarked
sqlmcp pii add shop users.phone public.users.ssn
sqlmcp log shop -n 50                             # latest queries, from every source
sqlmcp whoami
```

A database is named by its name or its ID. Global flags: `--json` prints
JSON, `--server` picks the server, `--version` prints the version.

`query` takes one read-only `SELECT`, as an argument or on stdin. The server
refuses one that touches a PII column, uses `SELECT *` over a table with PII,
or writes, and says why (exit code 1). Every row up to the server's
`QUERY_MAX_ROWS` comes back: nothing is cut to a preview or stored on
tmphost. Rows go to stdout as an aligned table, `--format csv` or
`--format json`; the row count goes to stderr.

The CLI can mark PII columns but never unmark one, nor change a database: the
server refuses its token everything else. Unmark a column in the web app.

## Development

```sh
make test-cli   # go test -race with coverage; CI requires 100%
make lint-cli
```

Pushing a `v*` tag runs `.github/workflows/release.yml`, which cross-builds
the CLI and attaches `sqlmcp_<os>_<arch>.tar.gz` / `.zip` and `checksums.txt`
to the GitHub release that `install.sh` and `install.ps1` download from.
