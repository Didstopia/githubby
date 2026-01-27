package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Verbose != false {
		t.Error("default verbose should be false")
	}
	if cfg.DryRun != false {
		t.Error("default dry-run should be false")
	}
	if cfg.Token != "" {
		t.Error("default token should be empty")
	}
	if cfg.FilterDays != -1 {
		t.Errorf("default filter-days should be -1, got %d", cfg.FilterDays)
	}
	if cfg.FilterCount != -1 {
		t.Errorf("default filter-count should be -1, got %d", cfg.FilterCount)
	}
	if cfg.IncludePrivate != false {
		t.Error("default include-private should be false")
	}
}

func TestConfig_Clone(t *testing.T) {
	original := &Config{
		Verbose:        true,
		DryRun:         true,
		Token:          "test-token",
		Repository:     "owner/repo",
		FilterDays:     30,
		FilterCount:    10,
		IncludePrivate: true,
		Include:        []string{"pattern1", "pattern2"},
		Exclude:        []string{"exclude1"},
	}

	clone := original.Clone()

	// Verify values are copied
	if clone.Verbose != original.Verbose {
		t.Error("verbose not cloned")
	}
	if clone.Token != original.Token {
		t.Error("token not cloned")
	}
	if clone.FilterDays != original.FilterDays {
		t.Error("filter-days not cloned")
	}
	if len(clone.Include) != len(original.Include) {
		t.Error("include not cloned")
	}
	if len(clone.Exclude) != len(original.Exclude) {
		t.Error("exclude not cloned")
	}

	// Verify slices are deep copied
	clone.Include[0] = "modified"
	if original.Include[0] == "modified" {
		t.Error("include slice should be deep copied")
	}
}

func TestConfig_SaveToAndLoadFrom(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	// Create config to save
	original := &Config{
		Verbose:        true,
		DryRun:         false,
		Token:          "test-token-123",
		Repository:     "owner/repo",
		FilterDays:     30,
		FilterCount:    5,
		User:           "testuser",
		Target:         "/tmp/repos",
		IncludePrivate: true,
		Include:        []string{"pattern*"},
		Exclude:        []string{"*-archive"},
	}

	// Save config
	if err := original.SaveTo(configPath); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Verify file permissions (should be 0600)
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("failed to stat config file: %v", err)
	}
	// Note: On Windows, file permissions work differently
	if perm := info.Mode().Perm(); perm&0077 != 0 {
		// Only check on Unix-like systems
		if os.Getenv("OS") != "Windows_NT" {
			t.Errorf("config file permissions should be 0600, got %o", perm)
		}
	}

	// Load config
	loaded, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Verify loaded values
	if loaded.Verbose != original.Verbose {
		t.Errorf("verbose mismatch: got %v, want %v", loaded.Verbose, original.Verbose)
	}
	if loaded.Token != original.Token {
		t.Errorf("token mismatch: got %v, want %v", loaded.Token, original.Token)
	}
	if loaded.FilterDays != original.FilterDays {
		t.Errorf("filter-days mismatch: got %v, want %v", loaded.FilterDays, original.FilterDays)
	}
	if loaded.FilterCount != original.FilterCount {
		t.Errorf("filter-count mismatch: got %v, want %v", loaded.FilterCount, original.FilterCount)
	}
	if loaded.User != original.User {
		t.Errorf("user mismatch: got %v, want %v", loaded.User, original.User)
	}
	if loaded.IncludePrivate != original.IncludePrivate {
		t.Errorf("include-private mismatch: got %v, want %v", loaded.IncludePrivate, original.IncludePrivate)
	}
	if len(loaded.Include) != len(original.Include) {
		t.Errorf("include length mismatch: got %d, want %d", len(loaded.Include), len(original.Include))
	}
}

func TestLoadFrom_NonExistent(t *testing.T) {
	_, err := LoadFrom("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestLoadFrom_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")

	// Write invalid YAML
	if err := os.WriteFile(configPath, []byte("{ invalid yaml"), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := LoadFrom(configPath)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}
