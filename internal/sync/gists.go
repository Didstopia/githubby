package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	gh "github.com/google/go-github/v86/github"

	"github.com/Didstopia/githubby/internal/fileutil"
)

// GistsDir returns where a user's gists live under target. It sits next to <login>/,
// not in it, so a repo named "gists" can't clash with it.
func GistsDir(target, login string) string {
	return filepath.Join(target, login+".gists")
}

// SyncGists clones or fetches every gist of the token's account (secret ones too)
// into <target>/<login>.gists/<id>, with the gist's API data in <id>.json.
// Results go into result; the error is only for when the gists can't be listed.
func (s *Syncer) SyncGists(ctx context.Context, result *Result) error {
	user, err := s.ghClient.GetAuthenticatedUser(ctx)
	if err != nil {
		return fmt.Errorf("failed to get authenticated user: %w", err)
	}

	gists, err := s.ghClient.ListGists(ctx)
	if err != nil {
		return fmt.Errorf("failed to list gists: %w", err)
	}

	dir := GistsDir(s.opts.Target, user.GetLogin())
	for _, gist := range gists {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		name := "gist " + gist.GetID()
		if s.opts.DryRun {
			result.Gists = append(result.Gists, name)
			continue
		}

		if err := s.syncGist(ctx, gist, dir); err != nil {
			result.Failed[name] = err
			continue
		}
		result.Gists = append(result.Gists, name)
	}
	return nil
}

func (s *Syncer) syncGist(ctx context.Context, gist *gh.Gist, dir string) error {
	localPath := filepath.Join(dir, gist.GetID())

	var err error
	if s.git.IsGitRepo(localPath) {
		err = withGitRetry(ctx, gitRetryConfig(),
			func() error { return s.git.FetchAll(ctx, localPath) },
			nil, isTransientGitError)
	} else {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		err = withGitRetry(ctx, gitRetryConfig(),
			func() error { return s.git.Clone(ctx, gist.GetGitPullURL(), localPath) },
			func() error { return os.RemoveAll(localPath) },
			isTransientGitError)
	}
	if err != nil {
		return err
	}

	return fileutil.WriteJSON(localPath+".json", gist)
}
