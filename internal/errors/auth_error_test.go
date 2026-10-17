package errors

import (
	"strings"
	"testing"
)

func TestNewAuthError(t *testing.T) {
	msg := NewAuthError().Error()
	for _, want := range []string{"not authenticated with GitHub", "githubby login", "--token", "GITHUB_TOKEN"} {
		if !strings.Contains(msg, want) {
			t.Errorf("auth error message missing %q: %s", want, msg)
		}
	}
}
