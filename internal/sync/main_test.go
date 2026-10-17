package sync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gh "github.com/google/go-github/v86/github"
	"github.com/stretchr/testify/require"
)

// TestMain gives the tests their own global git config, so they never touch
// the real one, and lets test repos use local paths as submodule URLs.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "githubby-sync-test")
	if err != nil {
		panic(err)
	}

	cfg := filepath.Join(dir, "gitconfig")
	body := "[protocol \"file\"]\n\tallow = always\n[user]\n\temail = test@example.com\n\tname = Test\n[init]\n\tdefaultBranch = main\n"
	if err := os.WriteFile(cfg, []byte(body), 0600); err != nil {
		panic(err)
	}
	os.Setenv("GIT_CONFIG_GLOBAL", cfg)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// runGit runs a real git command in a test and fails on error
func runGit(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

// newUpstream creates a repo with one commit and returns its path
func newUpstream(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "upstream")
	runGit(t, "init", "-q", dir)
	for name, body := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0644))
	}
	runGit(t, "-C", dir, "add", "-A")
	runGit(t, "-C", dir, "commit", "-q", "-m", "init")
	return dir
}

// localRepo is a repo whose clone URL is a local upstream path
func localRepo(owner, name, cloneURL string) *gh.Repository {
	return &gh.Repository{
		Name:     gh.Ptr(name),
		FullName: gh.Ptr(owner + "/" + name),
		Owner:    &gh.User{Login: gh.Ptr(owner)},
		CloneURL: gh.Ptr(cloneURL),
	}
}
