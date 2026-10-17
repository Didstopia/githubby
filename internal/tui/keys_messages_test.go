package tui

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestDefaultKeyMap(t *testing.T) {
	k := DefaultKeyMap()
	assert.Equal(t, []string{"up", "k"}, k.Up.Keys())
	assert.Equal(t, []string{"q", "ctrl+c"}, k.Quit.Keys())
	assert.Equal(t, []string{"esc"}, k.Back.Keys())

	short := k.ShortHelp()
	assert.Len(t, short, 4)
	assert.Equal(t, "enter", short[0].Help().Key)

	full := k.FullHelp()
	assert.Len(t, full, 4)
	for _, row := range full {
		assert.NotEmpty(t, row)
	}

	assert.Equal(t, k.Up.Keys(), GetKeyMap().Up.Keys())
}

func TestStyles(t *testing.T) {
	s := DefaultStyles()
	assert.Contains(t, s.HeaderTitle.Render("hi"), "hi")
	assert.Contains(t, s.BadgePrivate.Render("private"), "private")
	assert.Same(t, GetStyles(), GetStyles())
}

func TestCommandHelpers(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name string
		cmd  tea.Cmd
		want tea.Msg
	}{
		{"change screen", ChangeScreen(ScreenClean), ScreenChangeMsg{Screen: ScreenClean}},
		{"push screen", PushScreenCmd(ScreenSettings), ScreenPushMsg{Screen: ScreenSettings}},
		{"pop screen", PopScreenCmd(), ScreenPopMsg{}},
		{"quit", QuitCmd(), QuitMsg{}},
		{"error", ErrorCmd(boom), ErrorMsg{Err: boom}},
		{"refresh dashboard", RefreshDashboardCmd(), RefreshDashboardMsg{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cmd())
		})
	}
}

func TestTickHelpers(t *testing.T) {
	assert.Equal(t, ClearMessageMsg{}, ClearMessageCmd(time.Millisecond)())
	assert.Equal(t, ExitTimeoutMsg{}, ExitTimeoutCmd(time.Millisecond)())
	assert.Equal(t, 2*time.Second, ExitConfirmTimeout)
	assert.Equal(t, 3*time.Second, MessageDisplayDuration)
}
