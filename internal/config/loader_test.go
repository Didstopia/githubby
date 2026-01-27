package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// Disable homedir caching to allow tests to change HOME
	homedir.DisableCache = true
}

func TestNewLoader(t *testing.T) {
	loader := NewLoader()

	assert.NotNil(t, loader)
	assert.NotNil(t, loader.viper)
}

func TestLoader_SetDefault(t *testing.T) {
	loader := NewLoader()

	loader.SetDefault("test-key", "default-value")
	result := loader.GetString("test-key")

	assert.Equal(t, "default-value", result)
}

func TestLoader_GetString(t *testing.T) {
	loader := NewLoader()

	loader.SetDefault("string-key", "string-value")

	result := loader.GetString("string-key")
	assert.Equal(t, "string-value", result)
}

func TestLoader_GetBool(t *testing.T) {
	loader := NewLoader()

	t.Run("returns true", func(t *testing.T) {
		loader.SetDefault("bool-true", true)
		result := loader.GetBool("bool-true")
		assert.True(t, result)
	})

	t.Run("returns false", func(t *testing.T) {
		loader.SetDefault("bool-false", false)
		result := loader.GetBool("bool-false")
		assert.False(t, result)
	})
}

func TestLoader_GetInt(t *testing.T) {
	loader := NewLoader()

	loader.SetDefault("int-key", 42)

	result := loader.GetInt("int-key")
	assert.Equal(t, 42, result)
}

func TestLoader_IsSet(t *testing.T) {
	loader := NewLoader()

	t.Run("returns true for set key", func(t *testing.T) {
		loader.SetDefault("existing-key", "value")
		assert.True(t, loader.IsSet("existing-key"))
	})

	t.Run("returns false for unset key", func(t *testing.T) {
		assert.False(t, loader.IsSet("nonexistent-key"))
	})
}

func TestLoader_BindFlag(t *testing.T) {
	loader := NewLoader()

	cmd := &cobra.Command{}
	cmd.Flags().String("test-flag", "default", "test flag")

	flag := cmd.Flags().Lookup("test-flag")
	require.NotNil(t, flag)

	err := loader.BindFlag("test-flag", flag)
	assert.NoError(t, err)
}

func TestLoader_InjectToCommand(t *testing.T) {
	t.Run("injects config value to unchanged flag", func(t *testing.T) {
		loader := NewLoader()
		loader.SetDefault("inject-flag", "config-value")

		cmd := &cobra.Command{}
		cmd.Flags().String("inject-flag", "default", "test flag")

		// Flag not changed (default value)
		loader.InjectToCommand(cmd)

		result, _ := cmd.Flags().GetString("inject-flag")
		assert.Equal(t, "config-value", result)
	})

	t.Run("does not override changed flag", func(t *testing.T) {
		loader := NewLoader()
		loader.SetDefault("override-flag", "config-value")

		cmd := &cobra.Command{}
		cmd.Flags().String("override-flag", "default", "test flag")

		// Simulate flag being changed via CLI
		cmd.Flags().Set("override-flag", "cli-value")

		loader.InjectToCommand(cmd)

		result, _ := cmd.Flags().GetString("override-flag")
		assert.Equal(t, "cli-value", result) // CLI value preserved
	})

	t.Run("handles multiple flags", func(t *testing.T) {
		loader := NewLoader()
		loader.SetDefault("flag1", "value1")
		loader.SetDefault("flag2", "value2")

		cmd := &cobra.Command{}
		cmd.Flags().String("flag1", "default1", "test flag 1")
		cmd.Flags().String("flag2", "default2", "test flag 2")

		loader.InjectToCommand(cmd)

		result1, _ := cmd.Flags().GetString("flag1")
		result2, _ := cmd.Flags().GetString("flag2")

		assert.Equal(t, "value1", result1)
		assert.Equal(t, "value2", result2)
	})
}

func TestLoader_Viper(t *testing.T) {
	loader := NewLoader()

	viper := loader.Viper()
	assert.NotNil(t, viper)
	assert.Equal(t, loader.viper, viper)
}

func TestLoader_Initialize(t *testing.T) {
	t.Run("loads config from temp directory", func(t *testing.T) {
		// Save and restore the home directory for this subtest
		originalHome := os.Getenv("HOME")
		tmpDir := t.TempDir()
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", originalHome)

		// Create config file
		configPath := filepath.Join(tmpDir, DefaultConfigFileName+"."+DefaultConfigFileType)
		content := `verbose: true
dry-run: false
token: test-token
`
		require.NoError(t, os.WriteFile(configPath, []byte(content), 0600))

		loader := NewLoader()
		err := loader.Initialize()
		require.NoError(t, err)

		assert.True(t, loader.GetBool("verbose"))
		assert.False(t, loader.GetBool("dry-run"))
		assert.Equal(t, "test-token", loader.GetString("token"))
	})

	t.Run("creates default config when missing", func(t *testing.T) {
		// Save and restore the home directory for this subtest
		originalHome := os.Getenv("HOME")
		tmpDir := t.TempDir()
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", originalHome)

		configPath := filepath.Join(tmpDir, DefaultConfigFileName+"."+DefaultConfigFileType)

		// Verify config doesn't exist
		_, err := os.Stat(configPath)
		require.True(t, os.IsNotExist(err))

		loader := NewLoader()
		err = loader.Initialize()
		require.NoError(t, err)

		// Verify config was created
		_, err = os.Stat(configPath)
		assert.NoError(t, err)
	})
}

func TestLoader_Initialize_InvalidConfig(t *testing.T) {
	t.Run("returns error for invalid YAML", func(t *testing.T) {
		originalHome := os.Getenv("HOME")
		tmpDir := t.TempDir()
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", originalHome)

		// Create invalid config file
		configPath := filepath.Join(tmpDir, DefaultConfigFileName+"."+DefaultConfigFileType)
		content := `{ invalid yaml
`
		require.NoError(t, os.WriteFile(configPath, []byte(content), 0600))

		loader := NewLoader()
		err := loader.Initialize()
		assert.Error(t, err)
	})
}
