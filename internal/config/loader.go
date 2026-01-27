package config

import (
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Loader manages configuration loading from multiple sources
type Loader struct {
	viper *viper.Viper
}

// NewLoader creates a new configuration loader
func NewLoader() *Loader {
	return &Loader{
		viper: viper.New(),
	}
}

// Initialize sets up the configuration loader
func (l *Loader) Initialize() error {
	// Ensure default config file exists
	if err := EnsureConfigFile(); err != nil {
		return err
	}

	// Search home directory for config file
	home, err := homedir.Dir()
	if err != nil {
		return err
	}
	l.viper.AddConfigPath(home)

	// Also search current directory
	l.viper.AddConfigPath(".")

	// Set config file name and type
	l.viper.SetConfigName(DefaultConfigFileName)
	l.viper.SetConfigType(DefaultConfigFileType)

	// Load configuration file
	if err := l.viper.ReadInConfig(); err != nil {
		return err
	}

	// Enable environment variable support
	l.viper.AutomaticEnv()

	return nil
}

// BindFlag binds a flag to a viper key
func (l *Loader) BindFlag(key string, flag *pflag.Flag) error {
	return l.viper.BindPFlag(key, flag)
}

// SetDefault sets a default value for a key
func (l *Loader) SetDefault(key string, value interface{}) {
	l.viper.SetDefault(key, value)
}

// GetString returns a string value
func (l *Loader) GetString(key string) string {
	return l.viper.GetString(key)
}

// GetBool returns a bool value
func (l *Loader) GetBool(key string) bool {
	return l.viper.GetBool(key)
}

// GetInt returns an int value
func (l *Loader) GetInt(key string) int {
	return l.viper.GetInt(key)
}

// IsSet checks if a key has been set
func (l *Loader) IsSet(key string) bool {
	return l.viper.IsSet(key)
}

// InjectToCommand injects viper config values into command flags
// that weren't explicitly set via command line
func (l *Loader) InjectToCommand(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if !f.Changed && l.viper.IsSet(f.Name) {
			cmd.Flags().Set(f.Name, l.viper.GetString(f.Name))
		}
	})
}

// Viper returns the underlying viper instance
func (l *Loader) Viper() *viper.Viper {
	return l.viper
}
