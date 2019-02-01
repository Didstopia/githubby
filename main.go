package main

import (
	"log"

	_ "github.com/joho/godotenv/autoload"

	"github.com/google/go-github/v21/github"
)

func main() {
	log.Println("Hi")

	client := github.NewClient(nil)
}
