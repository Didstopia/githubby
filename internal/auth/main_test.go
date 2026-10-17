package auth

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestMain swaps the OS keychain for an in-memory one so tests never touch the real one.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}
