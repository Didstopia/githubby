package git

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain gives the tests their own global git config, so they never
// change the real one (git lfs install) or run the machine's global hooks.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "githubby-git-test")
	if err != nil {
		panic(err)
	}

	// A test identity, because CI machines have none and commits would fail.
	cfg := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(cfg, []byte("[user]\n\temail = test@example.com\n\tname = Test\n"), 0600); err != nil {
		panic(err)
	}
	os.Setenv("GIT_CONFIG_GLOBAL", cfg)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
