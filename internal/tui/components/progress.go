package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"

	"github.com/Didstopia/githubby/internal/tui"
)

// ProgressBar represents a progress bar component
type ProgressBar struct {
	Label    string
	Current  int
	Total    int
	Width    int
	ShowPct  bool
	bar      progress.Model
	styles   *tui.Styles
}

// NewProgressBar creates a new progress bar
func NewProgressBar() *ProgressBar {
	bar := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(40),
	)

	return &ProgressBar{
		bar:     bar,
		ShowPct: true,
		styles:  tui.GetStyles(),
	}
}

// SetLabel sets the progress label
func (p *ProgressBar) SetLabel(label string) *ProgressBar {
	p.Label = label
	return p
}

// SetProgress sets the current progress
func (p *ProgressBar) SetProgress(current, total int) *ProgressBar {
	p.Current = current
	p.Total = total
	return p
}

// SetWidth sets the progress bar width
func (p *ProgressBar) SetWidth(width int) *ProgressBar {
	p.Width = width
	p.bar.Width = width - 20 // account for label and percentage
	if p.bar.Width < 10 {
		p.bar.Width = 10
	}
	return p
}

// SetShowPct sets whether to show percentage
func (p *ProgressBar) SetShowPct(show bool) *ProgressBar {
	p.ShowPct = show
	return p
}

// View renders the progress bar
func (p *ProgressBar) View() string {
	var parts []string

	// Label
	if p.Label != "" {
		parts = append(parts, p.styles.ProgressLabel.Render(p.Label))
	}

	// Progress bar
	var pct float64
	if p.Total > 0 {
		pct = float64(p.Current) / float64(p.Total)
	}
	parts = append(parts, p.bar.ViewAs(pct))

	// Percentage
	if p.ShowPct {
		pctStr := fmt.Sprintf("%3.0f%%", pct*100)
		parts = append(parts, p.styles.ProgressPercent.Render(pctStr))
	}

	return lipgloss.JoinHorizontal(lipgloss.Center, strings.Join(parts, " "))
}

// ProgressItem represents a single progress item in a list
type ProgressItem struct {
	Name   string
	Status ProgressStatus
	Detail string
}

// ProgressStatus represents the status of a progress item
type ProgressStatus int

const (
	StatusPending ProgressStatus = iota
	StatusInProgress
	StatusSuccess
	StatusWarning
	StatusError
	StatusSkipped
)

// ProgressList represents a list of progress items
type ProgressList struct {
	Title  string
	Items  []ProgressItem
	Width  int
	Height int
	styles *tui.Styles
}

// NewProgressList creates a new progress list
func NewProgressList() *ProgressList {
	return &ProgressList{
		Items:  make([]ProgressItem, 0),
		styles: tui.GetStyles(),
	}
}

// SetTitle sets the list title
func (p *ProgressList) SetTitle(title string) *ProgressList {
	p.Title = title
	return p
}

// AddItem adds an item to the list
func (p *ProgressList) AddItem(name string, status ProgressStatus, detail string) *ProgressList {
	p.Items = append(p.Items, ProgressItem{
		Name:   name,
		Status: status,
		Detail: detail,
	})
	return p
}

// UpdateItem updates an existing item
func (p *ProgressList) UpdateItem(index int, status ProgressStatus, detail string) *ProgressList {
	if index >= 0 && index < len(p.Items) {
		p.Items[index].Status = status
		p.Items[index].Detail = detail
	}
	return p
}

// ClearItems clears all items
func (p *ProgressList) ClearItems() *ProgressList {
	p.Items = make([]ProgressItem, 0)
	return p
}

// SetDimensions sets the list dimensions
func (p *ProgressList) SetDimensions(width, height int) *ProgressList {
	p.Width = width
	p.Height = height
	return p
}

// View renders the progress list
func (p *ProgressList) View() string {
	var lines []string

	// Title
	if p.Title != "" {
		lines = append(lines, p.styles.ListTitle.Render(p.Title))
	}

	// Items
	for _, item := range p.Items {
		var statusIcon string
		var nameStyle lipgloss.Style

		switch item.Status {
		case StatusPending:
			statusIcon = p.styles.Muted.Render("○")
			nameStyle = p.styles.Muted
		case StatusInProgress:
			statusIcon = p.styles.Info.Render("◐")
			nameStyle = p.styles.Info
		case StatusSuccess:
			statusIcon = p.styles.Success.Render("●")
			nameStyle = p.styles.Success
		case StatusWarning:
			statusIcon = p.styles.Warning.Render("●")
			nameStyle = p.styles.Warning
		case StatusError:
			statusIcon = p.styles.Error.Render("●")
			nameStyle = p.styles.Error
		case StatusSkipped:
			statusIcon = p.styles.Muted.Render("○")
			nameStyle = p.styles.Muted
		}

		line := fmt.Sprintf("  %s %s", statusIcon, nameStyle.Render(item.Name))
		if item.Detail != "" {
			line += p.styles.Muted.Render(" - " + item.Detail)
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// Counters returns the count of items by status
func (p *ProgressList) Counters() map[ProgressStatus]int {
	counters := make(map[ProgressStatus]int)
	for _, item := range p.Items {
		counters[item.Status]++
	}
	return counters
}

// Summary returns a summary string
func (p *ProgressList) Summary() string {
	counters := p.Counters()
	var parts []string

	if c := counters[StatusSuccess]; c > 0 {
		parts = append(parts, p.styles.Success.Render(fmt.Sprintf("%d done", c)))
	}
	if c := counters[StatusError]; c > 0 {
		parts = append(parts, p.styles.Error.Render(fmt.Sprintf("%d failed", c)))
	}
	if c := counters[StatusSkipped]; c > 0 {
		parts = append(parts, p.styles.Muted.Render(fmt.Sprintf("%d skipped", c)))
	}
	if c := counters[StatusPending] + counters[StatusInProgress]; c > 0 {
		parts = append(parts, p.styles.Info.Render(fmt.Sprintf("%d remaining", c)))
	}

	return strings.Join(parts, "  ")
}
