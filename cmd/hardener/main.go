package main

import (
	"os"

	"github.com/zorosz/workflow-hardener-research/internal/hardener"
)

func main() {
	os.Exit(hardener.Run(os.Args[1:], os.Stdout, os.Stderr))
}
