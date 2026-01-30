package screens

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	gh "github.com/google/go-github/v68/github"

	"github.com/Didstopia/githubby/internal/github"
	"github.com/Didstopia/githubby/internal/tui"
)

// ReleaseItem represents a release in the list
type ReleaseItem struct {
	release  *gh.RepositoryRelease
	selected bool
}

func (i ReleaseItem) Title() string {
	name := i.release.GetTagName()
	if i.release.GetName() != "" && i.release.GetName() != i.release.GetTagName() {
		name = i.release.GetName() + " (" + i.release.GetTagName() + ")"
	}
	if i.selected {
		name = "[x] " + name
	} else {
		name = "[ ] " + name
	}
	return name
}

func (i ReleaseItem) Description() string {
	var parts []string

	created := i.release.GetCreatedAt()
	if !created.IsZero() {
		age := time.Since(created.Time)
		if age.Hours() < 24 {
			parts = append(parts, fmt.Sprintf("%.0fh ago", age.Hours()))
		} else {
			parts = append(parts, fmt.Sprintf("%.0fd ago", age.Hours()/24))
		}
	}

	if i.release.GetPrerelease() {
		parts = append(parts, "prerelease")
	}

	if i.release.GetDraft() {
		parts = append(parts, "draft")
	}

	return strings.Join(parts, " | ")
}

func (i ReleaseItem) FilterValue() string {
	return i.release.GetTagName()
}

// CleanScreen is the release cleanup screen
type CleanScreen struct {
	ctx    context.Context
	styles *tui.Styles
	keys   tui.KeyMap

	// GitHub client
	client github.Client

	// Inputs
	repoInput        textinput.Model
	filterDaysInput  textinput.Model
	filterCountInput textinput.Model

	// List component
	list list.Model

	// Spinner
	spinner spinner.Model

	// State
	mode        cleanMode
	owner       string
	repo        string
	releases    []*gh.RepositoryRelease
	selected    map[int64]bool
	filterDays  int
	filterCount int

	// Deletion progress
	deleteIdx    int
	deleted      int
	deleteFailed int

	// Channels for async operations
	releasesChan chan releasesLoadResult
	deleteChan   chan deleteResult

	// Dimensions
	width  int
	height int

	// Flags
	loading  bool
	deleting bool
	complete bool
	err      error
}

type cleanMode int

const (
	cleanModeRepoInput cleanMode = iota
	cleanModeFilterInput
	cleanModeLoadingReleases
	cleanModeSelectReleases
	cleanModeConfirmDelete
	cleanModeDeleting
	cleanModeComplete
)

// NewCleanScreen creates a new clean screen
func NewCleanScreen(ctx context.Context, client github.Client) *CleanScreen {
	// Create spinner
	s := spinner.New()
	s.Spinner = spinner.Pulse
	s.Style = tui.GetStyles().Spinner

	// Create repo input
	ri := textinput.New()
	ri.Placeholder = "owner/repo"
	ri.Focus()
	ri.CharLimit = 100
	ri.Width = 40

	// Create filter days input
	fdi := textinput.New()
	fdi.Placeholder = "30"
	fdi.CharLimit = 5
	fdi.Width = 10

	// Create filter count input
	fci := textinput.New()
	fci.Placeholder = "10"
	fci.CharLimit = 5
	fci.Width = 10

	// Create list
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = tui.GetStyles().ListSelected
	delegate.Styles.SelectedDesc = tui.GetStyles().Muted
	delegate.Styles.NormalTitle = tui.GetStyles().ListItem
	delegate.Styles.NormalDesc = tui.GetStyles().Muted

	l := list.New([]list.Item{}, delegate, 60, 15)
	l.Title = "Select releases to delete"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.Styles.Title = tui.GetStyles().ListTitle

	return &CleanScreen{
		ctx:              ctx,
		styles:           tui.GetStyles(),
		keys:             tui.GetKeyMap(),
		client:           client,
		repoInput:        ri,
		filterDaysInput:  fdi,
		filterCountInput: fci,
		list:             l,
		spinner:          s,
		selected:         make(map[int64]bool),
		filterDays:       -1,
		filterCount:      -1,
		mode:             cleanModeRepoInput,
		width:            80,
		height:           24,
	}
}

// SetRepository sets the repository to clean
func (c *CleanScreen) SetRepository(owner, repo string) *CleanScreen {
	c.owner = owner
	c.repo = repo
	c.repoInput.SetValue(owner + "/" + repo)
	return c
}

// Title returns the screen title
func (c *CleanScreen) Title() string {
	return "Clean Releases"
}

