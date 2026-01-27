// Package components provides reusable TUI components
package components

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/Didstopia/githubby/internal/tui"
)

// Header represents the app header component
type Header struct {
	Title           string
	Subtitle        string
	IsAuthenticated bool
	Username        string
	Width           int
	styles          *tui.Styles
}

// NewHeader creates a new header component
func NewHeader() *Header {
	return &Header{
		Title:  "GitHubby",
		styles: tui.GetStyles(),
	}
}

// SetTitle sets the header title
func (h *Header) SetTitle(title string) *Header {
	h.Title = title
	return h
}

// SetSubtitle sets the header subtitle
func (h *Header) SetSubtitle(subtitle string) *Header {
	h.Subtitle = subtitle
	return h
}

// SetAuth sets the authentication state
func (h *Header) SetAuth(authenticated bool, username string) *Header {
	h.IsAuthenticated = authenticated
	h.Username = username
	return h
}

// SetWidth sets the header width
func (h *Header) SetWidth(width int) *Header {
	h.Width = width
	return h
}

// View renders the header
func (h *Header) View() string {
	// Title section
	title := h.styles.HeaderTitle.Render(h.Title)
	if h.Subtitle != "" {
		title += h.styles.Muted.Render(" | " + h.Subtitle)
	}

	// Status section
	var status string
	if h.IsAuthenticated {
		status = h.styles.Success.Render("●") + " " + h.styles.HeaderStatus.Render(h.Username)
	} else {
		status = h.styles.Error.Render("●") + " " + h.styles.HeaderStatus.Render("Not authenticated")
	}

	// Calculate gap
	width := h.Width
	if width == 0 {
		width = 80
	}
	gap := width - lipgloss.Width(title) - lipgloss.Width(status) - 6
	if gap < 1 {
		gap = 1
	}

	headerContent := title + lipgloss.NewStyle().Width(gap).Render("") + status

	return h.styles.Header.Width(width - 4).Render(headerContent)
}
