// Package state provides persistence for sync profiles and history
package state

import (
	"time"

	"github.com/google/uuid"
)

// State represents the persisted application state
type State struct {
	Version            int            `yaml:"version"`
	OnboardingComplete bool           `yaml:"onboarding_complete,omitempty"`
	DefaultTargetDir   string         `yaml:"default_target_dir,omitempty"`
	DefaultUsername    string         `yaml:"default_username,omitempty"`
	Profiles           []*SyncProfile `yaml:"profiles"`
	SyncHistory        []*SyncRecord  `yaml:"sync_history"`
	RepoCache          []*CachedRepo  `yaml:"repo_cache,omitempty"`
}

// SyncProfile represents a saved sync configuration
type SyncProfile struct {
	ID             string    `yaml:"id"`
	Name           string    `yaml:"name"`
	Type           string    `yaml:"type"`    // "user" or "org"
	Source         string    `yaml:"source"`  // username or org name
	TargetDir      string    `yaml:"target_dir"`
	IncludePrivate bool      `yaml:"include_private"`
	SyncAllRepos   bool      `yaml:"sync_all_repos,omitempty"`  // true = fetch all from API, false = use SelectedRepos
	SelectedRepos  []string  `yaml:"selected_repos,omitempty"` // specific repos (only used when SyncAllRepos is false)
	IncludeFilter  []string  `yaml:"include_filter,omitempty"` // glob patterns
	ExcludeFilter  []string  `yaml:"exclude_filter,omitempty"` // glob patterns
	CreatedAt      time.Time `yaml:"created_at"`
	LastSyncAt     time.Time `yaml:"last_sync_at,omitempty"`
}

// SyncRecord represents a completed sync operation
type SyncRecord struct {
	ProfileID   string            `yaml:"profile_id"`
	ProfileName string            `yaml:"profile_name"`
	StartedAt   time.Time         `yaml:"started_at"`
	CompletedAt time.Time         `yaml:"completed_at"`
	TotalRepos  int               `yaml:"total_repos"`
	Cloned      int               `yaml:"cloned"`
	Updated     int               `yaml:"updated"`
	Skipped     int               `yaml:"skipped"`
	Failed      int               `yaml:"failed"`
	Archived    int               `yaml:"archived"` // repos that exist locally but not on remote (preserved)
	Results     []*RepoSyncResult `yaml:"results,omitempty"`
}

// RepoSyncResult represents the result of syncing a single repository
type RepoSyncResult struct {
	FullName string    `yaml:"full_name"`
	Status   string    `yaml:"status"` // "cloned", "updated", "skipped", "failed"
	SyncedAt time.Time `yaml:"synced_at"`
	Error    string    `yaml:"error,omitempty"`
}

// CachedRepo represents cached repository metadata
type CachedRepo struct {
	FullName   string    `yaml:"full_name"`
	Private    bool      `yaml:"private"`
	Language   string    `yaml:"language,omitempty"`
	Stars      int       `yaml:"stars"`
	LastSynced time.Time `yaml:"last_synced,omitempty"`
}

// NewState creates a new empty state
func NewState() *State {
	return &State{
		Version:     1,
		Profiles:    make([]*SyncProfile, 0),
		SyncHistory: make([]*SyncRecord, 0),
		RepoCache:   make([]*CachedRepo, 0),
	}
}

// NewProfile creates a new sync profile with a generated ID
func NewProfile(name, profileType, source, targetDir string, includePrivate bool) *SyncProfile {
	return &SyncProfile{
		ID:             uuid.New().String(),
		Name:           name,
		Type:           profileType,
		Source:         source,
		TargetDir:      targetDir,
		IncludePrivate: includePrivate,
		CreatedAt:      time.Now(),
	}
}

// NewSyncRecord creates a new sync record
func NewSyncRecord(profileID, profileName string) *SyncRecord {
	return &SyncRecord{
		ProfileID:   profileID,
		ProfileName: profileName,
		StartedAt:   time.Now(),
		Results:     make([]*RepoSyncResult, 0),
	}
}

// AddProfile adds a sync profile to the state
func (s *State) AddProfile(profile *SyncProfile) {
	s.Profiles = append(s.Profiles, profile)
}

