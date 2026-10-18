// Package metadata exports the parts of GitHub that live outside git (issues, pull
// requests, releases, settings, projects, ...) as JSON files next to the repos.
package metadata

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	gh "github.com/google/go-github/v86/github"

	"github.com/Didstopia/githubby/internal/fileutil"
	"github.com/Didstopia/githubby/internal/github"
)

// maxRateLimitWaits is how often one call may wait out a rate limit before giving up
const maxRateLimitWaits = 5

// serverErrorWait is the pause before retrying a call that got a 502, 503 or 504
const serverErrorWait = 10 * time.Second

// Exporter writes metadata JSON files under a target directory
type Exporter struct {
	gh       *gh.Client
	download *http.Client
	target   string

	// Verbose prints each file as it is written
	Verbose bool

	// sleep waits out rate limits; swapped out in tests
	sleep func(ctx context.Context, d time.Duration) error

	// partial collects GraphQL errors that came with usable data, for the current part
	partial []string
}

// New returns an Exporter. download fetches release assets from the storage host
// GitHub redirects to, so it must not carry the token.
func New(client *gh.Client, download *http.Client, target string) *Exporter {
	if download == nil {
		download = &http.Client{}
	}
	return &Exporter{gh: client, download: download, target: target, sleep: sleepCtx}
}

// Report collects what an export did
type Report struct {
	// Exported names every repo, org or user whose metadata was written
	Exported []string
	// Warnings are parts skipped because the token lacks a scope or access
	Warnings []string
	// Failed holds real failures, keyed by repo, org or user
	Failed map[string]error
}

// NewReport returns an empty report
func NewReport() *Report {
	return &Report{Failed: make(map[string]error)}
}

func (r *Report) fail(name string, err error) {
	if prev, ok := r.Failed[name]; ok {
		r.Failed[name] = errors.Join(prev, err)
		return
	}
	r.Failed[name] = err
}

// RepoDir returns where a repo's metadata lives under target
func RepoDir(target, owner, repo string) string {
	return filepath.Join(target, owner, repo+".meta")
}

// OwnerDir returns where an org's or user's metadata lives under target
func OwnerDir(target, login string) string {
	return filepath.Join(target, login+".meta")
}

// part is one JSON file of an export
type part struct {
	file string
	// scope names what the token needs for this part, for the warning
	scope string
	fetch func(ctx context.Context) (any, error)
}

// writeParts fetches and writes each part, sorting errors into warnings and failures
func (e *Exporter) writeParts(ctx context.Context, name, dir string, parts []part, rep *Report) {
	for _, p := range parts {
		if ctx.Err() != nil {
			rep.fail(name, ctx.Err())
			return
		}

		e.partial = nil
		data, err := p.fetch(ctx)
		for _, msg := range e.partial {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: %s is missing some entries the token can't see (%s)", name, p.file, msg))
		}
		if err == nil {
			err = fileutil.WriteJSON(filepath.Join(dir, p.file), data)
			if err == nil && e.Verbose {
				fmt.Printf("Wrote %s\n", filepath.Join(dir, p.file))
			}
		}
		if err == nil {
			continue
		}

		if isAccessError(err) {
			rep.Warnings = append(rep.Warnings,
				fmt.Sprintf("%s: %s skipped, needs %s (%s)", name, p.file, p.scope, accessDetail(err)))
			continue
		}
		rep.fail(name, fmt.Errorf("%s: %w", p.file, err))
	}
}

// isAccessError reports whether err means the token may not see this part
func isAccessError(err error) bool {
	switch statusOf(err) {
	case http.StatusForbidden, http.StatusNotFound:
		return true
	}
	var gqlErr *github.GraphQLError
	if errors.As(err, &gqlErr) {
		return gqlErr.HasType("INSUFFICIENT_SCOPES", "FORBIDDEN", "NOT_FOUND")
	}
	return false
}

