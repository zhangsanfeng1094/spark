package main

import (
	"fmt"
	"os"

	"spark/internal/app"
)

func main() {
	if err := app.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
