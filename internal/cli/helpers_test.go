package cli

import (
	"errors"
	"os"
	"path/filepath"

	synpkg "github.com/Didstopia/githubby/internal/sync"
)

// mkStateFile writes raw content to the state file under home.
func mkStateFile(home, content string) error {
	dir := filepath.Join(home, ".githubby")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "state.yaml"), []byte(content), 0o600)
}

func sampleResult() *synpkg.Result {
	r := synpkg.NewResult()
	r.Cloned = []string{"octo/a", "octo/b"}
	r.Updated = []string{"octo/c"}
	r.Skipped = []string{"octo/d"}
	r.Failed = map[string]error{"octo/bad": errors.New("boom")}
	r.Archived = []string{"octo/old"}
	return r
}
