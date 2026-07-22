package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	dedup "github.com/gustavosvalentim/miniflux-dedup"
)

const defaultTimeout = 30 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(
	ctx context.Context,
	args []string,
	getenv func(string) string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("miniflux-dedup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dryRun := flags.Bool("dry-run", false, "find and report duplicates without changing entries")
	date := flags.String("date", "", "calendar date to process in Miniflux user's timezone (YYYY-MM-DD)")
	timeout := flags.Duration("timeout", defaultTimeout, "timeout for each Miniflux HTTP request")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "miniflux-dedup: unexpected positional arguments")
		return 2
	}
	if *timeout <= 0 {
		_, _ = fmt.Fprintln(stderr, "miniflux-dedup: -timeout must be greater than zero")
		return 2
	}

	config, err := dedup.LoadConfig(getenv)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "miniflux-dedup: configuration: %v\n", err)
		return 1
	}
	client, err := dedup.NewClient(config, &http.Client{Timeout: *timeout})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "miniflux-dedup: configuration: %v\n", err)
		return 1
	}
	summary, err := dedup.Run(ctx, client, dedup.RunOptions{Date: *date, DryRun: *dryRun})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "miniflux-dedup: %v\n", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(summary); err != nil {
		_, _ = fmt.Fprintf(stderr, "miniflux-dedup: write summary: %v\n", err)
		return 1
	}
	return 0
}
