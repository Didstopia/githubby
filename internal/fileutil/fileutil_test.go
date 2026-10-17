package fileutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "x.json")
	require.NoError(t, WriteJSON(path, map[string]int{"n": 1}))

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "{\n  \"n\": 1\n}\n", string(body))

	// Overwrite leaves no temp files behind
	require.NoError(t, WriteJSON(path, []int{2}))
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestWriteJSON_Errors(t *testing.T) {
	assert.Error(t, WriteJSON(filepath.Join(t.TempDir(), "x.json"), make(chan int)))

	// Parent is a file, so the directory can't be made
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0644))
	assert.Error(t, WriteFileAtomic(filepath.Join(file, "x"), []byte("x"), 0644))

	// Target is a directory, so the rename fails
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "taken"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "taken", "keep"), nil, 0644))
	assert.Error(t, WriteFileAtomic(filepath.Join(dir, "taken"), []byte("x"), 0644))
}
