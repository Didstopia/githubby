package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrSubmoduleStatusFailed is returned when git can't report on the submodules
var ErrSubmoduleStatusFailed = errors.New("git submodule status failed")

// Submodule is one line of git submodule status
type Submodule struct {
	// Path is relative to the top repo, nested submodules included
	Path string
	// SHA is the commit the parent records for it
	SHA string
	// State is ' ' when checked out at SHA, '-' when not checked out,
	// '+' when at another commit and 'U' when it has merge conflicts
	State byte
}

// SubmoduleUpdate syncs submodule URLs and checks out all submodules (recursively) of a repository.
// The token goes along per command, and SSH submodule URLs are fetched over HTTPS,
// since a backup host usually has no SSH keys. Repos without .gitmodules are a no-op.
func (g *Git) SubmoduleUpdate(ctx context.Context, repoDir string) error {
	if _, err := os.Stat(filepath.Join(repoDir, ".gitmodules")); err != nil {
		return nil
	}

	https := "url.https://" + g.host() + "/.insteadOf"
	pairs := append(g.authConfig(),
		https, "git@"+g.host()+":",
		https, "ssh://git@"+g.host()+"/",
	)

	// Picks up submodule URLs that changed in .gitmodules since the last checkout
	if err := g.run(ctx, nil, ErrSubmoduleFailed, "-C", repoDir, "submodule", "sync", "--quiet", "--recursive"); err != nil {
		return err
	}
	return g.run(ctx, withConfig(pairs...), ErrSubmoduleFailed,
		"-C", repoDir, "submodule", "update", "--init", "--recursive")
}

// SubmoduleStatus lists every submodule, nested ones too, with its state.
// Repos without .gitmodules have none.
func (g *Git) SubmoduleStatus(ctx context.Context, repoDir string) ([]Submodule, error) {
	if _, err := os.Stat(filepath.Join(repoDir, ".gitmodules")); err != nil {
		return nil, nil
	}
	out, err := g.output(ctx, nil, ErrSubmoduleStatusFailed, "-C", repoDir, "submodule", "status", "--recursive")
	if err != nil {
		return nil, err
	}

	var subs []Submodule
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 2 {
			continue
		}
		sha, path, ok := strings.Cut(line[1:], " ")
		if !ok {
			continue
		}
		// A trailing " (<describe>)" follows the path when git can describe the commit
		if i := strings.LastIndex(path, " ("); i >= 0 && strings.HasSuffix(path, ")") {
			path = path[:i]
		}
		subs = append(subs, Submodule{Path: path, SHA: sha, State: line[0]})
	}
	return subs, nil
}
