package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/Didstopia/githubby/internal/config"
)

// TestMain keeps every test away from the real keychain, home directory, git config and network.
func TestMain(m *testing.M) {
	keyring.MockInit()
	homedir.DisableCache = true
	autoUpdateEnabled = false

	home, err := os.MkdirTemp("", "githubby-cli-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home) // Windows reads the home directory from here
	os.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	setGitIdentity()
	os.Unsetenv("GITHUB_TOKEN")

	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

// setGitIdentity lets test commits work on machines with no git user set up.
func setGitIdentity() {
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		os.Setenv(k, "Test")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		os.Setenv(k, "test@example.com")
	}
}

// testEnv gives one test its own HOME, an empty keychain and no GITHUB_TOKEN.
func testEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GITHUB_TOKEN", "")
	// The config loader reads env vars by flag name, so $USER would fill the --user flag.
	t.Setenv("USER", "")
	t.Setenv("ORG", "")
	t.Setenv("TARGET", "")
	keyring.MockInit()
	configLoader = config.NewLoader() // the loader remembers the first HOME it saw
	resetFlags(rootCmd)               // flag variables are globals; start every test from the defaults
	return home
}

// resetFlags puts every flag of every command back to its default, because the
// command tree and its flag variables are package globals shared by all tests.
func resetFlags(cmd *cobra.Command) {
	reset := func(fs *pflag.FlagSet) {
		fs.VisitAll(func(f *pflag.Flag) {
			if sv, ok := f.Value.(pflag.SliceValue); ok {
				def := strings.Trim(f.DefValue, "[]")
				if def == "" {
					_ = sv.Replace(nil)
				} else {
					_ = sv.Replace(strings.Split(def, ","))
				}
			} else {
				_ = f.Value.Set(f.DefValue)
			}
			f.Changed = false
		})
	}
	reset(cmd.Flags())
	reset(cmd.PersistentFlags())
	for _, c := range cmd.Commands() {
		resetFlags(c)
	}
}

func setContextRecursive(cmd *cobra.Command, ctx context.Context) {
	cmd.SetContext(ctx)
	for _, c := range cmd.Commands() {
		setContextRecursive(c, ctx)
	}
}

type execResult struct {
	stdout string
	stderr string
	err    error
}

// runCLI runs the real command tree with the given args and stdin and captures both streams.
func runCLI(t *testing.T, stdin string, args ...string) execResult {
	return runCLIContext(t, context.Background(), stdin, args...)
}

func runCLIContext(t *testing.T, ctx context.Context, stdin string, args ...string) execResult {
	t.Helper()
	resetFlags(rootCmd)
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	setContextRecursive(rootCmd, ctx) // cobra keeps a subcommand's old context between runs
	rootCmd.SetArgs(args)

	var res execResult
	restoreIn := replaceStdin(t, stdin)
	defer restoreIn()
	res.stdout, res.stderr = captureOutput(t, func() { res.err = rootCmd.Execute() })
	return res
}

func replaceStdin(t *testing.T, content string) func() {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdin")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	_, err = f.Seek(0, 0)
	require.NoError(t, err)
	orig := os.Stdin
	os.Stdin = f
	return func() {
		os.Stdin = orig
		f.Close()
	}
}

// captureOutput runs fn with os.Stdout and os.Stderr redirected into buffers.
func captureOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	errR, errW, err := os.Pipe()
	require.NoError(t, err)

	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	var wg sync.WaitGroup
	var outBuf, errBuf bytes.Buffer
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(&outBuf, outR) }()
	go func() { defer wg.Done(); _, _ = io.Copy(&errBuf, errR) }()

	func() {
		defer func() {
			os.Stdout, os.Stderr = origOut, origErr
			outW.Close()
			errW.Close()
		}()
		fn()
	}()
	wg.Wait()
	return outBuf.String(), errBuf.String()
}

// fakeRoute answers one request; keys are "METHOD /path" without the query string.
type fakeRoute func(r *http.Request) *http.Response

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fakeGitHub swaps the default transport for a router. Unrouted requests fail the test.
func fakeGitHub(t *testing.T, routes map[string]fakeRoute) *[]string {
	t.Helper()
	var mu sync.Mutex
	seen := []string{}
	orig := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		key := r.Method + " " + r.URL.Path
		mu.Lock()
		seen = append(seen, key)
		mu.Unlock()
		if route, ok := routes[key]; ok {
			return route(r), nil
		}
		t.Errorf("unexpected request: %s %s", key, r.URL.String())
		return jsonResponse(404, `{"message":"not routed"}`), nil
	})
	t.Cleanup(func() { http.DefaultTransport = orig })
	return &seen
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func formResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/x-www-form-urlencoded"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func route(status int, body string) fakeRoute {
	return func(*http.Request) *http.Response { return jsonResponse(status, body) }
}

const userJSON = `{"login":"octo","name":"Octo Cat","email":"octo@example.com"}`
