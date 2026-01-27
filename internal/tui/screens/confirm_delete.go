package screens

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Didstopia/githubby/internal/tui"
)

// ConfirmDeleteScreen handles profile deletion confirmation
type ConfirmDeleteScreen struct {
	ctx    context.Context
	app    *tui.App
	styles *tui.Styles
	keys   tui.KeyMap

	// Profile to delete
	profileID   string
	profileName string

	// Selection state (0 = Cancel, 1 = Delete)
	selected int

	// Dimensions
	width  int
	height int
}

// NewConfirmDeleteScreen creates a new confirmation screen
func NewConfirmDeleteScreen(ctx context.Context, app *tui.App) *ConfirmDeleteScreen {
	return &ConfirmDeleteScreen{
		ctx:      ctx,
		app:      app,
		styles:   tui.GetStyles(),
		keys:     tui.GetKeyMap(),
		selected: 0, // Default to Cancel
		width:    80,
		height:   24,
	}
}

// SetProfile sets the profile to be deleted
func (c *ConfirmDeleteScreen) SetProfile(id, name string) {
	c.profileID = id
	c.profileName = name
	c.selected = 0 // Reset to Cancel
}

// Title returns the screen title
func (c *ConfirmDeleteScreen) Title() string {
	return "Confirm Delete"
}

// ShortHelp returns key bindings for the footer
func (c *ConfirmDeleteScreen) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("left", "right", "h", "l", "tab"), key.WithHelp("←/→", "select")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

// Init initializes the screen
func (c *ConfirmDeleteScreen) Init() tea.Cmd {
	// Get profile info from app
	c.profileID, c.profileName = c.app.DeleteProfileInfo()
	c.selected = 0 // Default to Cancel
	return nil
}

// Update handles messages
func (c *ConfirmDeleteScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height

	case tea.KeyMsg:
		switch {
		case msg.String() == "left" || msg.String() == "h":
			c.selected = 0 // Cancel
		case msg.String() == "right" || msg.String() == "l":
			c.selected = 1 // Delete
		case msg.String() == "tab":
			c.selected = (c.selected + 1) % 2
		case msg.String() == "enter":
			if c.selected == 1 {
				// Delete confirmed
				return c, func() tea.Msg {
					return tui.DeleteProfileConfirmedMsg{
						ProfileID:   c.profileID,
						ProfileName: c.profileName,
					}
				}
			}
			// Cancel
			return c, func() tea.Msg {
				return tui.DeleteProfileCancelledMsg{}
			}
		case msg.String() == "esc" || msg.String() == "n":
			return c, func() tea.Msg {
				return tui.DeleteProfileCancelledMsg{}
			}
		case msg.String() == "y":
			// Quick confirm with 'y'
			return c, func() tea.Msg {
				return tui.DeleteProfileConfirmedMsg{
					ProfileID:   c.profileID,
					ProfileName: c.profileName,
				}
			}
		}
	}

	return c, nil
}

// View renders the confirmation screen
func (c *ConfirmDeleteScreen) View() string {
	var content strings.Builder

	// Warning icon and message
	warningStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("208")).
		Bold(true)

	content.WriteString(warningStyle.Render("⚠ DELETE PROFILE"))
	content.WriteString("\n\n")

	// Profile name
	content.WriteString("You are about to delete the following profile:\n\n")
	content.WriteString(c.styles.FormTitle.Render(fmt.Sprintf("  %s", c.profileName)))
	content.WriteString("\n\n")

	// Warning message
	content.WriteString(c.styles.Warning.Render("This action cannot be undone."))
	content.WriteString("\n")
	content.WriteString(c.styles.Muted.Render("The synced repositories will not be deleted from disk."))
	content.WriteString("\n\n")

	// Buttons
	cancelStyle := lipgloss.NewStyle().
		Padding(0, 3).
		Border(lipgloss.RoundedBorder())

	deleteStyle := lipgloss.NewStyle().
		Padding(0, 3).
		Border(lipgloss.RoundedBorder())

	if c.selected == 0 {
		cancelStyle = cancelStyle.
			BorderForeground(lipgloss.Color("86")).
			Foreground(lipgloss.Color("86")).
			Bold(true)
		deleteStyle = deleteStyle.
			BorderForeground(lipgloss.Color("240")).
			Foreground(lipgloss.Color("240"))
	} else {
		cancelStyle = cancelStyle.
			BorderForeground(lipgloss.Color("240")).
			Foreground(lipgloss.Color("240"))
		deleteStyle = deleteStyle.
			BorderForeground(lipgloss.Color("196")).
			Foreground(lipgloss.Color("196")).
			Bold(true)
	}

	cancelBtn := cancelStyle.Render("Cancel")
	deleteBtn := deleteStyle.Render("Delete")

	buttons := lipgloss.JoinHorizontal(lipgloss.Center, cancelBtn, "  ", deleteBtn)
	content.WriteString(buttons)

	// Center the content
	boxStyle := lipgloss.NewStyle().
		Padding(2, 4).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Width(50)

	box := boxStyle.Render(content.String())

	// Center vertically and horizontally
	return lipgloss.Place(
		c.width-4,
		c.height-10,
		lipgloss.Center,
		lipgloss.Center,
		box,
	)
}
