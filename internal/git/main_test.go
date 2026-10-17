package git

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain gives the tests an empty global git config, so they never
// change the real one (git lfs install) or run the machine's global hooks.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "githubby-git-test")
	if err != nil {
		panic(err)
	}

	os.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "gitconfig"))
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
