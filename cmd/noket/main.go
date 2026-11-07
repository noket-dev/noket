package main

import (
	"fmt"
	"os"

	"github.com/noket-dev/noket/internal/cli"
)

// version is set by goreleaser and the flake with -ldflags.
var version = "dev"

func main() {
	if err := cli.New(version).Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "noket: %v\n", err)
		os.Exit(1)
	}
}
