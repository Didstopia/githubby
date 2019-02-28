// Package main provides the primary entrypoint for the application.
package main

import (
	"github.com/Didstopia/githubby/cmd"
	_ "github.com/joho/godotenv/autoload"
)

// The main function's sole purpose is to pass execution to the primary command
func main() {
	cmd.Execute()
}
