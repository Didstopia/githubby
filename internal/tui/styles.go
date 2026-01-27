// Package tui provides the terminal user interface for GitHubby
package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Color palette
var (
	// Primary colors
	ColorPrimary   = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#A78BFA"} // Purple
	ColorSecondary = lipgloss.AdaptiveColor{Light: "#059669", Dark: "#34D399"} // Green
	ColorAccent    = lipgloss.AdaptiveColor{Light: "#0284C7", Dark: "#38BDF8"} // Blue

	// Status colors
	ColorSuccess = lipgloss.AdaptiveColor{Light: "#16A34A", Dark: "#4ADE80"}
	ColorWarning = lipgloss.AdaptiveColor{Light: "#CA8A04", Dark: "#FACC15"}
	ColorError   = lipgloss.AdaptiveColor{Light: "#DC2626", Dark: "#F87171"}
	ColorInfo    = lipgloss.AdaptiveColor{Light: "#0284C7", Dark: "#38BDF8"}

	// Neutral colors
	ColorMuted    = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	ColorSubtle   = lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#6B7280"}
	ColorBorder   = lipgloss.AdaptiveColor{Light: "#E5E7EB", Dark: "#374151"}
	ColorSelected = lipgloss.AdaptiveColor{Light: "#F3F4F6", Dark: "#1F2937"}
)

// Styles contains all the TUI styles
type Styles struct {
	// App-level styles
	App     lipgloss.Style
	Content lipgloss.Style

	// Header styles
	Header       lipgloss.Style
	HeaderTitle  lipgloss.Style
	HeaderStatus lipgloss.Style

	// Footer styles
	Footer    lipgloss.Style
	HelpKey   lipgloss.Style
	HelpValue lipgloss.Style

	// Menu styles
	MenuItem         lipgloss.Style
	MenuItemSelected lipgloss.Style
	MenuItemDisabled lipgloss.Style

	// List styles
	ListTitle     lipgloss.Style
	ListItem      lipgloss.Style
	ListItemDesc  lipgloss.Style
	ListSelected  lipgloss.Style
	ListPaginator lipgloss.Style

	// Form styles
	FormTitle       lipgloss.Style
	FormDescription lipgloss.Style
	FormInput       lipgloss.Style
	FormPlaceholder lipgloss.Style

	// Progress styles
	ProgressBar      lipgloss.Style
	ProgressLabel    lipgloss.Style
	ProgressPercent  lipgloss.Style
	ProgressComplete lipgloss.Style

	// Status styles
	Success lipgloss.Style
	Warning lipgloss.Style
	Error   lipgloss.Style
	Info    lipgloss.Style
	Muted   lipgloss.Style

	// Box styles
	Box            lipgloss.Style
	BoxTitle       lipgloss.Style
	BoxHighlighted lipgloss.Style

	// Badge styles
	Badge        lipgloss.Style
	BadgePrivate lipgloss.Style
	BadgePublic  lipgloss.Style

	// Spinner
	Spinner lipgloss.Style
}

// DefaultStyles returns the default style configuration
func DefaultStyles() *Styles {
	s := &Styles{}

	// App-level
	s.App = lipgloss.NewStyle().
		Padding(1, 2)

	s.Content = lipgloss.NewStyle().
		Padding(0, 1)

	// Header
	s.Header = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(ColorBorder).
		Padding(0, 1).
		MarginBottom(1)

	s.HeaderTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary)

	s.HeaderStatus = lipgloss.NewStyle().
		Foreground(ColorMuted)

	// Footer
	s.Footer = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(ColorBorder).
		Padding(0, 1).
		MarginTop(1)

	s.HelpKey = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary)

	s.HelpValue = lipgloss.NewStyle().
		Foreground(ColorMuted)

	// Menu
	s.MenuItem = lipgloss.NewStyle().
		Padding(0, 2)

	s.MenuItemSelected = lipgloss.NewStyle().
		Padding(0, 2).
		Background(ColorSelected).
		Bold(true).
		Foreground(ColorPrimary)

	s.MenuItemDisabled = lipgloss.NewStyle().
		Padding(0, 2).
		Foreground(ColorSubtle)

	// List
	s.ListTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	s.ListItem = lipgloss.NewStyle().
		PaddingLeft(2)

	s.ListItemDesc = lipgloss.NewStyle().
		Foreground(ColorMuted)

	s.ListSelected = lipgloss.NewStyle().
		PaddingLeft(2).
		Foreground(ColorPrimary).
		Bold(true)

	s.ListPaginator = lipgloss.NewStyle().
		Foreground(ColorMuted).
		MarginTop(1)

	// Form
	s.FormTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	s.FormDescription = lipgloss.NewStyle().
		Foreground(ColorMuted).
		MarginBottom(1)

	s.FormInput = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder).
		Padding(0, 1)

	s.FormPlaceholder = lipgloss.NewStyle().
		Foreground(ColorSubtle)

	// Progress
	s.ProgressBar = lipgloss.NewStyle()

	s.ProgressLabel = lipgloss.NewStyle().
		Foreground(ColorMuted)

	s.ProgressPercent = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary)

	s.ProgressComplete = lipgloss.NewStyle().
		Foreground(ColorSuccess)

	// Status
	s.Success = lipgloss.NewStyle().
		Foreground(ColorSuccess)

	s.Warning = lipgloss.NewStyle().
		Foreground(ColorWarning)

	s.Error = lipgloss.NewStyle().
		Foreground(ColorError)

	s.Info = lipgloss.NewStyle().
		Foreground(ColorInfo)

	s.Muted = lipgloss.NewStyle().
		Foreground(ColorMuted)

	// Box
	s.Box = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder).
		Padding(1, 2)

	s.BoxTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary)

	s.BoxHighlighted = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2)

	// Badge
	s.Badge = lipgloss.NewStyle().
		Padding(0, 1).
		Bold(true)

	s.BadgePrivate = lipgloss.NewStyle().
		Padding(0, 1).
		Bold(true).
		Background(ColorWarning).
		Foreground(lipgloss.Color("#000000"))

	s.BadgePublic = lipgloss.NewStyle().
		Padding(0, 1).
		Bold(true).
		Background(ColorSuccess).
		Foreground(lipgloss.Color("#000000"))

	// Spinner
	s.Spinner = lipgloss.NewStyle().
		Foreground(ColorPrimary)

	return s
}

// Global styles instance
var styles = DefaultStyles()

// GetStyles returns the global styles instance
func GetStyles() *Styles {
	return styles
}
