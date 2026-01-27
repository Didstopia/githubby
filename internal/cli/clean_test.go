package cli

import (
	"testing"
	"time"

	gh "github.com/google/go-github/v68/github"
	"github.com/stretchr/testify/assert"
)

func TestFilterReleases_ByCount(t *testing.T) {
	now := time.Now()
	releases := createTestReleases(5, now)

	tests := []struct {
		name         string
		releases     []*gh.RepositoryRelease
		filterCount  int64
		expectedLen  int
	}{
		{
			name:         "keep 3 most recent, remove 2",
			releases:     releases,
			filterCount:  3,
			expectedLen:  2,
		},
		{
			name:         "keep all releases",
			releases:     releases,
			filterCount:  10,
			expectedLen:  0,
		},
		{
			name:         "keep 0 releases",
			releases:     releases,
			filterCount:  0,
			expectedLen:  5,
		},
		{
			name:         "keep 1 release",
			releases:     releases,
			filterCount:  1,
			expectedLen:  4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset globals
			oldFilterCount := filterCount
			oldFilterDays := filterDays
			oldVerbose := verbose
			defer func() {
				filterCount = oldFilterCount
				filterDays = oldFilterDays
				verbose = oldVerbose
			}()

			filterCount = tt.filterCount
			filterDays = -1
			verbose = false

			result := filterReleases(tt.releases, false, true)
			assert.Len(t, result, tt.expectedLen)
		})
	}
}

func TestFilterReleases_ByDays(t *testing.T) {
	now := time.Now()

	// Create releases with different ages
	// Note: filterReleases uses math.Ceil on hours/24 and checks daysSinceRelease > filterDays
	// Using clear day boundaries (add 1h buffer to avoid edge cases from test execution timing)
	releases := []*gh.RepositoryRelease{
		createReleaseAt(1, now),                                        // ~0 days old
		createReleaseAt(2, now.Add(-5*24*time.Hour-time.Hour)),         // 5+ days old
		createReleaseAt(3, now.Add(-10*24*time.Hour-time.Hour)),        // 10+ days old
		createReleaseAt(4, now.Add(-30*24*time.Hour)),                  // 30 days old
		createReleaseAt(5, now.Add(-60*24*time.Hour)),                  // 60 days old
	}

	tests := []struct {
		name        string
		releases    []*gh.RepositoryRelease
		filterDays  int64
		expectedLen int
	}{
		{
			name:        "filter releases older than 7 days",
			releases:    releases,
			filterDays:  7,
			expectedLen: 3, // 10+, 30, 60 days old releases
		},
		{
			name:        "filter releases older than 50 days",
			releases:    releases,
			filterDays:  50,
			expectedLen: 1, // 60 days old release only
		},
		{
			name:        "filter releases older than 100 days (none)",
			releases:    releases,
			filterDays:  100,
			expectedLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset globals
			oldFilterCount := filterCount
			oldFilterDays := filterDays
			oldVerbose := verbose
			defer func() {
				filterCount = oldFilterCount
				filterDays = oldFilterDays
				verbose = oldVerbose
			}()

			filterCount = -1
			filterDays = tt.filterDays
			verbose = false

			result := filterReleases(tt.releases, true, false)
			assert.Len(t, result, tt.expectedLen)
		})
	}
}

func TestFilterReleases_Combined(t *testing.T) {
	now := time.Now()

	// Create 10 releases, with varying ages
	releases := []*gh.RepositoryRelease{
		createReleaseAt(1, now),                        // 0 days old
		createReleaseAt(2, now.Add(-1*24*time.Hour)),   // 1 day old
		createReleaseAt(3, now.Add(-2*24*time.Hour)),   // 2 days old
		createReleaseAt(4, now.Add(-3*24*time.Hour)),   // 3 days old
		createReleaseAt(5, now.Add(-4*24*time.Hour)),   // 4 days old
		createReleaseAt(6, now.Add(-10*24*time.Hour)),  // 10 days old
		createReleaseAt(7, now.Add(-20*24*time.Hour)),  // 20 days old
		createReleaseAt(8, now.Add(-30*24*time.Hour)),  // 30 days old
	}

	tests := []struct {
		name        string
		filterCount int64
		filterDays  int64
		expectedMin int
	}{
		{
			name:        "count filter takes precedence when more releases exceed count",
			filterCount: 3,
			filterDays:  100, // All releases are within 100 days
			expectedMin: 5,   // At least 5 releases exceed count of 3
		},
		{
			name:        "days filter catches older releases",
			filterCount: 100, // All releases are within count
			filterDays:  5,   // Releases older than 5 days
			expectedMin: 3,   // At least 3 releases are older than 5 days
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset globals
			oldFilterCount := filterCount
			oldFilterDays := filterDays
			oldVerbose := verbose
			defer func() {
				filterCount = oldFilterCount
				filterDays = oldFilterDays
				verbose = oldVerbose
			}()

			filterCount = tt.filterCount
			filterDays = tt.filterDays
			verbose = false

			result := filterReleases(releases, true, true)
			assert.GreaterOrEqual(t, len(result), tt.expectedMin)
		})
	}
}

func TestFilterReleases_NoFilters(t *testing.T) {
	now := time.Now()
	releases := createTestReleases(5, now)

	oldVerbose := verbose
	defer func() { verbose = oldVerbose }()
	verbose = false

	result := filterReleases(releases, false, false)
	assert.Empty(t, result)
}

func TestFilterReleases_EmptyInput(t *testing.T) {
	oldFilterCount := filterCount
	oldFilterDays := filterDays
	oldVerbose := verbose
	defer func() {
		filterCount = oldFilterCount
		filterDays = oldFilterDays
		verbose = oldVerbose
	}()

	filterCount = 3
	filterDays = 7
	verbose = false

	result := filterReleases([]*gh.RepositoryRelease{}, true, true)
	assert.Empty(t, result)
}

// Helper functions

func createTestReleases(count int, baseTime time.Time) []*gh.RepositoryRelease {
	releases := make([]*gh.RepositoryRelease, count)
	for i := 0; i < count; i++ {
		releases[i] = createReleaseAt(int64(i+1), baseTime.Add(-time.Duration(i)*24*time.Hour))
	}
	return releases
}

func createReleaseAt(id int64, createdAt time.Time) *gh.RepositoryRelease {
	timestamp := gh.Timestamp{Time: createdAt}
	return &gh.RepositoryRelease{
		ID:        &id,
		CreatedAt: &timestamp,
	}
}