// GetProfile returns a profile by ID
func (s *State) GetProfile(id string) *SyncProfile {
	for _, p := range s.Profiles {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// GetProfileByName returns a profile by name
func (s *State) GetProfileByName(name string) *SyncProfile {
	for _, p := range s.Profiles {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// UpdateProfile updates an existing profile
func (s *State) UpdateProfile(profile *SyncProfile) bool {
	for i, p := range s.Profiles {
		if p.ID == profile.ID {
			s.Profiles[i] = profile
			return true
		}
	}
	return false
}

// DeleteProfile removes a profile by ID and its associated sync history
func (s *State) DeleteProfile(id string) bool {
	found := false
	for i, p := range s.Profiles {
		if p.ID == id {
			s.Profiles = append(s.Profiles[:i], s.Profiles[i+1:]...)
			found = true
			break
		}
	}

	if found {
		// Also remove sync history records for this profile
		filtered := make([]*SyncRecord, 0, len(s.SyncHistory))
		for _, r := range s.SyncHistory {
			if r.ProfileID != id {
				filtered = append(filtered, r)
			}
		}
		s.SyncHistory = filtered
	}

	return found
}

// AddSyncRecord adds a sync record to history
func (s *State) AddSyncRecord(record *SyncRecord) {
	s.SyncHistory = append(s.SyncHistory, record)
	// Keep only last 100 records
	if len(s.SyncHistory) > 100 {
		s.SyncHistory = s.SyncHistory[len(s.SyncHistory)-100:]
	}
}

// GetLatestSyncForProfile returns the most recent sync record for a profile
func (s *State) GetLatestSyncForProfile(profileID string) *SyncRecord {
	var latest *SyncRecord
	for _, r := range s.SyncHistory {
		if r.ProfileID == profileID {
			if latest == nil || r.CompletedAt.After(latest.CompletedAt) {
				latest = r
			}
		}
	}
	return latest
}

// GetSyncStats returns aggregate statistics from all sync history
func (s *State) GetSyncStats() SyncStats {
	stats := SyncStats{}
	for _, r := range s.SyncHistory {
		stats.TotalSyncs++
		stats.TotalCloned += r.Cloned
		stats.TotalUpdated += r.Updated
		stats.TotalSkipped += r.Skipped
		stats.TotalFailed += r.Failed
		stats.TotalArchived += r.Archived
	}

	// Find last sync time
	for _, r := range s.SyncHistory {
		if stats.LastSync.IsZero() || r.CompletedAt.After(stats.LastSync) {
			stats.LastSync = r.CompletedAt
		}
	}

	return stats
}

// SyncStats provides aggregate sync statistics
type SyncStats struct {
	TotalSyncs    int
	TotalCloned   int
	TotalUpdated  int
	TotalSkipped  int
	TotalFailed   int
	TotalArchived int
	LastSync      time.Time
}

// TotalReposSynced returns the total number of repos successfully synced
func (ss SyncStats) TotalReposSynced() int {
	return ss.TotalCloned + ss.TotalUpdated
}

// UpdateRepoCache updates or adds a repo to the cache
func (s *State) UpdateRepoCache(repo *CachedRepo) {
	for i, r := range s.RepoCache {
		if r.FullName == repo.FullName {
			s.RepoCache[i] = repo
			return
		}
	}
	s.RepoCache = append(s.RepoCache, repo)
}

// GetCachedRepo returns a cached repo by full name
func (s *State) GetCachedRepo(fullName string) *CachedRepo {
	for _, r := range s.RepoCache {
		if r.FullName == fullName {
			return r
		}
	}
	return nil
}

// Complete marks a sync record as completed and updates statistics
func (r *SyncRecord) Complete() {
	r.CompletedAt = time.Now()
	r.TotalRepos = len(r.Results)
	for _, result := range r.Results {
		switch result.Status {
		case "cloned":
			r.Cloned++
		case "updated":
			r.Updated++
		case "skipped":
			r.Skipped++
		case "failed":
			r.Failed++
		case "archived":
			r.Archived++
		}
	}
}

// AddResult adds a repository sync result to the record
func (r *SyncRecord) AddResult(fullName, status, errorMsg string) {
	r.Results = append(r.Results, &RepoSyncResult{
		FullName: fullName,
		Status:   status,
		SyncedAt: time.Now(),
		Error:    errorMsg,
	})
}
