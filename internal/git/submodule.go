package git

import (
	"context"
	"os"
	"path/filepath"
)

// SubmoduleUpdate checks out all submodules (recursively) of a repository.
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

	return g.run(ctx, withConfig(pairs...), ErrSubmoduleFailed,
		"-C", repoDir, "submodule", "update", "--init", "--recursive")
}
