package cmd

import (
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const (
	configFileName = ".githubby"
	configFileType = "yaml"
)

type yamlConfig struct {
	Verbose     bool   `yaml:"verbose"`
	DryRun      bool   `yaml:"dry-run"`
	Token       string `yaml:"token"`
	Repository  string `yaml:"repository"`
	FilterDays  int    `yaml:"filter-days"`
	FilterCount int    `yaml:"filter-count"`
}

// The primary viper object
var viperConfig *viper.Viper = viper.New()

func initConfig() {
	// Make sure the default config file exists
	if err := writeDefaultConfig(); err != nil {
		logErrorAndExit(err)
	}

	// Search the home directory for a config file
	homeDir, err := getHomePath()
	logErrorAndExit(err)
	viperConfig.AddConfigPath(homeDir)

	// Also search the current directory
	viperConfig.AddConfigPath(".")

	// Set the config file name (without extension) and type (effectively the extension)
	viperConfig.SetConfigName(configFileName)
	viperConfig.SetConfigType(configFileType)

	// Attempt to load the configuration file
	if err := viperConfig.ReadInConfig(); err != nil {
		logErrorAndExit(err)
	}

	// Enable environment variable support
	viperConfig.AutomaticEnv()

	// Set default log level
	if viperConfig.GetBool("verbose") {
		log.SetLevel(logrus.DebugLevel)
	} else {
		log.SetLevel(logrus.InfoLevel)
	}
}

func writeDefaultConfig() error {
	// Get the config file path
	path, err := getConfigFilePath()
	if err != nil {
		return err
	}

	// Check if the config file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Create default config
		config := yamlConfig{
			Verbose:     false,
			DryRun:      false,
			Token:       "",
			Repository:  "",
			FilterDays:  -1,
			FilterCount: -1,
		}
		data, err := yaml.Marshal(&config)
		if err != nil {
			return err
		}
		// Write config file with secure permissions (0600 = owner read/write only)
		if err := os.WriteFile(path, data, 0600); err != nil {
			return err
		}
	}
	return nil
}

func getHomePath() (string, error) {
	// Find and return the home directory
	home, err := homedir.Dir()
	if err != nil {
		return "", err
	}
	return home, nil
}

func getConfigFilePath() (string, error) {
	// Construct and return the path to the config file
	home, err := getHomePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, configFileName+"."+configFileType), nil
}

// Read explicitly set values from viper and override Flags
// values with the same long-name if they were not explicitly set via cmd line
func injectViper(cmdViper *viper.Viper, cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if !f.Changed {
			if cmdViper.IsSet(f.Name) {
				//log.Debug("Injecting ", f.Name, " -> ", cmdViper.GetString(f.Name))
				cmd.Flags().Set(f.Name, cmdViper.GetString(f.Name))
			}
		}
	})
}
