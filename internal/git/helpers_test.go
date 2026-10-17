package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

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
	runGit(t, "init", "-q", "-b", "main", dir)
	runGit(t, "-C", dir, "config", "user.email", "test@example.com")
	runGit(t, "-C", dir, "config", "user.name", "Test")
	for name, body := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0644))
	}
	runGit(t, "-C", dir, "add", "-A")
	runGit(t, "-C", dir, "commit", "-q", "-m", "init")
	return dir
}

// fakeGit writes a stand-in git that logs its args and GIT_CONFIG env to a file
func fakeGit(t *testing.T) (gitPath, logPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake git is a shell script")
	}
	dir := t.TempDir()
	logPath = filepath.Join(dir, "log")
	gitPath = filepath.Join(dir, "git")
	script := "#!/bin/sh\necho \"ARGS $*\" >> " + logPath + "\nenv | grep '^GIT_CONFIG_' | sort >> " + logPath + "\n"
	require.NoError(t, os.WriteFile(gitPath, []byte(script), 0755))
	return gitPath, logPath
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}
