package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/stretchr/testify/assert"
)

func TestHeader(t *testing.T) {
	t.Run("defaults to unauthenticated", func(t *testing.T) {
		v := NewHeader().View()
		assert.Contains(t, v, "GitHubby")
		assert.Contains(t, v, "Not authenticated")
	})
	t.Run("shows title, subtitle and user", func(t *testing.T) {
		v := NewHeader().SetTitle("Sync").SetSubtitle("Wizard").SetAuth(true, "alice").SetWidth(100).View()
		assert.Contains(t, v, "Sync")
		assert.Contains(t, v, "| Wizard")
		assert.Contains(t, v, "alice")
		assert.NotContains(t, v, "Not authenticated")
	})
	t.Run("narrow width does not panic", func(t *testing.T) {
		assert.NotPanics(t, func() { NewHeader().SetWidth(5).View() })
	})
}

func TestFooter(t *testing.T) {
	enabled := key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "do thing"))
	disabled := key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "hidden thing"))
	disabled.SetEnabled(false)

	t.Run("renders enabled bindings and quit", func(t *testing.T) {
		v := NewFooter().SetBindings([]key.Binding{enabled, disabled}).SetWidth(100).View()
		assert.Contains(t, v, "do thing")
		assert.NotContains(t, v, "hidden thing")
		assert.Contains(t, v, "quit")
	})
	t.Run("quit can be hidden", func(t *testing.T) {
		v := NewFooter().SetShowQuit(false).View()
		assert.NotContains(t, v, "quit")
	})
}

func TestSimpleFooter(t *testing.T) {
	t.Run("renders items in order with quit", func(t *testing.T) {
		v := NewSimpleFooter().AddItem("enter", "select").AddItem("esc", "back").SetWidth(100).View()
		assert.Contains(t, v, "enter")
		assert.Contains(t, v, "select")
		assert.Less(t, strings.Index(v, "select"), strings.Index(v, "back"))
		assert.Contains(t, v, "quit")
	})
	t.Run("quit can be hidden", func(t *testing.T) {
		v := NewSimpleFooter().AddItem("x", "y").SetShowQuit(false).View()
		assert.NotContains(t, v, "quit")
	})
}

func TestProgressBar(t *testing.T) {
	t.Run("shows label and percentage", func(t *testing.T) {
		v := NewProgressBar().SetLabel("Cloning").SetProgress(1, 4).SetWidth(60).View()
		assert.Contains(t, v, "Cloning")
		assert.Contains(t, v, " 25%")
	})
	t.Run("zero total is 0 percent", func(t *testing.T) {
		v := NewProgressBar().SetProgress(5, 0).View()
		assert.Contains(t, v, "  0%")
	})
	t.Run("percentage can be hidden", func(t *testing.T) {
		shown := NewProgressBar().SetProgress(1, 2).View()
		hidden := NewProgressBar().SetProgress(1, 2).SetShowPct(false).View()
		assert.Less(t, strings.Count(hidden, "%"), strings.Count(shown, "%"))
	})
	t.Run("tiny width is clamped", func(t *testing.T) {
		p := NewProgressBar().SetWidth(5)
		assert.Equal(t, 10, p.bar.Width)
	})
}

func TestProgressList(t *testing.T) {
	l := NewProgressList().SetTitle("Repos").SetDimensions(80, 20)
	l.AddItem("pending", StatusPending, "")
	l.AddItem("running", StatusInProgress, "cloning")
	l.AddItem("ok", StatusSuccess, "")
	l.AddItem("warn", StatusWarning, "")
	l.AddItem("bad", StatusError, "boom")
	l.AddItem("skip", StatusSkipped, "")

	v := l.View()
	for _, want := range []string{"Repos", "pending", "running - cloning", "ok", "warn", "bad - boom", "skip", "○", "◐", "●"} {
		assert.Contains(t, v, want)
	}

	c := l.Counters()
	assert.Equal(t, 1, c[StatusPending])
	assert.Equal(t, 1, c[StatusError])

	s := l.Summary()
	assert.Contains(t, s, "1 done")
	assert.Contains(t, s, "1 failed")
	assert.Contains(t, s, "1 skipped")
	assert.Contains(t, s, "2 remaining")
}

func TestProgressList_UpdateAndClear(t *testing.T) {
	l := NewProgressList()
	l.AddItem("a", StatusPending, "")

	l.UpdateItem(0, StatusSuccess, "done")
	assert.Equal(t, StatusSuccess, l.Items[0].Status)
	assert.Equal(t, "done", l.Items[0].Detail)

	l.UpdateItem(5, StatusError, "ignored")
	l.UpdateItem(-1, StatusError, "ignored")
	assert.Equal(t, StatusSuccess, l.Items[0].Status)

	l.ClearItems()
	assert.Empty(t, l.Items)
	assert.Equal(t, "", l.Summary())
	assert.Equal(t, "", l.View())
}
