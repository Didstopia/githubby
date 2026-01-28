// Package main provides the entry point for GitHubby
package main

import (
	"github.com/Didstopia/githubby/internal/cli"
	_ "github.com/joho/godotenv/autoload"
)

// Version information - set via ldflags at build time
// IMPORTANT: These must be uppercase to match ldflags -X main.Version=...
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func main() {
	// Set version info
	cli.SetVersionInfo(Version, Commit, BuildDate)

	// Run the CLI
	cli.Execute()
}
