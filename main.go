// Package main provides the primary entrypoint for the application.
package main

import (
	"github.com/Didstopia/github-release-cleaner/cmd" // Include the primary command package
	_ "github.com/joho/godotenv/autoload"             // Automatically load environment variables from supported files (ie. from ".env" files)
)

// The main function's sole purpose is to pass execution to the primary command
func main() {
	cmd.Execute()
}
