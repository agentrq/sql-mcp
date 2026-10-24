// Command sqlmcp queries the databases registered on a SQL MCP server, with
// the same PII guard as its MCP tool.
package main

import (
	"os"
	"time"

	"github.com/agentrq/sql-mcp/cli/internal/cmd"
	"github.com/agentrq/sql-mcp/cli/internal/machine"
	"github.com/agentrq/sql-mcp/cli/internal/oauth"
)

// version is set when releasing, with -ldflags "-X main.version=v1.2.3".
var version = "dev"

// exit is swapped in tests, so main runs without ending the test binary.
var exit = os.Exit

func main() {
	exit(cmd.Execute(os.Args[1:], cmd.Options{
		In:          os.Stdin,
		Out:         os.Stdout,
		Err:         os.Stderr,
		Getenv:      os.Getenv,
		Now:         time.Now,
		OpenBrowser: oauth.OpenBrowser,
		MachineKey:  machine.Key,
		Version:     version,
	}))
}