// ShortHelp returns key bindings for the footer
func (c *CleanScreen) ShortHelp() []key.Binding {
	switch c.mode {
	case cleanModeSelectReleases:
		return []key.Binding{
			c.keys.Toggle,
			c.keys.SelectAll,
			c.keys.SelectNone,
			c.keys.Select,
			c.keys.Back,
		}
	default:
		return []key.Binding{
			c.keys.Select,
			c.keys.Back,
		}
	}
}

// Init initializes the clean screen
func (c *CleanScreen) Init() tea.Cmd {
	return tea.Batch(
		c.spinner.Tick,
		textinput.Blink,
	)
}

// Update handles messages
func (c *CleanScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
		c.list.SetWidth(msg.Width - 4)
		c.list.SetHeight(msg.Height - 12)

	case tea.KeyMsg:
		switch c.mode {
		case cleanModeRepoInput:
			switch {
			case key.Matches(msg, c.keys.Select):
				// Parse repo input
				parts := strings.Split(c.repoInput.Value(), "/")
				if len(parts) == 2 {
					c.owner = parts[0]
					c.repo = parts[1]
					c.mode = cleanModeFilterInput
					c.filterDaysInput.Focus()
				} else {
					c.err = fmt.Errorf("invalid repository format, use owner/repo")
				}
			case key.Matches(msg, c.keys.Back):
				return c, tui.PopScreenCmd()
			}

		case cleanModeFilterInput:
			switch {
			case key.Matches(msg, c.keys.Select):
				// Parse filters and load releases
				if v := c.filterDaysInput.Value(); v != "" {
					fmt.Sscanf(v, "%d", &c.filterDays)
				}
				if v := c.filterCountInput.Value(); v != "" {
					fmt.Sscanf(v, "%d", &c.filterCount)
				}
				c.mode = cleanModeLoadingReleases
				c.loading = true
				return c, c.loadReleases()
			case key.Matches(msg, c.keys.Back):
				c.mode = cleanModeRepoInput
				c.repoInput.Focus()
			case key.Matches(msg, c.keys.Tab):
				// Toggle between filter inputs
				if c.filterDaysInput.Focused() {
					c.filterDaysInput.Blur()
					c.filterCountInput.Focus()
				} else {
					c.filterCountInput.Blur()
					c.filterDaysInput.Focus()
				}
			}

		case cleanModeSelectReleases:
			switch {
			case key.Matches(msg, c.keys.Toggle):
				if item, ok := c.list.SelectedItem().(ReleaseItem); ok {
					id := item.release.GetID()
					c.selected[id] = !c.selected[id]
					c.updateListItems()
				}
			case key.Matches(msg, c.keys.SelectAll):
				for _, release := range c.releases {
					c.selected[release.GetID()] = true
				}
				c.updateListItems()
			case key.Matches(msg, c.keys.SelectNone):
				c.selected = make(map[int64]bool)
				c.updateListItems()
			case key.Matches(msg, c.keys.Select):
				count := 0
				for _, sel := range c.selected {
					if sel {
						count++
					}
				}
				if count > 0 {
					c.mode = cleanModeConfirmDelete
				}
			case key.Matches(msg, c.keys.Back):
				c.mode = cleanModeFilterInput
				c.filterDaysInput.Focus()
			}

		case cleanModeConfirmDelete:
			switch {
			case key.Matches(msg, c.keys.Confirm):
				c.mode = cleanModeDeleting
				c.deleting = true
				return c, c.startDeletion()
			case key.Matches(msg, c.keys.Back):
				c.mode = cleanModeSelectReleases
			}

		case cleanModeComplete:
			switch {
			case key.Matches(msg, c.keys.Back):
				return c, tui.PopScreenCmd()
			}
		}

	case spinner.TickMsg:
		if c.loading || c.deleting {
			var cmd tea.Cmd
			c.spinner, cmd = c.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	case releasesLoadedMsg:
		c.loading = false
		if msg.err != nil {
			c.err = msg.err
			c.mode = cleanModeRepoInput
		} else {
			c.releases = msg.releases
			c.applyFilters()
			c.updateListItems()
			c.mode = cleanModeSelectReleases
		}

	case deleteProgressMsg:
		if msg.err != nil {
			c.deleteFailed++
		} else {
			c.deleted++
		}
		c.deleteIdx++

		// Check if done
		count := 0
		for _, sel := range c.selected {
			if sel {
				count++
			}
		}
		if c.deleteIdx >= count {
			c.deleting = false
			c.complete = true
			c.mode = cleanModeComplete
		} else {
			// Continue deletion
			cmds = append(cmds, c.deleteNext())
		}
	}

	// Update components based on mode
	switch c.mode {
	case cleanModeRepoInput:
		var cmd tea.Cmd
		c.repoInput, cmd = c.repoInput.Update(msg)
		cmds = append(cmds, cmd)

	case cleanModeFilterInput:
		var cmd tea.Cmd
		if c.filterDaysInput.Focused() {
			c.filterDaysInput, cmd = c.filterDaysInput.Update(msg)
		} else {
			c.filterCountInput, cmd = c.filterCountInput.Update(msg)
		}
		cmds = append(cmds, cmd)

	case cleanModeSelectReleases:
		var cmd tea.Cmd
		c.list, cmd = c.list.Update(msg)
		cmds = append(cmds, cmd)
	}

	return c, tea.Batch(cmds...)
}

