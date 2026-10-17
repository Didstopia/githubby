package cli

import (
	"context"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/update"
)

func TestRootGlobalFlags(t *testing.T) {
	testEnv(t)
	res := runCLI(t, "", "version", "--verbose", "--dry-run", "--token", "abc")
	require.NoError(t, res.err)
	assert.True(t, verbose)
	assert.True(t, dryRun)
	assert.Equal(t, "abc", token)
	assert.Equal(t, logrus.DebugLevel, log.GetLevel())

	res = runCLI(t, "", "version")
	require.NoError(t, res.err)
	assert.False(t, verbose)
	assert.False(t, dryRun)
	assert.Equal(t, logrus.InfoLevel, log.GetLevel())
}

func TestRootCreatesDefaultConfigFile(t *testing.T) {
	home := testEnv(t)
	res := runCLI(t, "", "version")
	require.NoError(t, res.err)
	assert.FileExists(t, home+"/.githubby.yaml")
}

func TestConfigFileValuesFillFlags(t *testing.T) {
	home := testEnv(t)
	require.NoError(t, os.WriteFile(home+"/.githubby.yaml", []byte("verbose: true\n"), 0o600))
	res := runCLI(t, "", "version")
	require.NoError(t, res.err)
	assert.True(t, verbose, "verbose from the config file applies when the flag is absent")
}

func TestExecute_RunsACommand(t *testing.T) {
	testEnv(t)
	withVersion(t, "dev")
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })
	os.Args = []string{"githubby", "version"}
	resetFlags(rootCmd)
	rootCmd.SetArgs([]string{"version"})

	out, _ := captureOutput(t, Execute)
	assert.Contains(t, out, "githubby dev")
}

func TestExecute_WithoutArgsStartsTheTUI(t *testing.T) {
	testEnv(t)
	origArgs, origInteractive := os.Args, isInteractive
	t.Cleanup(func() { os.Args, isInteractive = origArgs, origInteractive })
	os.Args = []string{"githubby"}
	isInteractive = func() bool { return false }

	out, _ := captureOutput(t, Execute)
	assert.Contains(t, out, "Interactive mode is not available")
}

func TestStartBackgroundUpdateCheck(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })

	t.Run("skipped for commands that must not nag", func(t *testing.T) {
		withVersion(t, "1.0.0")
		for _, cmd := range []string{"update", "version", "help", "--help", "-h"} {
			os.Args = []string{"githubby", cmd}
			_, open := <-startBackgroundUpdateCheck(context.Background())
			assert.False(t, open, cmd)
		}
	})
	t.Run("skipped for dev builds", func(t *testing.T) {
		withVersion(t, "dev")
		os.Args = []string{"githubby", "sync"}
		_, open := <-startBackgroundUpdateCheck(context.Background())
		assert.False(t, open)
	})
	t.Run("reports an available update", func(t *testing.T) {
		withVersion(t, "1.0.0")
		os.Args = []string{"githubby", "sync"}
		fakeGitHub(t, releaseRoutes("2.0.0"))
		res := <-startBackgroundUpdateCheck(context.Background())
		require.NotNil(t, res)
		assert.True(t, res.Available)
		assert.Equal(t, "2.0.0", res.LatestVersion)
	})
	t.Run("a failed check yields nothing", func(t *testing.T) {
		withVersion(t, "1.0.0")
		os.Args = []string{"githubby", "sync"}
		fakeGitHub(t, map[string]fakeRoute{
			"GET /repos/Didstopia/githubby/releases": route(500, `{"message":"boom"}`),
		})
		res, open := <-startBackgroundUpdateCheck(context.Background())
		assert.Nil(t, res)
		assert.False(t, open)
	})
}

func TestShowUpdateNotification(t *testing.T) {
	t.Run("prints when an update exists", func(t *testing.T) {
		ch := make(chan *update.Result, 1)
		ch <- &update.Result{CurrentVersion: "1.0.0", LatestVersion: "2.0.0", Available: true}
		_, errOut := captureOutput(t, func() { showUpdateNotification(ch) })
		assert.Contains(t, errOut, "Update available: v1.0.0 -> v2.0.0")
	})
	t.Run("silent when nothing is available", func(t *testing.T) {
		ch := make(chan *update.Result, 1)
		ch <- &update.Result{Available: false}
		_, errOut := captureOutput(t, func() { showUpdateNotification(ch) })
		assert.Empty(t, errOut)
	})
	t.Run("does not wait for a slow check", func(t *testing.T) {
		ch := make(chan *update.Result) // never answers
		_, errOut := captureOutput(t, func() { showUpdateNotification(ch) })
		assert.Empty(t, errOut)
	})
	t.Run("closed channel", func(t *testing.T) {
		ch := make(chan *update.Result)
		close(ch)
		_, errOut := captureOutput(t, func() { showUpdateNotification(ch) })
		assert.Empty(t, errOut)
	})
}
