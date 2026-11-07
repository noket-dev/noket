package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"

	"github.com/noket-dev/noket/internal/noketd"
)

// version is set by goreleaser and the flake with -ldflags.
var version = "dev"

func main() {
	var (
		name    string
		verbose bool
	)
	flag.StringVar(&name, "name", "", "override the node name reported to the tunnel")
	flag.BoolVar(&verbose, "verbose", false, "enable debug logging")
	flag.Parse()

	level := zerolog.InfoLevel
	if verbose {
		level = zerolog.DebugLevel
	}
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
		Level(level).
		With().
		Timestamp().
		Logger()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := noketd.Run(ctx, noketd.RunOptions{Name: name, Managed: true, Version: version, Logger: logger}); err != nil {
		fmt.Fprintf(os.Stderr, "noketd: %v\n", err)
		os.Exit(1)
	}
}
