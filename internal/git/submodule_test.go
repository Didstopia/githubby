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
	assert.Contains(t, log, "GIT_CONFIG_COUNT=4")
	assert.Contains(t, log, "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader")
	assert.Contains(t, log, "GIT_CONFIG_VALUE_0=Authorization: Basic eC1hY2Nlc3MtdG9rZW46c2VjcmV0")
	assert.Contains(t, log, "GIT_CONFIG_KEY_2=url.https://github.com/.insteadOf")
	assert.Contains(t, log, "GIT_CONFIG_VALUE_2=git@github.com:")
	assert.Contains(t, log, "GIT_CONFIG_VALUE_3=ssh://git@github.com/")
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

func TestSubmoduleStatus(t *testing.T) {
	g, err := NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	t.Setenv("GIT_CONFIG_PARAMETERS", "'protocol.file.allow'='always'")

	inner := newUpstream(t, map[string]string{"inner.txt": "inner"})
	child := newUpstream(t, map[string]string{"child.txt": "child"})
	runGit(t, "-C", child, "submodule", "add", "-q", inner, "deep")
	runGit(t, "-C", child, "commit", "-q", "-m", "add inner")
	other := newUpstream(t, map[string]string{"o.txt": "o"})
	parent := newUpstream(t, map[string]string{"README": "parent"})
	runGit(t, "-C", parent, "submodule", "add", "-q", child, "lib/my child")
	runGit(t, "-C", parent, "submodule", "add", "-q", other, "other")
	runGit(t, "-C", parent, "commit", "-q", "-m", "add submodules")
	childSHA := runGit(t, "-C", child, "rev-parse", "HEAD")

	clone := filepath.Join(t.TempDir(), "clone")
	require.NoError(t, g.Clone(ctx, parent, clone))
	subs, err := g.SubmoduleStatus(ctx, clone)
	require.NoError(t, err)
	require.Len(t, subs, 2, "nested ones are unknown until the parent is checked out")
	assert.Equal(t, Submodule{Path: "lib/my child", SHA: childSHA, State: '-'}, subs[0])

	require.NoError(t, g.SubmoduleUpdate(ctx, clone))
	runGit(t, "-C", filepath.Join(clone, "other"), "commit", "-q", "--allow-empty", "-m", "moved")
	subs, err = g.SubmoduleStatus(ctx, clone)
	require.NoError(t, err)
	require.Len(t, subs, 3)
	states := map[string]byte{}
	for _, s := range subs {
		states[s.Path] = s.State
	}
	assert.Equal(t, map[string]byte{"lib/my child": ' ', "lib/my child/deep": ' ', "other": '+'}, states)

	// No .gitmodules, no submodules
	subs, err = g.SubmoduleStatus(ctx, filepath.Join(child, "deep"))
	require.NoError(t, err)
	assert.Empty(t, subs)

	bad := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bad, ".gitmodules"), nil, 0644))
	_, err = g.SubmoduleStatus(ctx, bad)
	assert.ErrorIs(t, err, ErrSubmoduleStatusFailed)
}
