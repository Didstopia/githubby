package auth

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cli/oauth/device"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fakeNetwork replaces the default transport for the test; the auth code builds
// plain http.Clients, so this is the only seam needed.
func fakeNetwork(t *testing.T, fn roundTripFunc) {
	t.Helper()
	orig := http.DefaultTransport
	http.DefaultTransport = fn
	t.Cleanup(func() { http.DefaultTransport = orig })
}

func formResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/x-www-form-urlencoded; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func jsonResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

const codeBody = "device_code=DEV&user_code=ABCD-1234&verification_uri=https%3A%2F%2Fexample.com%2Fdevice&interval=0&expires_in=60"

func TestDefaultDeviceFlowOptions(t *testing.T) {
	o := DefaultDeviceFlowOptions()
	assert.Equal(t, GitHubOAuthClientID, o.ClientID)
	assert.Equal(t, []string{"repo", "read:org"}, o.Scopes)
	assert.Equal(t, DefaultHostname, o.Hostname)
}

func TestRunDeviceFlow_Success(t *testing.T) {
	var urls []string
	fakeNetwork(t, func(r *http.Request) (*http.Response, error) {
		urls = append(urls, r.URL.String())
		if strings.HasSuffix(r.URL.Path, "/device/code") {
			return formResp(200, codeBody), nil
		}
		return formResp(200, "access_token=tok123&token_type=bearer"), nil
	})

	var gotCode *device.CodeResponse
	res, err := RunDeviceFlow(context.Background(), &DeviceFlowOptions{
		OnCode: func(c *device.CodeResponse) { gotCode = c },
	})
	require.NoError(t, err)
	assert.Equal(t, "tok123", res.Token)
	assert.Equal(t, "bearer", res.TokenType)
	require.NotNil(t, gotCode)
	assert.Equal(t, "ABCD-1234", gotCode.UserCode)
	require.Len(t, urls, 2)
	assert.Equal(t, "https://github.com/login/device/code", urls[0])
	assert.Equal(t, "https://github.com/login/oauth/access_token", urls[1])
}

func TestRunDeviceFlow_NilOptsAndEnterpriseHost(t *testing.T) {
	var urls []string
	fakeNetwork(t, func(r *http.Request) (*http.Response, error) {
		urls = append(urls, r.URL.String())
		return nil, io.ErrUnexpectedEOF
	})
	_, err := RunDeviceFlow(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to request device code")

	_, err = RunDeviceFlow(context.Background(), &DeviceFlowOptions{Hostname: "ghe.example.com"})
	require.Error(t, err)
	require.Len(t, urls, 2)
	assert.Equal(t, "https://ghe.example.com/login/device/code", urls[1])
}

func TestRunDeviceFlow_EnterpriseTokenURL(t *testing.T) {
	var urls []string
	fakeNetwork(t, func(r *http.Request) (*http.Response, error) {
		urls = append(urls, r.URL.String())
		if strings.HasSuffix(r.URL.Path, "/device/code") {
			return formResp(200, codeBody), nil
		}
		return formResp(200, "access_token=t&token_type=bearer"), nil
	})
	_, err := RunDeviceFlow(context.Background(), &DeviceFlowOptions{Hostname: "ghe.example.com"})
	require.NoError(t, err)
	require.Len(t, urls, 2)
	assert.Equal(t, "https://ghe.example.com/login/oauth/access_token", urls[1])
}

func TestRunDeviceFlow_PollFails(t *testing.T) {
	fakeNetwork(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/device/code") {
			return formResp(200, codeBody), nil
		}
		return formResp(200, "error=access_denied&error_description=nope"), nil
	})
	_, err := RunDeviceFlow(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to complete device flow")
}

func TestRequestDeviceCodeAndPoll(t *testing.T) {
	fakeNetwork(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/device/code") {
			return formResp(200, codeBody), nil
		}
		return formResp(200, "access_token=pt&token_type=bearer"), nil
	})
	code, err := RequestDeviceCode(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "DEV", code.DeviceCode)

	res, err := PollForToken(context.Background(), code)
	require.NoError(t, err)
	assert.Equal(t, "pt", res.Token)
}

func TestRequestDeviceCodeAndPoll_Errors(t *testing.T) {
	fakeNetwork(t, func(r *http.Request) (*http.Response, error) {
		return nil, io.ErrUnexpectedEOF
	})
	_, err := RequestDeviceCode(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to request device code")

	_, err = PollForToken(context.Background(), &device.CodeResponse{ExpiresIn: 60})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to complete device flow")
}

func TestGetDeviceFlowInstructions(t *testing.T) {
	tests := []struct {
		name, uri, host, wantURL string
	}{
		{"uses server uri", "https://x.example/dev", "", "https://x.example/dev"},
		{"default host", "", "", "https://github.com/login/device"},
		{"explicit github.com", "", "github.com", "https://github.com/login/device"},
		{"enterprise host", "", "ghe.example.com", "https://ghe.example.com/login/device"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := GetDeviceFlowInstructions(&device.CodeResponse{UserCode: "CODE-1", VerificationURI: tt.uri}, tt.host)
			assert.Contains(t, out, "CODE-1")
			assert.Contains(t, out, tt.wantURL)
		})
	}
}

func TestOpenBrowser_UsesPlatformOpener(t *testing.T) {
	var name string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "linux":
		name = "xdg-open"
	default:
		t.Skip("no fake opener for " + runtime.GOOS)
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "called")
	script := "#!/bin/sh\necho \"$1\" > " + marker + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755)) //nolint:gosec // test script must be executable
	t.Setenv("PATH", dir)

	require.NoError(t, OpenBrowser("https://example.com"))
	assert.Eventually(t, func() bool {
		b, err := os.ReadFile(marker)
		return err == nil && strings.TrimSpace(string(b)) == "https://example.com"
	}, 5*time.Second, 10*time.Millisecond)
}

func TestOpenBrowser_MissingOpener(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip()
	}
	t.Setenv("PATH", t.TempDir())
	assert.Error(t, OpenBrowser("https://example.com"))
}