// accessDetail keeps access warnings short; GraphQL repeats the same error once per field
func accessDetail(err error) string {
	var gqlErr *github.GraphQLError
	if errors.As(err, &gqlErr) && len(gqlErr.Errors) > 0 {
		if gqlErr.Errors[0].Type != "" {
			return gqlErr.Errors[0].Type
		}
		return gqlErr.Errors[0].Message
	}
	return err.Error()
}

// statusOf returns the HTTP status of a go-github error, or 0
func statusOf(err error) int {
	var respErr *gh.ErrorResponse
	if errors.As(err, &respErr) && respErr.Response != nil {
		return respErr.Response.StatusCode
	}
	return 0
}

// call runs fn, waiting out rate limits (primary, secondary and GraphQL) in between
func (e *Exporter) call(ctx context.Context, fn func() error) error {
	for waits := 0; ; waits++ {
		err := fn()
		wait, limited := rateLimitWait(err)
		if !limited || waits >= maxRateLimitWaits {
			return err
		}
		if e.Verbose {
			fmt.Printf("Rate limited, waiting %s\n", wait.Round(time.Second))
		}
		if err := e.sleep(ctx, wait); err != nil {
			return err
		}
	}
}

// rateLimitWait returns how long to wait when err is a rate limit
func rateLimitWait(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}

	var rateErr *gh.RateLimitError
	if errors.As(err, &rateErr) {
		return untilReset(rateErr.Rate.Reset.Time), true
	}

	var abuseErr *gh.AbuseRateLimitError
	if errors.As(err, &abuseErr) {
		if abuseErr.RetryAfter != nil && *abuseErr.RetryAfter > 0 {
			return *abuseErr.RetryAfter, true
		}
		return time.Minute, true
	}

	var gqlErr *github.GraphQLError
	if errors.As(err, &gqlErr) && gqlErr.HasType("RATE_LIMITED") {
		return untilReset(gqlErr.Rate.Reset.Time), true
	}

	// GitHub sometimes times out on big lists; a retry usually gets through
	switch statusOf(err) {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return serverErrorWait, true
	}

	return 0, false
}

// untilReset returns the wait until a rate limit resets, at least a second
func untilReset(reset time.Time) time.Duration {
	wait := time.Until(reset) + time.Second
	if wait < time.Second {
		return time.Second
	}
	return wait
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// listAll pages through a go-github list call until the last page
func listAll[T any](ctx context.Context, e *Exporter, list func(opts gh.ListOptions) ([]T, *gh.Response, error)) ([]T, error) {
	return listAllAfter(ctx, e, func(opts gh.ListOptions, _ string) ([]T, *gh.Response, error) {
		return list(opts)
	})
}

// listAllAfter is listAll for endpoints that also page with an "after" cursor.
// GitHub refuses page numbers past 100 there, so the cursor must be sent along.
func listAllAfter[T any](ctx context.Context, e *Exporter, list func(opts gh.ListOptions, after string) ([]T, *gh.Response, error)) ([]T, error) {
	all := make([]T, 0)
	opts := gh.ListOptions{Page: 1, PerPage: 100}
	after := ""
	for {
		var items []T
		var resp *gh.Response
		err := e.call(ctx, func() error {
			var err error
			items, resp, err = list(opts, after)
			return err
		})
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if resp == nil || (resp.NextPage == 0 && resp.After == "") {
			return all, nil
		}
		opts.Page = resp.NextPage
		after = resp.After
	}
}

// graphql runs one GraphQL query with rate limit waits. When the reply has errors
// but got returns true (the data we asked for is there), the errors become warnings.
func (e *Exporter) graphql(ctx context.Context, query string, vars map[string]any, out any, got func() bool) error {
	err := e.call(ctx, func() error {
		return github.GraphQL(ctx, e.gh, query, vars, out)
	})
	var gqlErr *github.GraphQLError
	if errors.As(err, &gqlErr) && got() {
		e.partial = append(e.partial, gqlErr.Error())
		return nil
	}
	return err
}

// numberFromURL returns the trailing number of an API URL like .../issues/12
func numberFromURL(url string) int {
	i := strings.LastIndex(url, "/")
	n := 0
	for _, c := range url[i+1:] {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
