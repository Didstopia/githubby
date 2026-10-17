package cli

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	gh "github.com/google/go-github/v86/github"
	"github.com/stretchr/testify/assert"

	gitpkg "github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
	"github.com/Didstopia/githubby/internal/verify"
)

func resetVerifyFlags(t *testing.T) {
	t.Helper()
	target, user, org, all, gists, meta := verifyTarget, verifyUser, verifyOrg, verifyAllOrgs, verifyGists, verifyMetadata
	t.Cleanup(func() {
		verifyTarget, verifyUser, verifyOrg, verifyAllOrgs, verifyGists, verifyMetadata = target, user, org, all, gists, meta
	})
}

func TestRunVerify_Validation(t *testing.T) {
	resetVerifyFlags(t)
	verifyTarget, verifyUser, verifyOrg, verifyAllOrgs = "", "", "", false
	assert.ErrorContains(t, runVerify(verifyCmd, nil), "--target is required")

	verifyTarget = t.TempDir()
	assert.ErrorContains(t, runVerify(verifyCmd, nil), "--all-orgs")
}

func TestExecuteVerify(t *testing.T) {
	g, err := gitpkg.NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}
	resetVerifyFlags(t)

	m := github.NewMockClient()
	verifyTarget, verifyOrg, verifyUser, verifyAllOrgs, verifyGists, verifyMetadata = t.TempDir(), "Didstopia", "", false, false, false

	// GitHub lists nothing: never a pass, but the report is still written
	assert.ErrorContains(t, executeVerify(context.Background(), m, g), "backup check failed")
	assert.FileExists(t, filepath.Join(verifyTarget, verify.ReportFile))

	// A repo that was never backed up fails the run
	m.ListOrgReposFunc = func(ctx context.Context, org string, o *github.ListOptions) ([]*gh.Repository, error) {
		return []*gh.Repository{repoAt("Didstopia", "tool", localUpstream(t))}, nil
	}
	assert.ErrorContains(t, executeVerify(context.Background(), m, g), "backup check failed with 1 problem(s)")

	// Listing errors fail too
	m.ListOrgReposFunc = func(ctx context.Context, org string, o *github.ListOptions) ([]*gh.Repository, error) {
		return nil, errors.New("down")
	}
	assert.Error(t, executeVerify(context.Background(), m, g))
}

func TestPrintVerifySummary(t *testing.T) {
	rep := &verify.Report{Target: "/x", Checks: []string{"git fsck"}, Missing: []string{"a/b"}, Errors: []string{"e"}}
	assert.NotPanics(t, func() {
		printVerifySummary(rep)
		printVerifySummary(&verify.Report{Target: "/x", OK: true})
	})
}
