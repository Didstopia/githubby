// Package main provides the entry point for GitHubby
package main

import (
	"github.com/Didstopia/githubby/internal/cli"
	_ "github.com/joho/godotenv/autoload"
)

// Version information - set via ldflags at build time
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	// Set version info
	cli.SetVersionInfo(version, commit, buildDate)

	// Run the CLI
	cli.Execute()
}
