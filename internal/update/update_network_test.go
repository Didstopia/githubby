package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fakeNetwork replaces the default transport for the test so nothing reaches GitHub.
func fakeNetwork(t *testing.T, fn roundTripFunc) {
	t.Helper()
	orig := http.DefaultTransport
	http.DefaultTransport = fn
	t.Cleanup(func() { http.DefaultTransport = orig })
}

func resp(status int, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func makeTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}))
	_, err := tw.Write(content)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

// releaseServer fakes the GitHub API for a single release "v<version>".
type releaseServer struct {
	version   string
	assetName string
	asset     []byte
	checksums string // body of checksums.txt; defaults to a valid one for asset
	failAsset bool
	releases  string // overrides the release list JSON when set
}

func (s *releaseServer) handler(t *testing.T) roundTripFunc {
	return func(r *http.Request) (*http.Response, error) {
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/releases"):
			if s.releases != "" {
				return resp(200, "application/json", s.releases), nil
			}
			if s.checksums == "" {
				sum := sha256.Sum256(s.asset)
				s.checksums = hex.EncodeToString(sum[:]) + "  " + s.assetName + "\n"
			}
			assets := fmt.Sprintf(`{"id":11,"name":%q,"size":%d,"browser_download_url":"https://example.com/a"}`, s.assetName, len(s.asset))
			assets += `,{"id":12,"name":"checksums.txt","size":10,"browser_download_url":"https://example.com/c"}`
			body := fmt.Sprintf(`[{"id":1,"tag_name":"v%s","name":"v%s","html_url":"https://example.com/rel","body":"notes","draft":false,"prerelease":false,"assets":[%s]}]`,
				s.version, s.version, assets)
			return resp(200, "application/json", body), nil
		case strings.HasSuffix(p, "/assets/11"):
			if s.failAsset {
				return resp(404, "application/json", `{"message":"gone"}`), nil
			}
			return resp(200, "application/octet-stream", string(s.asset)), nil
		case strings.HasSuffix(p, "/assets/12"):
			return resp(200, "text/plain", s.checksums), nil
		}
		t.Errorf("unexpected request %s", r.URL)
		return resp(404, "application/json", `{}`), nil
	}
}

func platformAssetName(version string) string {
	return fmt.Sprintf("githubby_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
}

func TestCheckForUpdate_Network(t *testing.T) {
	tests := []struct {
		name          string
		current       string
		latest        string
		wantAvailable bool
		wantErr       string
	}{
		{"newer available", "1.0.0", "1.2.0", true, ""},
		{"same version", "1.2.0", "1.2.0", false, ""},
		{"current is newer", "2.0.0", "1.2.0", false, ""},
		{"bad current version", "not-semver", "1.2.0", false, "failed to parse current version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &releaseServer{version: tt.latest, assetName: platformAssetName(tt.latest), asset: []byte("x")}
			fakeNetwork(t, srv.handler(t))

			res, err := NewUpdater(tt.current).CheckForUpdate(context.Background())
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantAvailable, res.Available)
			assert.Equal(t, tt.latest, res.LatestVersion)
			assert.Equal(t, tt.current, res.CurrentVersion)
			assert.Equal(t, "https://example.com/rel", res.ReleaseURL)
			assert.Equal(t, "notes", res.ReleaseNotes)
		})
	}
}

func TestCheckForUpdate_NoReleaseFound(t *testing.T) {
	srv := &releaseServer{releases: `[]`}
	fakeNetwork(t, srv.handler(t))

	res, err := NewUpdater("1.0.0").CheckForUpdate(context.Background())
	require.NoError(t, err)
	assert.False(t, res.Available)
	assert.Empty(t, res.LatestVersion)
}

