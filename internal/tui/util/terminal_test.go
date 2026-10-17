package util

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsInteractive_FalseWithoutTerminal(t *testing.T) {
	// Under `go test` stdin is not a terminal, so this must be false.
	assert.False(t, IsInteractive())
}

func TestIsInteractive_FalseWithNullStdin(t *testing.T) {
	devnull, err := os.Open(os.DevNull)
	require.NoError(t, err)
	defer devnull.Close()
	orig := os.Stdin
	os.Stdin = devnull
	t.Cleanup(func() { os.Stdin = orig })

	assert.False(t, IsInteractive())
}

func TestGetTerminalSize_Defaults(t *testing.T) {
	w, h := GetTerminalSize()
	assert.Equal(t, 80, w)
	assert.Equal(t, 24, h)
}

func TestBell_WritesBEL(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	Bell()
	require.NoError(t, w.Close())
	os.Stdout = orig

	out, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "\a", string(out))
}

func fakeTerminal(t *testing.T, stdin, stdout bool) {
	t.Helper()
	in, out := os.Stdin.Fd(), os.Stdout.Fd()
	orig := isTerminal
	isTerminal = func(fd uintptr) bool {
		switch fd {
		case in:
			return stdin
		case out:
			return stdout
		}
		return false
	}
	t.Cleanup(func() { isTerminal = orig })
}

var ciVars = []string{
	"CI", "GITHUB_ACTIONS", "GITLAB_CI", "TRAVIS", "CIRCLECI", "JENKINS_URL",
	"BUILDKITE", "DRONE", "TEAMCITY_VERSION", "TF_BUILD", "CODEBUILD_BUILD_ID",
}

func clearCI(t *testing.T) {
	t.Helper()
	for _, v := range ciVars {
		t.Setenv(v, "")
	}
}

func TestIsInteractive_TerminalsAndNoCI(t *testing.T) {
	clearCI(t)
	fakeTerminal(t, true, true)
	assert.True(t, IsInteractive())
}

func TestIsInteractive_FalseWhenStdoutIsNotATerminal(t *testing.T) {
	clearCI(t)
	fakeTerminal(t, true, false)
	assert.False(t, IsInteractive())
}

func TestIsInteractive_FalseInEachCIEnvironment(t *testing.T) {
	for _, v := range ciVars {
		t.Run(v, func(t *testing.T) {
			clearCI(t)
			fakeTerminal(t, true, true)
			t.Setenv(v, "1")
			assert.False(t, IsInteractive())
		})
	}
}

func TestIsTerminal_FalseForPipe(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer r.Close()
	defer w.Close()
	assert.False(t, isTerminal(r.Fd()))
}
