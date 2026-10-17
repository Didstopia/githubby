package util

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsInteractive_FalseWithoutTerminal(t *testing.T) {
	// Under `go test` stdin is not a terminal, so this must be false.
	assert.False(t, IsInteractive())
}

func TestIsInteractive_FalseWithNullStdin(t *testing.T) {
	devnull, err := os.Open(os.DevNull)
	require.NoError(t, err)
	defer devnull.Close()
	orig := os.Stdin
	os.Stdin = devnull
	t.Cleanup(func() { os.Stdin = orig })

	assert.False(t, IsInteractive())
}

func TestGetTerminalSize_Defaults(t *testing.T) {
	w, h := GetTerminalSize()
	assert.Equal(t, 80, w)
	assert.Equal(t, 24, h)
}

func TestBell_WritesBEL(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	Bell()
	require.NoError(t, w.Close())
	os.Stdout = orig

	out, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "\a", string(out))
}
