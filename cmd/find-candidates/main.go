package main

import (
	"os"

	"github.com/zorosz/workflow-hardener/internal/hardener"
)

func main() {
	os.Exit(hardener.RunCandidates(os.Args[1:], os.Getenv("GH_TOKEN"), os.Stdout, os.Stderr))
}
