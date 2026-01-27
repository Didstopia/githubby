package cmd

import (
	"fmt"
	"os"
)

func logErrorAndExit(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
