package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpdateDisabled(t *testing.T) {
	// Point the Docker marker at a path that doesn't exist
	orig := dockerEnvPath
	dockerEnvPath = filepath.Join(t.TempDir(), "missing")
	defer func() { dockerEnvPath = orig }()

	t.Run("enabled by default", func(t *testing.T) {
		t.Setenv(noUpdateEnv, "")
		assert.False(t, updateDisabled([]string{"sync"}))
	})

	t.Run("env var turns it off", func(t *testing.T) {
		t.Setenv(noUpdateEnv, "1")
		assert.True(t, updateDisabled([]string{"sync"}))
	})

	t.Run("env var set to 0 keeps it on", func(t *testing.T) {
		t.Setenv(noUpdateEnv, "0")
		assert.False(t, updateDisabled([]string{"sync"}))
	})

	t.Run("flag turns it off", func(t *testing.T) {
		t.Setenv(noUpdateEnv, "")
		assert.True(t, updateDisabled([]string{"sync", "--no-update"}))
	})

	t.Run("inside Docker", func(t *testing.T) {
		t.Setenv(noUpdateEnv, "")
		marker := filepath.Join(t.TempDir(), ".dockerenv")
		assert.NoError(t, os.WriteFile(marker, nil, 0600))
		dockerEnvPath = marker
		assert.True(t, updateDisabled([]string{"sync"}))
	})
}
