package git

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeInstaller puts stand-in package manager commands on an otherwise empty PATH.
// The runner command (brew, or sudo on Linux) drops a git-lfs next to itself when
// it succeeds, so Install can find it afterwards.
func fakeInstaller(t *testing.T, runner string, others []string, runnerExit int, dropsLFS bool) string {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("fake package managers are shell scripts")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	body := "#!/bin/sh\necho \"$0 $*\" >> " + log + "\n"
	if dropsLFS {
		body += "printf '#!/bin/sh\\nexit 0\\n' > " + filepath.Join(dir, "git-lfs") + "\n/bin/chmod +x " + filepath.Join(dir, "git-lfs") + "\n"
	}
	body += "exit " + string(rune('0'+runnerExit)) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, runner), []byte(body), 0755))
	for _, name := range others {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0755))
	}
	t.Setenv("PATH", dir)
	return log
}

// managerFor returns the runner and the package manager probes this OS uses
func managerFor(manager string) (runner string, others []string) {
	if runtime.GOOS == "darwin" {
		return "brew", nil
	}
	return "sudo", []string{manager}
}

func TestLFS_Install(t *testing.T) {
	ctx := context.Background()
	g := &Git{GitPath: "git", Quiet: true}

	t.Run("installs and finds git-lfs", func(t *testing.T) {
		runner, others := managerFor("apt-get")
		log := fakeInstaller(t, runner, others, 0, true)
		l := &LFS{Git: g, Quiet: true}

		require.NoError(t, l.Install(ctx))
		assert.True(t, l.IsInstalled())
		assert.Contains(t, readLog(t, log), "git-lfs")
	})

	t.Run("prints output when not quiet", func(t *testing.T) {
		runner, others := managerFor("apt-get")
		fakeInstaller(t, runner, others, 0, true)
		require.NoError(t, (&LFS{Git: g}).Install(ctx))
	})

	t.Run("install command fails", func(t *testing.T) {
		runner, others := managerFor("apt-get")
		fakeInstaller(t, runner, others, 1, false)
		err := (&LFS{Git: g, Quiet: true}).Install(ctx)
		assert.ErrorIs(t, err, ErrLFSInstallFail)
	})

	t.Run("install ran but git-lfs is still missing", func(t *testing.T) {
		runner, others := managerFor("apt-get")
		fakeInstaller(t, runner, others, 0, false)
		l := &LFS{Git: g, Quiet: true}
		assert.ErrorIs(t, l.Install(ctx), ErrLFSNotInstalled)
		assert.False(t, l.IsInstalled())
	})

	t.Run("no package manager", func(t *testing.T) {
		if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
			t.Skip()
		}
		t.Setenv("PATH", t.TempDir())
		err := (&LFS{Git: g, Quiet: true}).Install(ctx)
		assert.ErrorIs(t, err, ErrLFSInstallFail)
	})
}

func TestLFS_Install_LinuxPackageManagers(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux package manager probing")
	}
	for _, manager := range []string{"apt-get", "dnf", "yum", "pacman"} {
		t.Run(manager, func(t *testing.T) {
			log := fakeInstaller(t, "sudo", []string{manager}, 0, true)
			l := &LFS{Git: &Git{GitPath: "git"}, Quiet: true}
			require.NoError(t, l.Install(context.Background()))
			assert.Contains(t, readLog(t, log), manager)
		})
	}
}

func TestLFS_EnsureLFS_InstallsThenInitializes(t *testing.T) {
	g := quietGit(t)
	for _, quiet := range []bool{true, false} {
		runner, others := managerFor("apt-get")
		fakeInstaller(t, runner, others, 0, true)
		t.Chdir(t.TempDir())
		l := &LFS{Git: g, Quiet: quiet}
		require.NoError(t, l.EnsureLFS(context.Background()))
	}
}

func TestLFS_EnsureLFS_InstallFailure(t *testing.T) {
	g := quietGit(t)
	runner, others := managerFor("apt-get")
	fakeInstaller(t, runner, others, 1, false)
	l := &LFS{Git: g, Quiet: true}
	assert.ErrorIs(t, l.EnsureLFS(context.Background()), ErrLFSInstallFail)
}