func TestCheckForUpdate_APIError(t *testing.T) {
	fakeNetwork(t, func(r *http.Request) (*http.Response, error) {
		return nil, io.ErrUnexpectedEOF
	})
	_, err := NewUpdater("1.0.0").CheckForUpdate(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to detect latest version")
}

func TestUpdate_NoReleaseFound(t *testing.T) {
	srv := &releaseServer{releases: `[]`}
	fakeNetwork(t, srv.handler(t))

	res, err := NewUpdater("1.0.0").Update(context.Background())
	require.NoError(t, err)
	assert.False(t, res.Available)
}

func TestUpdate_APIError(t *testing.T) {
	fakeNetwork(t, func(r *http.Request) (*http.Response, error) {
		return nil, io.ErrUnexpectedEOF
	})
	_, err := NewUpdater("1.0.0").Update(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to detect latest version")
}

func TestUpdate_BadCurrentVersion(t *testing.T) {
	srv := &releaseServer{version: "1.2.0", assetName: platformAssetName("1.2.0"), asset: []byte("x")}
	fakeNetwork(t, srv.handler(t))

	_, err := NewUpdater("nope").Update(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse current version")
}

func TestUpdate_AlreadyLatest(t *testing.T) {
	srv := &releaseServer{version: "1.2.0", assetName: platformAssetName("1.2.0"), asset: []byte("x")}
	fakeNetwork(t, srv.handler(t))

	res, err := NewUpdater("1.2.0").Update(context.Background())
	require.NoError(t, err)
	assert.False(t, res.Available)
	assert.Equal(t, "1.2.0", res.LatestVersion)
}

func TestUpdate_DownloadFails(t *testing.T) {
	srv := &releaseServer{version: "1.2.0", assetName: platformAssetName("1.2.0"), asset: []byte("x"), failAsset: true}
	fakeNetwork(t, srv.handler(t))
	setExecutable(t)

	_, err := NewUpdater("1.0.0").Update(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to download asset")
}

func TestUpdate_ChecksumMismatch(t *testing.T) {
	archive := makeTarGz(t, "githubby", []byte("new binary"))
	srv := &releaseServer{
		version: "1.2.0", assetName: platformAssetName("1.2.0"), asset: archive,
		checksums: strings.Repeat("0", 64) + "  " + platformAssetName("1.2.0") + "\n",
	}
	fakeNetwork(t, srv.handler(t))
	target := setExecutable(t)

	_, err := NewUpdater("1.0.0").Update(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum")
	assert.Equal(t, "old binary", readFile(t, target), "target must stay untouched")
}

func TestUpdate_ArchiveWithoutExecutable(t *testing.T) {
	archive := makeTarGz(t, "README.md", []byte("hi"))
	srv := &releaseServer{version: "1.2.0", assetName: platformAssetName("1.2.0"), asset: archive}
	fakeNetwork(t, srv.handler(t))
	setExecutable(t)

	_, err := NewUpdater("1.0.0").Update(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to extract executable")
}

func TestUpdate_Success(t *testing.T) {
	archive := makeTarGz(t, "githubby", []byte("new binary"))
	sum := sha256.Sum256(archive)
	srv := &releaseServer{
		version: "1.2.0", assetName: platformAssetName("1.2.0"), asset: archive,
		checksums: hex.EncodeToString(sum[:]) + "  " + platformAssetName("1.2.0") + "\n",
	}
	fakeNetwork(t, srv.handler(t))
	target := setExecutable(t)

	res, err := NewUpdater("1.0.0").Update(context.Background())
	require.NoError(t, err)
	assert.True(t, res.Available)
	assert.Equal(t, "1.2.0", res.LatestVersion)
	assert.Equal(t, "new binary", readFile(t, target))
}

func TestUpdate_ApplyFails(t *testing.T) {
	archive := makeTarGz(t, "githubby", []byte("new binary"))
	srv := &releaseServer{version: "1.2.0", assetName: platformAssetName("1.2.0"), asset: archive}
	fakeNetwork(t, srv.handler(t))
	dir := t.TempDir()
	target := filepath.Join(dir, "githubby")
	require.NoError(t, os.WriteFile(target, []byte("old binary"), 0o755)) //nolint:gosec // test executable
	require.NoError(t, os.Chmod(dir, 0o500))                              //nolint:gosec // make the directory read-only
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })                        //nolint:gosec // restore so TempDir can clean up
	executablePath = func() (string, error) { return target, nil }
	t.Cleanup(func() { executablePath = defaultExecutablePath })
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	_, err := NewUpdater("1.0.0").Update(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to apply update")
}

func TestUpdate_ExecutablePathError(t *testing.T) {
	srv := &releaseServer{version: "1.2.0", assetName: platformAssetName("1.2.0"), asset: []byte("x")}
	fakeNetwork(t, srv.handler(t))
	executablePath = func() (string, error) { return "", io.ErrClosedPipe }
	t.Cleanup(func() { executablePath = defaultExecutablePath })

	_, err := NewUpdater("1.0.0").Update(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get executable path")
}

func setExecutable(t *testing.T) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "githubby")
	require.NoError(t, os.WriteFile(target, []byte("old binary"), 0o755)) //nolint:gosec // test executable
	executablePath = func() (string, error) { return target, nil }
	t.Cleanup(func() { executablePath = defaultExecutablePath })
	return target
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	return string(b)
}

func TestExtractExecutable(t *testing.T) {
	payload := []byte("binary-bytes")

	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, err := zw.Create("dist/githubby")
	require.NoError(t, err)
	_, err = w.Write(payload)
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	var gzbuf bytes.Buffer
	gw := gzip.NewWriter(&gzbuf)
	_, err = gw.Write(payload)
	require.NoError(t, err)
	require.NoError(t, gw.Close())

	tests := []struct {
		name    string
		data    []byte
		asset   string
		want    []byte
		wantErr string
	}{
		{"tar.gz", makeTarGz(t, "sub/githubby", payload), "x.tar.gz", payload, ""},
		{"tgz", makeTarGz(t, "githubby", payload), "x.TGZ", payload, ""},
		{"zip", zbuf.Bytes(), "x.zip", payload, ""},
		{"plain gz", gzbuf.Bytes(), "x.gz", payload, ""},
		{"raw binary", payload, "githubby", payload, ""},
		{"tar.gz missing exe", makeTarGz(t, "other", payload), "x.tar.gz", nil, "not found in tar.gz"},
		{"tar.gz corrupt", []byte("junk"), "x.tar.gz", nil, "gzip reader"},
		{"zip missing exe", zbuf.Bytes()[:0], "x.zip", nil, "zip reader"},
		{"plain gz corrupt", []byte("junk"), "x.gz", nil, "gzip reader"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractExecutable(tt.data, tt.asset)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractFromZip_MissingExecutable(t *testing.T) {
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, err := zw.Create("readme.txt")
	require.NoError(t, err)
	_, _ = w.Write([]byte("x"))
	require.NoError(t, zw.Close())

	_, err = extractFromZip(zbuf.Bytes(), "githubby")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in zip")
}

func TestMatchesExecutableName(t *testing.T) {
	osArch := runtime.GOOS + "_" + runtime.GOARCH
	tests := []struct {
		file string
		want bool
	}{
		{"githubby", true},
		{"githubby.exe", true},
		{"githubby_v1.2.3", true},
		{"githubby-1.2.3", true},
		{"githubby_" + osArch, true},
		{"githubby_1.2.3_" + osArch, true},
		{"githubby-test", false},
		{"README.md", false},
		{"xgithubby", false},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			assert.Equal(t, tt.want, matchesExecutableName(tt.file, "githubby"))
		})
	}
}
