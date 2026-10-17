package screens

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	gh "github.com/google/go-github/v86/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/github"
	"github.com/Didstopia/githubby/internal/tui"
)

// cl_press builds a key message from a name like "enter", "esc", "tab", "down" or a single rune.
func cl_press(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// cl_flatten runs a command and, for batches, every command inside it. Only use it on
// commands known to finish immediately (no tea.Tick).
func cl_flatten(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, cl_flatten(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// cl_find returns the first message of type T from a command.
func cl_find[T any](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	for _, m := range cl_flatten(cmd) {
		if v, ok := m.(T); ok {
			return v
		}
	}
	var zero T
	t.Fatalf("no %T among command results", zero)
	return zero
}

func cl_release(id int64, tag string, age time.Duration) *gh.RepositoryRelease {
	return &gh.RepositoryRelease{
		ID:        gh.Ptr(id),
		TagName:   gh.Ptr(tag),
		CreatedAt: &gh.Timestamp{Time: time.Now().Add(-age)},
	}
}

func cl_send(s *CleanScreen, msg tea.Msg) tea.Cmd {
	_, cmd := s.Update(msg)
	return cmd
}

func cl_type(s *CleanScreen, text string) {
	for _, r := range text {
		cl_send(s, cl_press(string(r)))
	}
}

func TestReleaseItem(t *testing.T) {
	t.Run("title shows selection and name", func(t *testing.T) {
		rel := &gh.RepositoryRelease{TagName: gh.Ptr("v1.0.0"), Name: gh.Ptr("First")}
		assert.Equal(t, "[ ] First (v1.0.0)", ReleaseItem{release: rel}.Title())
		assert.Equal(t, "[x] First (v1.0.0)", ReleaseItem{release: rel, selected: true}.Title())
	})
	t.Run("title falls back to the tag", func(t *testing.T) {
		same := &gh.RepositoryRelease{TagName: gh.Ptr("v1"), Name: gh.Ptr("v1")}
		assert.Equal(t, "[ ] v1", ReleaseItem{release: same}.Title())
		none := &gh.RepositoryRelease{TagName: gh.Ptr("v2")}
		assert.Equal(t, "[ ] v2", ReleaseItem{release: none}.Title())
		assert.Equal(t, "v2", ReleaseItem{release: none}.FilterValue())
	})
	t.Run("description", func(t *testing.T) {
		recent := cl_release(1, "a", 3*time.Hour)
		assert.Equal(t, "3h ago", ReleaseItem{release: recent}.Description())

		old := cl_release(2, "b", 48*time.Hour)
		old.Prerelease = gh.Ptr(true)
		old.Draft = gh.Ptr(true)
		assert.Equal(t, "2d ago | prerelease | draft", ReleaseItem{release: old}.Description())

		assert.Equal(t, "", ReleaseItem{release: &gh.RepositoryRelease{}}.Description())
	})
}

func TestCleanScreen_Basics(t *testing.T) {
	s := NewCleanScreen(context.Background(), github.NewMockClient())
	assert.Equal(t, "Clean Releases", s.Title())
	assert.NotNil(t, s.Init())
	assert.Len(t, s.ShortHelp(), 2)
	s.mode = cleanModeSelectReleases
	assert.Len(t, s.ShortHelp(), 5)

	s.SetRepository("octo", "cat")
	assert.Equal(t, "octo/cat", s.repoInput.Value())
	assert.Equal(t, "octo", s.owner)

	cl_send(s, tea.WindowSizeMsg{Width: 100, Height: 40})
	assert.Equal(t, 100, s.width)
	assert.Equal(t, 40, s.height)
}

func TestCleanScreen_RepoInput(t *testing.T) {
	t.Run("invalid format shows an error", func(t *testing.T) {
		s := NewCleanScreen(context.Background(), github.NewMockClient())
		cl_type(s, "justaname")
		cl_send(s, cl_press("enter"))
		assert.Equal(t, cleanModeRepoInput, s.mode)
		assert.Contains(t, s.View(), "invalid repository format")
	})
	t.Run("valid format moves to filters", func(t *testing.T) {
		s := NewCleanScreen(context.Background(), github.NewMockClient())
		cl_type(s, "octo/cat")
		cl_send(s, cl_press("enter"))
		assert.Equal(t, cleanModeFilterInput, s.mode)
		assert.Equal(t, "octo", s.owner)
		assert.Equal(t, "cat", s.repo)
		assert.Contains(t, s.View(), "Repository: octo/cat")
	})
	t.Run("esc pops the screen", func(t *testing.T) {
		s := NewCleanScreen(context.Background(), github.NewMockClient())
		cmd := cl_send(s, cl_press("esc"))
		assert.Equal(t, tui.ScreenPopMsg{}, cmd())
	})
	t.Run("view shows prompt", func(t *testing.T) {
		s := NewCleanScreen(context.Background(), github.NewMockClient())
		assert.Contains(t, s.View(), "Repository to clean:")
	})
}

func TestCleanScreen_FilterInput(t *testing.T) {
	newAtFilters := func() *CleanScreen {
		s := NewCleanScreen(context.Background(), github.NewMockClient())
		cl_type(s, "octo/cat")
		cl_send(s, cl_press("enter"))
		return s
	}

	t.Run("tab switches between the two fields", func(t *testing.T) {
		s := newAtFilters()
		assert.True(t, s.filterDaysInput.Focused())
		cl_send(s, cl_press("tab"))
		assert.True(t, s.filterCountInput.Focused())
		assert.False(t, s.filterDaysInput.Focused())
		cl_send(s, cl_press("tab"))
		assert.True(t, s.filterDaysInput.Focused())
	})
	t.Run("esc goes back to the repository", func(t *testing.T) {
		s := newAtFilters()
		cl_send(s, cl_press("esc"))
		assert.Equal(t, cleanModeRepoInput, s.mode)
	})
	t.Run("enter parses both filters and starts loading", func(t *testing.T) {
		mock := github.NewMockClient()
		mock.GetReleasesFunc = func(ctx context.Context, owner, repo string) ([]*gh.RepositoryRelease, error) {
			return []*gh.RepositoryRelease{cl_release(1, "v1", time.Hour)}, nil
		}
		s := NewCleanScreen(context.Background(), mock)
		cl_type(s, "octo/cat")
		cl_send(s, cl_press("enter"))
		cl_type(s, "30")
		cl_send(s, cl_press("tab"))
		cl_type(s, "5")

		cmd := cl_send(s, cl_press("enter"))
		assert.Equal(t, cleanModeLoadingReleases, s.mode)
		assert.True(t, s.loading)
		assert.Equal(t, 30, s.filterDays)
		assert.Equal(t, 5, s.filterCount)
		assert.Contains(t, s.View(), "Loading releases from octo/cat")

		loaded := cl_find[releasesLoadedMsg](t, cmd)
		require.NoError(t, loaded.err)
		require.Len(t, loaded.releases, 1)
		assert.Equal(t, []github.MockCall{{Method: "GetReleases", Args: []interface{}{"octo", "cat"}}}, mock.Calls)
	})
	t.Run("view shows the filter form", func(t *testing.T) {
		assert.Contains(t, newAtFilters().View(), "Filter Releases")
	})
}

func TestCleanScreen_ReleasesLoaded(t *testing.T) {
	t.Run("error returns to repo input", func(t *testing.T) {
		s := NewCleanScreen(context.Background(), github.NewMockClient())
		s.mode, s.loading = cleanModeLoadingReleases, true
		cl_send(s, releasesLoadedMsg{err: errors.New("api down")})
		assert.False(t, s.loading)
		assert.Equal(t, cleanModeRepoInput, s.mode)
		assert.Contains(t, s.View(), "api down")
	})
	t.Run("filters preselect old releases and beyond the keep count", func(t *testing.T) {
		s := NewCleanScreen(context.Background(), github.NewMockClient())
		s.filterDays, s.filterCount = 10, 2
		releases := []*gh.RepositoryRelease{
			cl_release(1, "new1", 24*time.Hour),
			cl_release(2, "new2", 48*time.Hour),
			cl_release(3, "new3", 72*time.Hour),       // beyond the keep count
			cl_release(4, "ancient", 20*24*time.Hour), // beyond the keep count and old
		}
		releases = append(releases, &gh.RepositoryRelease{ID: gh.Ptr(int64(5)), TagName: gh.Ptr("nodate")})
		s.filterCount = 100
		cl_send(s, releasesLoadedMsg{releases: releases})

		assert.Equal(t, cleanModeSelectReleases, s.mode)
		assert.Equal(t, map[int64]bool{4: true}, s.selected, "only the 20 day old release is past 10 days")
		assert.Contains(t, s.View(), "1 of 5 selected for deletion")

		s2 := NewCleanScreen(context.Background(), github.NewMockClient())
		s2.filterCount = 2
		cl_send(s2, releasesLoadedMsg{releases: releases})
		assert.Equal(t, map[int64]bool{3: true, 4: true, 5: true}, s2.selected)
	})
	t.Run("empty result", func(t *testing.T) {
		s := NewCleanScreen(context.Background(), github.NewMockClient())
		cl_send(s, releasesLoadedMsg{})
		assert.Contains(t, s.View(), "No releases found")
	})
	t.Run("leftover error is shown in the select view", func(t *testing.T) {
		s := NewCleanScreen(context.Background(), github.NewMockClient())
		s.mode = cleanModeSelectReleases
		s.err = errors.New("stale")
		assert.Contains(t, s.View(), "Error: stale")
	})
}

func cl_selectScreen(mock *github.MockClient) *CleanScreen {
	s := NewCleanScreen(context.Background(), mock)
	s.owner, s.repo = "octo", "cat"
	cl_send(s, releasesLoadedMsg{releases: []*gh.RepositoryRelease{
		cl_release(1, "v1", time.Hour),
		cl_release(2, "v2", time.Hour),
		cl_release(3, "v3", time.Hour),
	}})
	return s
}

func TestCleanScreen_Selection(t *testing.T) {
	s := cl_selectScreen(github.NewMockClient())

	cl_send(s, cl_press(" "))
	assert.Equal(t, map[int64]bool{1: true}, s.selected)
	cl_send(s, cl_press(" "))
	assert.False(t, s.selected[1], "space toggles back off")

	cl_send(s, cl_press("a"))
	assert.Equal(t, 3, s.countSelected())
	assert.Contains(t, s.View(), "3 of 3 selected")

	cl_send(s, cl_press("n"))
	assert.Equal(t, 0, s.countSelected())

	cl_send(s, cl_press("enter"))
	assert.Equal(t, cleanModeSelectReleases, s.mode, "enter with nothing selected does nothing")

	cl_send(s, cl_press("esc"))
	assert.Equal(t, cleanModeFilterInput, s.mode)
}

func TestCleanScreen_ConfirmAndDelete(t *testing.T) {
	var deleted []string
	mock := github.NewMockClient()
	mock.RemoveReleaseFunc = func(ctx context.Context, owner, repo string, r *gh.RepositoryRelease) error {
		deleted = append(deleted, r.GetTagName())
		if r.GetTagName() == "v2" {
			return errors.New("denied")
		}
		return nil
	}
	s := cl_selectScreen(mock)
	cl_send(s, cl_press("a"))
	cl_send(s, cl_press("enter"))
	require.Equal(t, cleanModeConfirmDelete, s.mode)

	view := s.View()
	assert.Contains(t, view, "delete 3 releases from octo/cat")
	assert.Contains(t, view, "v1")
	assert.Contains(t, view, "v3")

	// esc backs out to the selection
	cl_send(s, cl_press("esc"))
	assert.Equal(t, cleanModeSelectReleases, s.mode)
	cl_send(s, cl_press("enter"))

	cmd := cl_send(s, cl_press("y"))
	assert.Equal(t, cleanModeDeleting, s.mode)
	assert.Contains(t, s.View(), "Deleting releases... 0/3")

	for step := 0; step < 3; step++ {
		msg := cl_find[deleteProgressMsg](t, cmd)
		cmd = cl_send(s, msg)
	}
	assert.Nil(t, cl_flatten(cmd), "nothing left to delete")
	assert.Equal(t, []string{"v1", "v2", "v3"}, deleted)
	assert.Equal(t, 2, s.deleted)
	assert.Equal(t, 1, s.deleteFailed)
	assert.Equal(t, cleanModeComplete, s.mode)
	assert.True(t, s.complete)

	done := s.View()
	assert.Contains(t, done, "Deleted 2 releases")
	assert.Contains(t, done, "Failed to delete 1 releases")

	back := cl_send(s, cl_press("esc"))
	assert.Equal(t, tui.ScreenPopMsg{}, back())
}

func TestCleanScreen_DeletingView(t *testing.T) {
	s := NewCleanScreen(context.Background(), github.NewMockClient())
	s.mode = cleanModeDeleting
	s.deleted, s.deleteFailed = 2, 1
	v := s.View()
	assert.Contains(t, v, "2 deleted")
	assert.Contains(t, v, "1 failed")
}

func TestCleanScreen_ConfirmViewTruncatesLongLists(t *testing.T) {
	s := NewCleanScreen(context.Background(), github.NewMockClient())
	for i := int64(1); i <= 13; i++ {
		s.releases = append(s.releases, cl_release(i, "tag", time.Hour))
		s.selected[i] = true
	}
	s.mode = cleanModeConfirmDelete
	assert.Contains(t, s.View(), "... and 3 more")
}

func TestCleanScreen_SpinnerOnlyTicksWhileBusy(t *testing.T) {
	s := NewCleanScreen(context.Background(), github.NewMockClient())
	tick := s.spinner.Tick()
	assert.Nil(t, cl_send(s, tick), "idle screen ignores spinner ticks")

	s.loading = true
	assert.NotNil(t, cl_send(s, tick))
}

func TestCleanScreen_DeleteNextWithNothingSelected(t *testing.T) {
	s := NewCleanScreen(context.Background(), github.NewMockClient())
	assert.Nil(t, s.deleteNext())
	assert.Nil(t, s.startDeletion())
}

func TestCleanScreen_ProgressDoneWithNothingSelected(t *testing.T) {
	s := NewCleanScreen(context.Background(), github.NewMockClient())
	s.mode, s.deleting = cleanModeDeleting, true
	cl_send(s, deleteProgressMsg{})
	assert.Equal(t, cleanModeComplete, s.mode)
	assert.False(t, s.deleting)
}
