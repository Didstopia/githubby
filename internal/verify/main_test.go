package verify

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain gives the tests their own global git config, so they never touch the real one
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "githubby-verify-test")
	if err != nil {
		panic(err)
	}

	cfg := filepath.Join(dir, "gitconfig")
	body := "[user]\n\temail = test@example.com\n\tname = Test\n[init]\n\tdefaultBranch = main\n"
	if err := os.WriteFile(cfg, []byte(body), 0600); err != nil {
		panic(err)
	}
	os.Setenv("GIT_CONFIG_GLOBAL", cfg)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
