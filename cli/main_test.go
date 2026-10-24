package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMainPrintsTheVersion(t *testing.T) {
	code := -1
	old, oldArgs := exit, os.Args
	t.Cleanup(func() { exit, os.Args = old, oldArgs })
	exit = func(c int) { code = c }
	os.Args = []string{"sqlmcp", "--version"}
	main()
	assert.Equal(t, 0, code)
}
