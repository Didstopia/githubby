package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain gives the tests an empty global git config, so test repos never
// touch the real one or run the machine's global hooks.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "githubby-cli-test")
	if err != nil {
		panic(err)
	}

	os.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "gitconfig"))
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
