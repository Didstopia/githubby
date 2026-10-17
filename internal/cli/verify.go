package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Didstopia/githubby/internal/auth"
	gherrors "github.com/Didstopia/githubby/internal/errors"
	gitpkg "github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
	"github.com/Didstopia/githubby/internal/verify"
)

var (
	verifyTarget   string
	verifyUser     string
	verifyOrg      string
	verifyAllOrgs  bool
	verifyGists    bool
	verifyMetadata bool
)

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Check that a backup holds everything GitHub has",
	Long: `Check a backup made by sync against GitHub.

Every repo (private ones too), wiki and gist GitHub lists must exist locally,
pass git fsck, have every branch at the same commit as on GitHub, and have all
its LFS objects. With --metadata, the issue and pull request counts must match.

A check that can't run counts as a failure. The exit code is 0 only when
everything is clean. A report is written to <target>/verify-report.json.

Examples:
  # Check a full backup of your account and all its organizations
  githubby verify --all-orgs --gists --metadata --target ~/backup

  # Check one organization
  githubby verify --org <orgname> --target ~/backup`,
	RunE: runVerify,
}

func init() {
	verifyCmd.Flags().StringVarP(&verifyTarget, "target", "T", "", "Backup directory to check")
	verifyCmd.Flags().StringVarP(&verifyUser, "user", "u", "", "GitHub username whose repositories to check")
	verifyCmd.Flags().StringVarP(&verifyOrg, "org", "o", "", "GitHub organization whose repositories to check")
	verifyCmd.Flags().BoolVar(&verifyAllOrgs, "all-orgs", false, "Check your own repositories plus every organization you belong to")
	verifyCmd.Flags().BoolVar(&verifyGists, "gists", false, "Also check your gists")
	verifyCmd.Flags().BoolVar(&verifyMetadata, "metadata", false, "Also check the metadata export")

	verifyCmd.MarkFlagsMutuallyExclusive("org", "user")
	verifyCmd.MarkFlagsMutuallyExclusive("org", "all-orgs")

	rootCmd.AddCommand(verifyCmd)
}

func runVerify(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if verifyTarget == "" {
		return fmt.Errorf("--target is required")
	}
	if verifyUser == "" && verifyOrg == "" && !verifyAllOrgs {
		return fmt.Errorf("either --user, --org or --all-orgs must be specified")
	}

	resolvedToken, err := auth.GetToken(ctx, token, "")
	if err != nil || resolvedToken.Token == "" {
		return gherrors.NewAuthError()
	}

	git, err := gitpkg.NewQuietWithToken(resolvedToken.Token)
	if err != nil {
		return fmt.Errorf("git initialization failed: %w", err)
	}
	git.Host = resolvedToken.Hostname

	return executeVerify(ctx, github.NewClient(resolvedToken.Token), git)
}

// executeVerify runs the checks, prints the summary and fails unless all is clean
func executeVerify(ctx context.Context, client github.Client, git *gitpkg.Git) error {
	target, err := filepath.Abs(verifyTarget)
	if err != nil {
		return err
	}

	fmt.Printf("Verifying backup in %s\n", target)
	rep, err := verify.New(client, git, &verify.Options{
		Target:   target,
		AllOrgs:  verifyAllOrgs,
		User:     verifyUser,
		Org:      verifyOrg,
		Gists:    verifyGists,
		Metadata: verifyMetadata,
	}).Run(ctx)
	if rep != nil {
		printVerifySummary(rep)
	}
	if err != nil {
		return fmt.Errorf("failed to write the report: %w", err)
	}
	if !rep.OK {
		return fmt.Errorf("backup check failed with %d problem(s)", rep.Problems())
	}
	return nil
}

func printVerifySummary(rep *verify.Report) {
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("Verify Summary")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("Checks: %s\n", strings.Join(rep.Checks, ", "))

	sections := []struct {
		title string
		items []string
	}{
		{"Missing locally", rep.Missing},
		{"Corrupt (git fsck)", rep.Corrupt},
		{"Stale branches", rep.Stale},
		{"Missing LFS objects", rep.LFS},
		{"Metadata problems", rep.Metadata},
		{"Checks that could not run", rep.Errors},
	}
	for _, s := range sections {
		if len(s.items) == 0 {
			continue
		}
		fmt.Printf("\n%s (%d):\n", s.title, len(s.items))
		for _, item := range s.items {
			fmt.Printf("  - %s\n", item)
		}
	}

	fmt.Println(strings.Repeat("=", 50))
	if rep.OK {
		fmt.Printf("OK: %d repos, wikis and gists checked, no problems\n", rep.Checked)
	} else {
		fmt.Printf("FAILED: %d checked, %d problem(s)\n", rep.Checked, rep.Problems())
	}
	fmt.Printf("Report: %s\n", filepath.Join(rep.Target, verify.ReportFile))
}
