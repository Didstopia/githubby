package util

import (
	"testing"
)

func TestValidateGitHubRepository(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "valid repository",
			input:     "owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantErr:   false,
		},
		{
			name:      "valid with dashes",
			input:     "my-org/my-repo-name",
			wantOwner: "my-org",
			wantRepo:  "my-repo-name",
			wantErr:   false,
		},
		{
			name:      "valid with underscores",
			input:     "my_org/my_repo",
			wantOwner: "my_org",
			wantRepo:  "my_repo",
			wantErr:   false,
		},
		{
			name:      "valid with numbers",
			input:     "org123/repo456",
			wantOwner: "org123",
			wantRepo:  "repo456",
			wantErr:   false,
		},
		{
			name:    "too short",
			input:   "a",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "no slash",
			input:   "noslash",
			wantErr: true,
		},
		{
			name:    "multiple slashes",
			input:   "owner/repo/extra",
			wantErr: true,
		},
		{
			name:    "https URL",
			input:   "https://github.com/owner/repo",
			wantErr: true,
		},
		{
			name:    "ssh URL",
			input:   "git://github.com/owner/repo",
			wantErr: true,
		},
		{
			name:    "github.com in string",
			input:   "github.com/owner/repo",
			wantErr: true,
		},
		{
			name:    "empty owner",
			input:   "/repo",
			wantErr: true,
		},
		{
			name:    "empty repo",
			input:   "owner/",
			wantErr: true,
		},
		{
			name:    "owner with @",
			input:   "owner@/repo",
			wantErr: true,
		},
		{
			name:    "repo with #",
			input:   "owner/repo#123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := ValidateGitHubRepository(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateGitHubRepository() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if owner != tt.wantOwner {
					t.Errorf("owner = %v, want %v", owner, tt.wantOwner)
				}
				if repo != tt.wantRepo {
					t.Errorf("repo = %v, want %v", repo, tt.wantRepo)
				}
			}
		})
	}
}

func TestParseRepositoryURL(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "HTTPS URL",
			input:     "https://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantErr:   false,
		},
		{
			name:      "HTTPS URL with .git",
			input:     "https://github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantErr:   false,
		},
		{
			name:      "HTTPS URL with trailing slash",
			input:     "https://github.com/owner/repo/",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantErr:   false,
		},
		{
			name:      "SSH URL",
			input:     "git@github.com:owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantErr:   false,
		},
		{
			name:      "SSH URL without .git",
			input:     "git@github.com:owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantErr:   false,
		},
		{
			name:    "non-GitHub URL",
			input:   "https://gitlab.com/owner/repo",
			wantErr: true,
		},
		{
			name:    "invalid URL",
			input:   "not-a-url",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := ParseRepositoryURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRepositoryURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if owner != tt.wantOwner {
					t.Errorf("owner = %v, want %v", owner, tt.wantOwner)
				}
				if repo != tt.wantRepo {
					t.Errorf("repo = %v, want %v", repo, tt.wantRepo)
				}
			}
		})
	}
}
