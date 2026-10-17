package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewState(t *testing.T) {
	s := NewState()
	assert.Equal(t, 1, s.Version)
	assert.NotNil(t, s.Profiles)
	assert.Empty(t, s.Profiles)
	assert.NotNil(t, s.SyncHistory)
	assert.NotNil(t, s.RepoCache)
}

func TestNewProfileAndRecord(t *testing.T) {
	p := NewProfile("work", "org", "acme", "/tmp/x", true)
	assert.NotEmpty(t, p.ID)
	assert.Equal(t, "work", p.Name)
	assert.Equal(t, "org", p.Type)
	assert.Equal(t, "acme", p.Source)
	assert.Equal(t, "/tmp/x", p.TargetDir)
	assert.True(t, p.IncludePrivate)
	assert.False(t, p.CreatedAt.IsZero())
	assert.NotEqual(t, p.ID, NewProfile("work", "org", "acme", "/tmp/x", true).ID)

	r := NewSyncRecord(p.ID, p.Name)
	assert.Equal(t, p.ID, r.ProfileID)
	assert.Equal(t, "work", r.ProfileName)
	assert.False(t, r.StartedAt.IsZero())
	assert.NotNil(t, r.Results)
}

func TestState_ProfileCRUD(t *testing.T) {
	s := NewState()
	a := NewProfile("a", "user", "alice", "/a", false)
	b := NewProfile("b", "user", "bob", "/b", false)
	s.AddProfile(a)
	s.AddProfile(b)

	assert.Same(t, a, s.GetProfile(a.ID))
	assert.Nil(t, s.GetProfile("nope"))
	assert.Same(t, b, s.GetProfileByName("b"))
	assert.Nil(t, s.GetProfileByName("nope"))

	updated := *a
	updated.Name = "a2"
	assert.True(t, s.UpdateProfile(&updated))
	assert.Equal(t, "a2", s.GetProfile(a.ID).Name)
	assert.False(t, s.UpdateProfile(&SyncProfile{ID: "missing"}))

	assert.False(t, s.DeleteProfile("missing"))
	assert.Len(t, s.Profiles, 2)
}

func TestState_DeleteProfileRemovesItsHistory(t *testing.T) {
	s := NewState()
	a := NewProfile("a", "user", "alice", "/a", false)
	b := NewProfile("b", "user", "bob", "/b", false)
	s.AddProfile(a)
	s.AddProfile(b)
	s.AddSyncRecord(&SyncRecord{ProfileID: a.ID})
	s.AddSyncRecord(&SyncRecord{ProfileID: b.ID})
	s.AddSyncRecord(&SyncRecord{ProfileID: a.ID})

	assert.True(t, s.DeleteProfile(a.ID))
	assert.Len(t, s.Profiles, 1)
	require.Len(t, s.SyncHistory, 1)
	assert.Equal(t, b.ID, s.SyncHistory[0].ProfileID)
}

func TestState_SyncHistoryCappedAt100(t *testing.T) {
	s := NewState()
	for i := 0; i < 105; i++ {
		s.AddSyncRecord(&SyncRecord{TotalRepos: i})
	}
	require.Len(t, s.SyncHistory, 100)
	assert.Equal(t, 5, s.SyncHistory[0].TotalRepos, "oldest records are dropped")
	assert.Equal(t, 104, s.SyncHistory[99].TotalRepos)
}

func TestState_GetLatestSyncForProfile(t *testing.T) {
	s := NewState()
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	old := &SyncRecord{ProfileID: "p", CompletedAt: base}
	newer := &SyncRecord{ProfileID: "p", CompletedAt: base.Add(time.Hour)}
	other := &SyncRecord{ProfileID: "q", CompletedAt: base.Add(2 * time.Hour)}
	s.SyncHistory = []*SyncRecord{newer, old, other}

	assert.Same(t, newer, s.GetLatestSyncForProfile("p"))
	assert.Nil(t, s.GetLatestSyncForProfile("none"))
}

func TestState_GetSyncStats(t *testing.T) {
	s := NewState()
	assert.Equal(t, SyncStats{}, s.GetSyncStats())

	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	s.SyncHistory = []*SyncRecord{
		{Cloned: 1, Updated: 2, Skipped: 3, Failed: 4, Archived: 5, CompletedAt: base},
		{Cloned: 10, Updated: 20, Skipped: 30, Failed: 40, Archived: 50, CompletedAt: base.Add(time.Hour)},
	}
	got := s.GetSyncStats()
	assert.Equal(t, 2, got.TotalSyncs)
	assert.Equal(t, 11, got.TotalCloned)
	assert.Equal(t, 22, got.TotalUpdated)
	assert.Equal(t, 33, got.TotalSkipped)
	assert.Equal(t, 44, got.TotalFailed)
	assert.Equal(t, 55, got.TotalArchived)
	assert.Equal(t, base.Add(time.Hour), got.LastSync)
	assert.Equal(t, 33, got.TotalReposSynced())
}

func TestState_RepoCache(t *testing.T) {
	s := NewState()
	s.UpdateRepoCache(&CachedRepo{FullName: "a/b", Stars: 1})
	s.UpdateRepoCache(&CachedRepo{FullName: "c/d", Stars: 2})
	s.UpdateRepoCache(&CachedRepo{FullName: "a/b", Stars: 9})

	assert.Len(t, s.RepoCache, 2)
	assert.Equal(t, 9, s.GetCachedRepo("a/b").Stars)
	assert.Nil(t, s.GetCachedRepo("x/y"))
}

func TestSyncRecord_CompleteCountsStatuses(t *testing.T) {
	r := NewSyncRecord("p", "n")
	for _, st := range []string{"cloned", "cloned", "updated", "skipped", "failed", "archived", "weird"} {
		r.AddResult("o/"+st, st, "")
	}
	r.AddResult("o/err", "failed", "boom")
	r.Complete()

	assert.Equal(t, 8, r.TotalRepos)
	assert.Equal(t, 2, r.Cloned)
	assert.Equal(t, 1, r.Updated)
	assert.Equal(t, 1, r.Skipped)
	assert.Equal(t, 2, r.Failed)
	assert.Equal(t, 1, r.Archived)
	assert.False(t, r.CompletedAt.IsZero())
	assert.Equal(t, "boom", r.Results[7].Error)
}
