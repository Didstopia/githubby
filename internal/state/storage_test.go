package state

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStorage_UpdateProfileLastSync_NoSyncRecord(t *testing.T) {
	storage := NewStorageWithPath(filepath.Join(t.TempDir(), StateFileName))
	profile := NewProfile("p", "user", "someone", t.TempDir(), false)
	require.NoError(t, storage.AddProfile(profile))

	// The CLI never writes sync records, so this must not need one
	require.NoError(t, storage.UpdateProfileLastSync(profile.ID))
	assert.False(t, storage.GetProfile(profile.ID).LastSyncAt.IsZero())
}
