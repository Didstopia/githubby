package screens

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/tui"
)

// cd_app builds an App that remembers a profile queued for deletion.
func cd_app(t *testing.T, id, name string) *tui.App {
	t.Helper()
	app := tui.NewApp()
	app.Update(tui.DeleteProfileRequestMsg{ProfileID: id, ProfileName: name})
	return app
}

func cd_screen(t *testing.T) *ConfirmDeleteScreen {
	t.Helper()
	s := NewConfirmDeleteScreen(context.Background(), cd_app(t, "p1", "My Profile"))
	s.Init()
	return s
}

func cd_press(s *ConfirmDeleteScreen, k string) tea.Cmd {
	_, cmd := s.Update(cl_press(k))
	return cmd
}

func TestConfirmDelete_InitReadsProfileFromApp(t *testing.T) {
	s := cd_screen(t)
	assert.Equal(t, "p1", s.profileID)
	assert.Equal(t, "My Profile", s.profileName)
	assert.Equal(t, 0, s.selected)
	assert.Equal(t, "Confirm Delete", s.Title())
	assert.Len(t, s.ShortHelp(), 3)
}

func TestConfirmDelete_SetProfile(t *testing.T) {
	s := cd_screen(t)
	s.selected = 1
	s.SetProfile("x", "Other")
	assert.Equal(t, "x", s.profileID)
	assert.Equal(t, "Other", s.profileName)
	assert.Equal(t, 0, s.selected)
}

func TestConfirmDelete_Selection(t *testing.T) {
	s := cd_screen(t)

	cd_press(s, "right")
	assert.Equal(t, 1, s.selected)
	cd_press(s, "left")
	assert.Equal(t, 0, s.selected)
	cd_press(s, "l")
	assert.Equal(t, 1, s.selected)
	cd_press(s, "h")
	assert.Equal(t, 0, s.selected)
	cd_press(s, "tab")
	assert.Equal(t, 1, s.selected)
	cd_press(s, "tab")
	assert.Equal(t, 0, s.selected)
}

func TestConfirmDelete_Decisions(t *testing.T) {
	confirmed := tui.DeleteProfileConfirmedMsg{ProfileID: "p1", ProfileName: "My Profile"}
	cancelled := tui.DeleteProfileCancelledMsg{}

	tests := []struct {
		name string
		keys []string
		want tea.Msg
	}{
		{"enter on Cancel cancels", []string{"enter"}, cancelled},
		{"enter on Delete confirms", []string{"right", "enter"}, confirmed},
		{"esc cancels", []string{"right", "esc"}, cancelled},
		{"n cancels", []string{"n"}, cancelled},
		{"y confirms right away", []string{"y"}, confirmed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := cd_screen(t)
			var cmd tea.Cmd
			for _, k := range tt.keys {
				cmd = cd_press(s, k)
			}
			require.NotNil(t, cmd)
			assert.Equal(t, tt.want, cmd())
		})
	}

	t.Run("other keys do nothing", func(t *testing.T) {
		assert.Nil(t, cd_press(cd_screen(t), "z"))
	})
}

func TestConfirmDelete_View(t *testing.T) {
	s := cd_screen(t)
	s.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	assert.Equal(t, 100, s.width)

	for _, selected := range []int{0, 1} {
		s.selected = selected
		v := s.View()
		assert.Contains(t, v, "DELETE PROFILE")
		assert.Contains(t, v, "My Profile")
		assert.Contains(t, v, "This action cannot be undone.")
		assert.Contains(t, v, "Cancel")
		assert.Contains(t, v, "Delete")
	}
}

// Compile-time check that the screen satisfies the app's screen interface.
var _ tui.ScreenModel = (*ConfirmDeleteScreen)(nil)
