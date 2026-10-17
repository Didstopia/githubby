package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubmoduleUpdate(t *testing.T) {
	g, err := NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()

	child := newUpstream(t, map[string]string{"child.txt": "child"})
	parent := newUpstream(t, map[string]string{"README": "parent"})
	runGit(t, "-C", parent, "-c", "protocol.file.allow=always", "submodule", "add", "-q", child, "lib/child")
	runGit(t, "-C", parent, "commit", "-q", "-m", "add submodule")

	clone := filepath.Join(t.TempDir(), "clone")
	require.NoError(t, g.Clone(ctx, parent, clone))
	_, err = os.Stat(filepath.Join(clone, "lib", "child", "child.txt"))
	require.True(t, os.IsNotExist(err), "plain clone should not have the submodule yet")

	t.Setenv("GIT_CONFIG_PARAMETERS", "'protocol.file.allow'='always'")
	require.NoError(t, g.SubmoduleUpdate(ctx, clone))

	body, err := os.ReadFile(filepath.Join(clone, "lib", "child", "child.txt"))
	require.NoError(t, err)
	assert.Equal(t, "child", string(body))
}

func TestSubmoduleUpdate_NoGitmodulesIsNoop(t *testing.T) {
	gitPath, logPath := fakeGit(t)
	g := &Git{GitPath: gitPath}

	require.NoError(t, g.SubmoduleUpdate(context.Background(), t.TempDir()))
	_, err := os.Stat(logPath)
	assert.True(t, os.IsNotExist(err), "git should not run")
}

func TestSubmoduleUpdate_PassesTokenAndHTTPSRewrite(t *testing.T) {
	gitPath, logPath := fakeGit(t)
	g := &Git{GitPath: gitPath, Token: "secret", Quiet: true}

	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitmodules"), []byte("[submodule]"), 0644))
	require.NoError(t, g.SubmoduleUpdate(context.Background(), repo))

	log := readLog(t, logPath)
	assert.Contains(t, log, "ARGS -C "+repo+" submodule update --init --recursive")
	assert.Contains(t, log, "GIT_CONFIG_COUNT=3")
	assert.Contains(t, log, "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader")
	assert.Contains(t, log, "GIT_CONFIG_VALUE_0=Authorization: Basic eC1hY2Nlc3MtdG9rZW46c2VjcmV0")
	assert.Contains(t, log, "GIT_CONFIG_KEY_1=url.https://github.com/.insteadOf")
	assert.Contains(t, log, "GIT_CONFIG_VALUE_1=git@github.com:")
	assert.Contains(t, log, "GIT_CONFIG_VALUE_2=ssh://git@github.com/")
}

func TestSubmoduleUpdate_FailureWrapsError(t *testing.T) {
	g, err := NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	runGit(t, "init", "-q", repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitmodules"),
		[]byte("[submodule \"x\"]\n\tpath = x\n\turl = /nonexistent/repo\n"), 0644))
	runGit(t, "-C", repo, "update-index", "--add", "--cacheinfo", "160000,1111111111111111111111111111111111111111,x")

	err = g.SubmoduleUpdate(context.Background(), repo)
	assert.ErrorIs(t, err, ErrSubmoduleFailed)
}