// View renders the clean screen
func (c *CleanScreen) View() string {
	var content strings.Builder

	switch c.mode {
	case cleanModeRepoInput:
		content.WriteString(c.styles.FormTitle.Render("Clean Releases"))
		content.WriteString("\n\n")
		content.WriteString("Repository to clean:\n\n")
		content.WriteString(c.repoInput.View())
		if c.err != nil {
			content.WriteString("\n\n")
			content.WriteString(c.styles.Error.Render(c.err.Error()))
		}
		content.WriteString("\n\n")
		content.WriteString(c.styles.Muted.Render("Format: owner/repo"))

	case cleanModeFilterInput:
		content.WriteString(c.styles.FormTitle.Render("Filter Releases"))
		content.WriteString("\n\n")
		content.WriteString(c.styles.Info.Render("Repository: " + c.owner + "/" + c.repo))
		content.WriteString("\n\n")
		content.WriteString("Delete releases older than (days, -1 for no limit):\n")
		content.WriteString(c.filterDaysInput.View())
		content.WriteString("\n\n")
		content.WriteString("Keep only the most recent (count, -1 for no limit):\n")
		content.WriteString(c.filterCountInput.View())
		content.WriteString("\n\n")
		content.WriteString(c.styles.Muted.Render("Press Tab to switch fields, Enter to continue"))

	case cleanModeLoadingReleases:
		content.WriteString(c.spinner.View())
		content.WriteString(" Loading releases from " + c.owner + "/" + c.repo + "...")

	case cleanModeSelectReleases:
		if c.err != nil {
			content.WriteString(c.styles.Error.Render("Error: " + c.err.Error()))
			content.WriteString("\n\n")
			content.WriteString("Press " + c.styles.HelpKey.Render("Esc") + " to go back")
		} else if len(c.releases) == 0 {
			content.WriteString(c.styles.Warning.Render("No releases found matching the filters"))
			content.WriteString("\n\n")
			content.WriteString("Press " + c.styles.HelpKey.Render("Esc") + " to go back")
		} else {
			count := 0
			for _, sel := range c.selected {
				if sel {
					count++
				}
			}
			content.WriteString(c.styles.Info.Render(fmt.Sprintf("%d of %d selected for deletion", count, len(c.releases))))
			content.WriteString("\n\n")
			content.WriteString(c.list.View())
		}

	case cleanModeConfirmDelete:
		content.WriteString(c.styles.FormTitle.Render("Confirm Deletion"))
		content.WriteString("\n\n")

		count := 0
		for _, sel := range c.selected {
			if sel {
				count++
			}
		}

		content.WriteString(c.styles.Warning.Render(fmt.Sprintf("You are about to delete %d releases from %s/%s", count, c.owner, c.repo)))
		content.WriteString("\n\n")
		content.WriteString(c.styles.Error.Render("This action cannot be undone!"))
		content.WriteString("\n\n")

		// Show selected releases
		content.WriteString("Releases to delete:\n")
		shown := 0
		for _, release := range c.releases {
			if c.selected[release.GetID()] && shown < 10 {
				content.WriteString(c.styles.Muted.Render("  • " + release.GetTagName()))
				content.WriteString("\n")
				shown++
			}
		}
		if count > 10 {
			content.WriteString(c.styles.Muted.Render(fmt.Sprintf("  ... and %d more", count-10)))
			content.WriteString("\n")
		}

		content.WriteString("\n")
		content.WriteString("Press " + c.styles.HelpKey.Render("Enter/y") + " to delete, ")
		content.WriteString(c.styles.HelpKey.Render("Esc") + " to cancel")

	case cleanModeDeleting:
		content.WriteString(c.spinner.View())
		content.WriteString(fmt.Sprintf(" Deleting releases... %d/%d", c.deleteIdx, c.countSelected()))
		content.WriteString("\n\n")
		if c.deleted > 0 {
			content.WriteString(c.styles.Success.Render(fmt.Sprintf("%d deleted", c.deleted)))
			content.WriteString("  ")
		}
		if c.deleteFailed > 0 {
			content.WriteString(c.styles.Error.Render(fmt.Sprintf("%d failed", c.deleteFailed)))
		}

	case cleanModeComplete:
		content.WriteString(c.styles.FormTitle.Render("Cleanup Complete"))
		content.WriteString("\n\n")
		if c.deleted > 0 {
			content.WriteString(c.styles.Success.Render(fmt.Sprintf("Deleted %d releases", c.deleted)))
			content.WriteString("\n")
		}
		if c.deleteFailed > 0 {
			content.WriteString(c.styles.Error.Render(fmt.Sprintf("Failed to delete %d releases", c.deleteFailed)))
			content.WriteString("\n")
		}
		content.WriteString("\n")
		content.WriteString("Press " + c.styles.HelpKey.Render("Esc") + " to go back")
	}

	return lipgloss.NewStyle().
		Padding(1, 2).
		Render(content.String())
}

