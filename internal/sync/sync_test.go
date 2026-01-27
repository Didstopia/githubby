package sync

import (
	"testing"
)

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		input    string
		expected bool
	}{
		{
			name:     "wildcard matches all",
			pattern:  "*",
			input:    "anything",
			expected: true,
		},
		{
			name:     "prefix match",
			pattern:  "prefix*",
			input:    "prefix-something",
			expected: true,
		},
		{
			name:     "prefix no match",
			pattern:  "prefix*",
			input:    "other-something",
			expected: false,
		},
		{
			name:     "suffix match",
			pattern:  "*-suffix",
			input:    "something-suffix",
			expected: true,
		},
		{
			name:     "suffix no match",
			pattern:  "*-suffix",
			input:    "something-other",
			expected: false,
		},
		{
			name:     "exact match",
			pattern:  "exact",
			input:    "exact",
			expected: true,
		},
		{
			name:     "exact no match",
			pattern:  "exact",
			input:    "different",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchGlob(tt.pattern, tt.input)
			if result != tt.expected {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.input, result, tt.expected)
			}
		})
	}
}

func TestNewResult(t *testing.T) {
	result := NewResult()

	if result.Cloned == nil {
		t.Error("Cloned should be initialized")
	}
	if result.Updated == nil {
		t.Error("Updated should be initialized")
	}
	if result.Skipped == nil {
		t.Error("Skipped should be initialized")
	}
	if result.Failed == nil {
		t.Error("Failed should be initialized")
	}

	if len(result.Cloned) != 0 {
		t.Error("Cloned should be empty")
	}
	if len(result.Updated) != 0 {
		t.Error("Updated should be empty")
	}
	if len(result.Skipped) != 0 {
		t.Error("Skipped should be empty")
	}
	if len(result.Failed) != 0 {
		t.Error("Failed should be empty")
	}
}

func TestSyncer_shouldSync(t *testing.T) {
	tests := []struct {
		name     string
		opts     *Options
		repoName string
		expected bool
	}{
		{
			name:     "no filters - include all",
			opts:     &Options{},
			repoName: "any-repo",
			expected: true,
		},
		{
			name: "include pattern matches",
			opts: &Options{
				Include: []string{"my-*"},
			},
			repoName: "my-project",
			expected: true,
		},
		{
			name: "include pattern no match",
			opts: &Options{
				Include: []string{"my-*"},
			},
			repoName: "other-project",
			expected: false,
		},
		{
			name: "exclude pattern matches",
			opts: &Options{
				Exclude: []string{"*-archive"},
			},
			repoName: "old-archive",
			expected: false,
		},
		{
			name: "exclude pattern no match",
			opts: &Options{
				Exclude: []string{"*-archive"},
			},
			repoName: "my-project",
			expected: true,
		},
		{
			name: "exclude overrides include",
			opts: &Options{
				Include: []string{"my-*"},
				Exclude: []string{"my-old*"},
			},
			repoName: "my-old-project",
			expected: false,
		},
		{
			name: "both patterns - included",
			opts: &Options{
				Include: []string{"my-*"},
				Exclude: []string{"*-archive"},
			},
			repoName: "my-project",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncer := &Syncer{opts: tt.opts}
			result := syncer.shouldSync(tt.repoName)
			if result != tt.expected {
				t.Errorf("shouldSync(%q) = %v, want %v", tt.repoName, result, tt.expected)
			}
		})
	}
}
