package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mitchellh/go-homedir"
	"github.com/zalando/go-keyring"
	"gopkg.in/yaml.v3"
)

const (
	// KeyringServiceName is the service name used in the system keychain
	KeyringServiceName = "githubby"

	// ConfigFileName is the name of the config file
	ConfigFileName = ".githubby.yaml"
)

// Storage handles secure token storage
type Storage struct {
	keychainAvailable bool
	configPath        string // Custom config path (for testing)
}

// NewStorage creates a new Storage instance
func NewStorage() *Storage {
	s := &Storage{}
	s.keychainAvailable = s.checkKeychainAvailable()
	return s
}

// NewStorageWithConfig creates a Storage instance with a custom config path (for testing)
func NewStorageWithConfig(configPath string, useKeychain bool) *Storage {
	return &Storage{
		keychainAvailable: useKeychain,
		configPath:        configPath,
	}
}

// checkKeychainAvailable tests if the system keychain is available
func (s *Storage) checkKeychainAvailable() bool {
	// Try to access the keychain with a test operation
	// We use a unique test key that won't conflict with real data
	testKey := "githubby-keychain-test"

	// Try to get a non-existent key - if keychain is unavailable, this will error differently
	_, err := keyring.Get(KeyringServiceName, testKey)

	// ErrNotFound means keychain is available but key doesn't exist (expected)
	// Any other error might indicate keychain is unavailable
	if err == keyring.ErrNotFound {
		return true
	}

	// On some systems (e.g., headless Linux), keyring operations fail
	// We also check for specific error messages
	if err != nil {
		// If we got an error that's not "not found", try a set/delete cycle
		testErr := keyring.Set(KeyringServiceName, testKey, "test")
		if testErr != nil {
			return false
		}
		_ = keyring.Delete(KeyringServiceName, testKey)
		return true
	}

	return true
}

// GetToken retrieves the stored token for a hostname
func (s *Storage) GetToken(hostname string) (string, TokenSource, error) {
	if hostname == "" {
		hostname = DefaultHostname
	}

	// Try keychain first
	if s.keychainAvailable {
		token, err := keyring.Get(KeyringServiceName, hostname)
		if err == nil && token != "" {
			return token, TokenSourceKeychain, nil
		}
		// If not found in keychain, fall through to config file
	}

	// Fall back to config file
	token, err := s.getTokenFromConfig()
	if err == nil && token != "" {
		return token, TokenSourceConfig, nil
	}

	return "", TokenSourceNone, fmt.Errorf("no token found")
}

// SetToken stores a token for a hostname
func (s *Storage) SetToken(hostname, token string) error {
	if hostname == "" {
		hostname = DefaultHostname
	}

	// Try keychain first
	if s.keychainAvailable {
		err := keyring.Set(KeyringServiceName, hostname, token)
		if err == nil {
			return nil
		}
		// Fall through to config file if keychain fails
	}

	// Fall back to config file
	return s.setTokenInConfig(token)
}

// DeleteToken removes the stored token for a hostname
func (s *Storage) DeleteToken(hostname string) error {
	if hostname == "" {
		hostname = DefaultHostname
	}

	var keychainErr, configErr error

	// Try to delete from keychain
	if s.keychainAvailable {
		keychainErr = keyring.Delete(KeyringServiceName, hostname)
	}

	// Also try to clear from config file
	configErr = s.clearTokenFromConfig()

	// Return error only if both failed
	if keychainErr != nil && configErr != nil {
		return fmt.Errorf("failed to delete token from keychain (%v) and config (%v)", keychainErr, configErr)
	}

	return nil
}

// IsKeychainAvailable returns whether the system keychain is available
func (s *Storage) IsKeychainAvailable() bool {
	return s.keychainAvailable
}

// GetStorageLocation returns a description of where tokens are stored
func (s *Storage) GetStorageLocation() string {
	if s.keychainAvailable {
		switch runtime.GOOS {
		case "darwin":
			return "macOS Keychain"
		case "linux":
			return "Secret Service (GNOME Keyring/KWallet)"
		case "windows":
			return "Windows Credential Manager"
		default:
			return "System Keychain"
		}
	}
	return "config file (~/.githubby.yaml)"
}

// getConfigPath returns the path to the config file
func (s *Storage) getConfigPath() (string, error) {
	// Use custom path if set (for testing)
	if s.configPath != "" {
		return s.configPath, nil
	}

	home, err := homedir.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ConfigFileName), nil
}

// configFileData represents the structure of the config file
type configFileData struct {
	Token string `yaml:"token"`
	// Other fields are preserved but not used here
	Verbose        bool     `yaml:"verbose,omitempty"`
	DryRun         bool     `yaml:"dry-run,omitempty"`
	Repository     string   `yaml:"repository,omitempty"`
	FilterDays     int      `yaml:"filter-days,omitempty"`
	FilterCount    int      `yaml:"filter-count,omitempty"`
	User           string   `yaml:"user,omitempty"`
	Org            string   `yaml:"org,omitempty"`
	Target         string   `yaml:"target,omitempty"`
	IncludePrivate bool     `yaml:"include-private,omitempty"`
	Include        []string `yaml:"include,omitempty"`
	Exclude        []string `yaml:"exclude,omitempty"`
}

// getTokenFromConfig retrieves the token from the config file
func (s *Storage) getTokenFromConfig() (string, error) {
	path, err := s.getConfigPath()
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var cfg configFileData
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return "", err
	}

	return cfg.Token, nil
}

// setTokenInConfig stores the token in the config file
func (s *Storage) setTokenInConfig(token string) error {
	path, err := s.getConfigPath()
	if err != nil {
		return err
	}

	// Read existing config or create new
	var cfg configFileData
	if data, err := os.ReadFile(path); err == nil {
		_ = yaml.Unmarshal(data, &cfg)
	}

	cfg.Token = token

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}

	// Write with secure permissions (0600 = owner read/write only)
	return os.WriteFile(path, data, 0600)
}

// clearTokenFromConfig removes the token from the config file
func (s *Storage) clearTokenFromConfig() error {
	path, err := s.getConfigPath()
	if err != nil {
		return err
	}

	// Read existing config
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No config file, nothing to clear
		}
		return err
	}

	var cfg configFileData
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}

	cfg.Token = ""

	newData, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(path, newData, 0600)
}