// countSelected returns the number of selected releases
func (c *CleanScreen) countSelected() int {
	count := 0
	for _, sel := range c.selected {
		if sel {
			count++
		}
	}
	return count
}

// updateListItems updates the list with current selection state
func (c *CleanScreen) updateListItems() {
	items := make([]list.Item, len(c.releases))
	for i, release := range c.releases {
		items[i] = ReleaseItem{
			release:  release,
			selected: c.selected[release.GetID()],
		}
	}
	c.list.SetItems(items)
}

// applyFilters applies filter criteria and pre-selects releases
func (c *CleanScreen) applyFilters() {
	now := time.Now()

	for i, release := range c.releases {
		shouldSelect := false

		// Filter by days
		if c.filterDays > 0 {
			created := release.GetCreatedAt()
			if !created.IsZero() {
				age := now.Sub(created.Time)
				if age.Hours()/24 > float64(c.filterDays) {
					shouldSelect = true
				}
			}
		}

		// Filter by count (keep N most recent)
		if c.filterCount > 0 && i >= c.filterCount {
			shouldSelect = true
		}

		if shouldSelect {
			c.selected[release.GetID()] = true
		}
	}
}

// loadReleases loads releases from GitHub asynchronously
func (c *CleanScreen) loadReleases() tea.Cmd {
	c.releasesChan = make(chan releasesLoadResult, 1)

	// Start fetch in background goroutine
	go func() {
		defer close(c.releasesChan)

		releases, err := c.client.GetReleases(c.ctx, c.owner, c.repo)
		c.releasesChan <- releasesLoadResult{releases: releases, err: err}
	}()

	// Return batch with spinner tick to keep UI responsive
	return tea.Batch(
		c.spinner.Tick,
		c.waitForReleases(),
	)
}

// waitForReleases waits for releases to be fetched
func (c *CleanScreen) waitForReleases() tea.Cmd {
	return func() tea.Msg {
		result := <-c.releasesChan
		return releasesLoadedMsg(result)
	}
}

// startDeletion starts the deletion process
func (c *CleanScreen) startDeletion() tea.Cmd {
	c.deleteIdx = 0
	return c.deleteNext()
}

// deleteNext deletes the next selected release asynchronously
func (c *CleanScreen) deleteNext() tea.Cmd {
	// Find next selected release
	idx := 0
	for _, release := range c.releases {
		if c.selected[release.GetID()] {
			if idx == c.deleteIdx {
				c.deleteChan = make(chan deleteResult, 1)

				// Start deletion in background goroutine
				releaseToDelete := release // capture for goroutine
				go func() {
					defer close(c.deleteChan)

					err := c.client.RemoveRelease(c.ctx, c.owner, c.repo, releaseToDelete)
					c.deleteChan <- deleteResult{err: err}
				}()

				// Return batch with spinner tick to keep UI responsive
				return tea.Batch(
					c.spinner.Tick,
					c.waitForDelete(),
				)
			}
			idx++
		}
	}
	return nil
}

// waitForDelete waits for deletion result
func (c *CleanScreen) waitForDelete() tea.Cmd {
	return func() tea.Msg {
		result := <-c.deleteChan
		return deleteProgressMsg(result)
	}
}

// Message types
type releasesLoadedMsg struct {
	releases []*gh.RepositoryRelease
	err      error
}

type deleteProgressMsg struct {
	err error
}

// Async result types for channel communication
type releasesLoadResult struct {
	releases []*gh.RepositoryRelease
	err      error
}

type deleteResult struct {
	err error
}
