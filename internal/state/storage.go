package state

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

const (
	// StateFileName is the name of the state file
	StateFileName = "state.yaml"
	// ConfigDirName is the name of the config directory
	ConfigDirName = ".githubby"
)

// Storage handles state persistence
type Storage struct {
	mu       sync.RWMutex
	filePath string
	state    *State
}

// NewStorage creates a new storage instance with the default path
func NewStorage() (*Storage, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ConfigDirName)
	filePath := filepath.Join(configDir, StateFileName)

	return &Storage{
		filePath: filePath,
		state:    NewState(),
	}, nil
}

// NewStorageWithPath creates a new storage instance with a custom path
func NewStorageWithPath(filePath string) *Storage {
	return &Storage{
		filePath: filePath,
		state:    NewState(),
	}
}

// Load reads the state from disk
func (s *Storage) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// No state file yet, use default empty state
			s.state = NewState()
			return nil
		}
		return fmt.Errorf("failed to read state file: %w", err)
	}

	var state State
	if err := yaml.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("failed to parse state file: %w", err)
	}

	// Migrate if needed
	if state.Version < 1 {
		state.Version = 1
	}

	s.state = &state
	return nil
}

// Save writes the state to disk atomically
func (s *Storage) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.saveInternal()
}

// saveInternal performs the actual save (must be called with lock held)
func (s *Storage) saveInternal() error {
	// Ensure directory exists
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(s.state)
	if err != nil {
		return fmt.Errorf("failed to serialize state: %w", err)
	}

	// Write atomically using temp file
	tempFile := s.filePath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write temp state file: %w", err)
	}

	if err := os.Rename(tempFile, s.filePath); err != nil {
		// Clean up temp file on failure
		_ = os.Remove(tempFile)
		return fmt.Errorf("failed to rename temp state file: %w", err)
	}

	return nil
}

// State returns the current state (read-only copy)
func (s *Storage) State() *State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// AddProfile adds a profile and saves
func (s *Storage) AddProfile(profile *SyncProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.AddProfile(profile)
	return s.saveInternal()
}

// UpdateProfile updates a profile and saves
func (s *Storage) UpdateProfile(profile *SyncProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.state.UpdateProfile(profile) {
		return fmt.Errorf("profile not found: %s", profile.ID)
	}
	return s.saveInternal()
}

// DeleteProfile deletes a profile and saves
func (s *Storage) DeleteProfile(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.state.DeleteProfile(id) {
		return fmt.Errorf("profile not found: %s", id)
	}
	return s.saveInternal()
}

// GetProfile returns a profile by ID
func (s *Storage) GetProfile(id string) *SyncProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.GetProfile(id)
}

// GetProfiles returns all profiles
func (s *Storage) GetProfiles() []*SyncProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Return a copy to prevent external modification
	profiles := make([]*SyncProfile, len(s.state.Profiles))
	copy(profiles, s.state.Profiles)
	return profiles
}

// AddSyncRecord adds a sync record and saves
func (s *Storage) AddSyncRecord(record *SyncRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.AddSyncRecord(record)
	return s.saveInternal()
}

// GetLatestSyncForProfile returns the most recent sync for a profile
func (s *Storage) GetLatestSyncForProfile(profileID string) *SyncRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.GetLatestSyncForProfile(profileID)
}

// GetSyncStats returns aggregate sync statistics
func (s *Storage) GetSyncStats() SyncStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.GetSyncStats()
}

// UpdateProfileLastSync updates the last sync time for a profile
func (s *Storage) UpdateProfileLastSync(profileID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	profile := s.state.GetProfile(profileID)
	if profile == nil {
		return fmt.Errorf("profile not found: %s", profileID)
	}

	profile.LastSyncAt = s.state.GetLatestSyncForProfile(profileID).CompletedAt
	return s.saveInternal()
}

// UpdateRepoCache updates the repo cache and saves
func (s *Storage) UpdateRepoCache(repo *CachedRepo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.UpdateRepoCache(repo)
	return s.saveInternal()
}

// GetCachedRepo returns a cached repo by full name
func (s *Storage) GetCachedRepo(fullName string) *CachedRepo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.GetCachedRepo(fullName)
}

// GetSyncHistory returns all sync records
func (s *Storage) GetSyncHistory() []*SyncRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Return a copy
	history := make([]*SyncRecord, len(s.state.SyncHistory))
	copy(history, s.state.SyncHistory)
	return history
}

// GetRecentSyncHistory returns the most recent N sync records
func (s *Storage) GetRecentSyncHistory(n int) []*SyncRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	history := s.state.SyncHistory
	if len(history) <= n {
		result := make([]*SyncRecord, len(history))
		copy(result, history)
		return result
	}

	result := make([]*SyncRecord, n)
	copy(result, history[len(history)-n:])
	return result
}

// IsOnboardingComplete returns whether onboarding has been completed
func (s *Storage) IsOnboardingComplete() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.OnboardingComplete
}

// SetOnboardingComplete marks onboarding as complete and saves
func (s *Storage) SetOnboardingComplete(complete bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.OnboardingComplete = complete
	return s.saveInternal()
}

// GetDefaultTargetDir returns the default target directory for syncing
func (s *Storage) GetDefaultTargetDir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.DefaultTargetDir
}

// GetDefaultUsername returns the default GitHub username
func (s *Storage) GetDefaultUsername() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.DefaultUsername
}

// SetDefaults sets the default target directory and username
func (s *Storage) SetDefaults(targetDir, username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.DefaultTargetDir = targetDir
	s.state.DefaultUsername = username
	return s.saveInternal()
}
