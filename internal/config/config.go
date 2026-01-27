// Package config provides configuration management for GitHubby
package config

import (
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
	"gopkg.in/yaml.v3"
)

const (
	// DefaultConfigFileName is the name of the config file (without extension)
	DefaultConfigFileName = ".githubby"
	// DefaultConfigFileType is the config file extension
	DefaultConfigFileType = "yaml"
)

// Config holds all application configuration
type Config struct {
	// Global flags
	Verbose bool   `yaml:"verbose"`
	DryRun  bool   `yaml:"dry-run"`
	Token   string `yaml:"token"`

	// Clean command
	Repository  string `yaml:"repository"`
	FilterDays  int    `yaml:"filter-days"`
	FilterCount int    `yaml:"filter-count"`

	// Sync command
	User           string   `yaml:"user"`
	Org            string   `yaml:"org"`
	Target         string   `yaml:"target"`
	IncludePrivate bool     `yaml:"include-private"`
	Include        []string `yaml:"include"`
	Exclude        []string `yaml:"exclude"`
}

// DefaultConfig returns a new Config with default values
func DefaultConfig() *Config {
	return &Config{
		Verbose:        false,
		DryRun:         false,
		Token:          "",
		Repository:     "",
		FilterDays:     -1,
		FilterCount:    -1,
		User:           "",
		Org:            "",
		Target:         "",
		IncludePrivate: false,
		Include:        nil,
		Exclude:        nil,
	}
}

// GetConfigFilePath returns the path to the config file
func GetConfigFilePath() (string, error) {
	home, err := homedir.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, DefaultConfigFileName+"."+DefaultConfigFileType), nil
}

// EnsureConfigFile creates the config file with defaults if it doesn't exist
func EnsureConfigFile() error {
	path, err := GetConfigFilePath()
	if err != nil {
		return err
	}

	// Check if config file already exists
	if _, err := os.Stat(path); err == nil {
		return nil // File exists
	} else if !os.IsNotExist(err) {
		return err // Some other error
	}

	// Create default config
	cfg := DefaultConfig()
	return cfg.SaveTo(path)
}

// LoadFrom loads configuration from a file
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// SaveTo saves configuration to a file with secure permissions
func (c *Config) SaveTo(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	// Write with secure permissions (0600 = owner read/write only)
	return os.WriteFile(path, data, 0600)
}

// Load loads configuration from the default config file
func Load() (*Config, error) {
	path, err := GetConfigFilePath()
	if err != nil {
		return nil, err
	}

	return LoadFrom(path)
}

// Clone returns a deep copy of the config
func (c *Config) Clone() *Config {
	clone := *c
	if c.Include != nil {
		clone.Include = make([]string, len(c.Include))
		copy(clone.Include, c.Include)
	}
	if c.Exclude != nil {
		clone.Exclude = make([]string, len(c.Exclude))
		copy(clone.Exclude, c.Exclude)
	}
	return &clone
}
