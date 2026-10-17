package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInitSkipsStartupUpdateWhenTurnedOff(t *testing.T) {
	app := NewApp(WithVersion("1.0.0", "abc", "now"), WithNoUpdate(true))
	app.Init()
	assert.Equal(t, UpdatePhaseNone, app.startupUpdatePhase)
	assert.Nil(t, app.checkForUpdateCmd())
}

func TestInitChecksForUpdateByDefault(t *testing.T) {
	app := NewApp(WithVersion("1.0.0", "abc", "now"))
	cmd := app.Init()
	assert.NotNil(t, cmd)
	assert.Equal(t, UpdatePhaseChecking, app.startupUpdatePhase)
}

func TestDevBuildSkipsUpdates(t *testing.T) {
	app := NewApp(WithVersion("dev", "abc", "now"))
	app.Init()
	assert.Equal(t, UpdatePhaseNone, app.startupUpdatePhase)
	assert.Nil(t, app.checkForUpdateCmd())
}
