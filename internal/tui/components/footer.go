package components

import (
	"github.com/charmbracelet/bubbles/key"

	"github.com/Didstopia/githubby/internal/tui"
)

// Footer represents the app footer component with help
type Footer struct {
	HelpBindings []key.Binding
	ShowQuit     bool
	Width        int
	styles       *tui.Styles
}

// NewFooter creates a new footer component
func NewFooter() *Footer {
	return &Footer{
		HelpBindings: make([]key.Binding, 0),
		ShowQuit:     true,
		styles:       tui.GetStyles(),
	}
}

// SetBindings sets the help key bindings
func (f *Footer) SetBindings(bindings []key.Binding) *Footer {
	f.HelpBindings = bindings
	return f
}

// SetShowQuit sets whether to show quit help
func (f *Footer) SetShowQuit(show bool) *Footer {
	f.ShowQuit = show
	return f
}

// SetWidth sets the footer width
func (f *Footer) SetWidth(width int) *Footer {
	f.Width = width
	return f
}

// View renders the footer
func (f *Footer) View() string {
	var helpItems []string

	// Render help bindings
	for _, binding := range f.HelpBindings {
		if binding.Enabled() {
			help := f.styles.HelpKey.Render(binding.Help().Key) + " " +
				f.styles.HelpValue.Render(binding.Help().Desc)
			helpItems = append(helpItems, help)
		}
	}

	// Add quit help if enabled
	if f.ShowQuit {
		quitHelp := f.styles.HelpKey.Render("q") + " " + f.styles.HelpValue.Render("quit")
		helpItems = append(helpItems, quitHelp)
	}

	// Join help items
	helpText := ""
	for i, item := range helpItems {
		if i > 0 {
			helpText += "  "
		}
		helpText += item
	}

	width := f.Width
	if width == 0 {
		width = 80
	}

	return f.styles.Footer.Width(width - 4).Render(helpText)
}

// HelpItem represents a single help item
type HelpItem struct {
	Key   string
	Value string
}

// SimpleFooter creates a footer from simple key/value pairs
type SimpleFooter struct {
	Items    []HelpItem
	ShowQuit bool
	Width    int
	styles   *tui.Styles
}

// NewSimpleFooter creates a new simple footer
func NewSimpleFooter() *SimpleFooter {
	return &SimpleFooter{
		Items:    make([]HelpItem, 0),
		ShowQuit: true,
		styles:   tui.GetStyles(),
	}
}

// AddItem adds a help item
func (f *SimpleFooter) AddItem(key, value string) *SimpleFooter {
	f.Items = append(f.Items, HelpItem{Key: key, Value: value})
	return f
}

// SetShowQuit sets whether to show quit help
func (f *SimpleFooter) SetShowQuit(show bool) *SimpleFooter {
	f.ShowQuit = show
	return f
}

// SetWidth sets the footer width
func (f *SimpleFooter) SetWidth(width int) *SimpleFooter {
	f.Width = width
	return f
}

// View renders the footer
func (f *SimpleFooter) View() string {
	var helpItems []string

	for _, item := range f.Items {
		help := f.styles.HelpKey.Render(item.Key) + " " +
			f.styles.HelpValue.Render(item.Value)
		helpItems = append(helpItems, help)
	}

	if f.ShowQuit {
		quitHelp := f.styles.HelpKey.Render("q") + " " + f.styles.HelpValue.Render("quit")
		helpItems = append(helpItems, quitHelp)
	}

	helpText := ""
	for i, item := range helpItems {
		if i > 0 {
			helpText += "  "
		}
		helpText += item
	}

	width := f.Width
	if width == 0 {
		width = 80
	}

	return f.styles.Footer.Width(width - 4).Render(helpText)
}
