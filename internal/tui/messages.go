package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	gh "github.com/google/go-github/v68/github"

	"github.com/Didstopia/githubby/internal/state"
	"github.com/Didstopia/githubby/internal/sync"
)

// Navigation Messages

// ScreenChangeMsg signals a direct screen change
type ScreenChangeMsg struct {
	Screen Screen
}

// ScreenPushMsg signals a screen push (saves current to stack)
type ScreenPushMsg struct {
	Screen Screen
}

// ScreenPopMsg signals a screen pop (return to previous)
type ScreenPopMsg struct{}

// QuitMsg signals app quit
type QuitMsg struct{}

// ErrorMsg signals an error occurred
type ErrorMsg struct {
	Err error
}

// Authentication Messages

// AuthCompleteMsg signals authentication completed
type AuthCompleteMsg struct {
	Token    string
	Username string
	Error    error
}

// AuthRequiredMsg signals authentication is required
type AuthRequiredMsg struct{}

// ExitTimeoutMsg signals exit confirmation timeout expired
type ExitTimeoutMsg struct{}

// Sync Wizard Messages

// OrgsLoadedMsg signals organizations have been fetched
type OrgsLoadedMsg struct {
	Orgs  []*gh.Organization
	Error error
}

// ReposLoadedMsg signals repositories have been fetched
type ReposLoadedMsg struct {
	Repos []*gh.Repository
	Error error
}

// SyncProgressMsg signals progress during sync
type SyncProgressMsg struct {
	RepoName string
	Status   sync.ProgressStatus
	Message  string
	Current  int
	Total    int
}

// SyncCompleteMsg signals sync completed
type SyncCompleteMsg struct {
	Result *sync.Result
	Record *state.SyncRecord
	Error  error
}

// ProfileSavedMsg signals a profile was saved
type ProfileSavedMsg struct {
	Profile *state.SyncProfile
	Error   error
}

// Dashboard Messages

// RefreshDashboardMsg signals dashboard should refresh data
type RefreshDashboardMsg struct{}

// ProfileSelectedMsg signals a profile was selected
type ProfileSelectedMsg struct {
	Profile *state.SyncProfile
}

// NewSyncRequestedMsg signals user wants to start a new sync
type NewSyncRequestedMsg struct{}

// QuickSyncRequestedMsg signals user wants to quick sync a profile
type QuickSyncRequestedMsg struct {
	ProfileID string
}

// SyncAllProfilesMsg signals user wants to sync all profiles
type SyncAllProfilesMsg struct{}

// SyncPendingProfilesMsg signals user wants to sync pending profiles
type SyncPendingProfilesMsg struct{}

// ClearMessageMsg signals a temporary message should be cleared
type ClearMessageMsg struct{}

// DeleteProfileRequestMsg signals user wants to delete a profile (shows confirmation)
type DeleteProfileRequestMsg struct {
	ProfileID   string
	ProfileName string
}

// DeleteProfileConfirmedMsg signals user confirmed profile deletion
type DeleteProfileConfirmedMsg struct {
	ProfileID   string
	ProfileName string
}

// DeleteProfileCancelledMsg signals user cancelled profile deletion
type DeleteProfileCancelledMsg struct{}

// UpdateAvailableMsg signals that an update is available
type UpdateAvailableMsg struct {
	CurrentVersion string
	LatestVersion  string
}

// Clean Messages

// ReleasesLoadedMsg signals releases have been fetched
type ReleasesLoadedMsg struct {
	Releases []*gh.RepositoryRelease
	Error    error
}

// DeleteProgressMsg signals progress during release deletion
type DeleteProgressMsg struct {
	ReleaseName string
	Success     bool
	Error       error
	Current     int
	Total       int
}

// Command Helpers

// ChangeScreen returns a command to change to a screen
func ChangeScreen(screen Screen) tea.Cmd {
	return func() tea.Msg {
		return ScreenChangeMsg{Screen: screen}
	}
}

// PushScreenCmd returns a command to push a screen
func PushScreenCmd(screen Screen) tea.Cmd {
	return func() tea.Msg {
		return ScreenPushMsg{Screen: screen}
	}
}

// PopScreenCmd returns a command to pop the screen stack
func PopScreenCmd() tea.Cmd {
	return func() tea.Msg {
		return ScreenPopMsg{}
	}
}

// QuitCmd returns a command to quit the app
func QuitCmd() tea.Cmd {
	return func() tea.Msg {
		return QuitMsg{}
	}
}

// ErrorCmd returns a command to set an error
func ErrorCmd(err error) tea.Cmd {
	return func() tea.Msg {
		return ErrorMsg{Err: err}
	}
}

// RefreshDashboardCmd returns a command to refresh dashboard
func RefreshDashboardCmd() tea.Cmd {
	return func() tea.Msg {
		return RefreshDashboardMsg{}
	}
}

// ClearMessageCmd returns a command to clear message after delay
func ClearMessageCmd(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return ClearMessageMsg{}
	})
}

// ExitTimeoutCmd returns a command for exit confirmation timeout
func ExitTimeoutCmd(timeout time.Duration) tea.Cmd {
	return tea.Tick(timeout, func(time.Time) tea.Msg {
		return ExitTimeoutMsg{}
	})
}

// Constants

const (
	// ExitConfirmTimeout is the duration before exit confirmation expires
	ExitConfirmTimeout = 2 * time.Second
	// MessageDisplayDuration is how long temporary messages are shown
	MessageDisplayDuration = 3 * time.Second
)
