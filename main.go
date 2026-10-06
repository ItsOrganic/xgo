package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ItsOrganic/xgo/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll("tmp", 0o755); err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("create tmp directory: %w", err))
		os.Exit(1)
	}

	// The compiled app is deliberately left in tmp/ on exit. Deleting it
	// used to cost every subsequent `xgo run` a full re-link, since
	// `go build -o X` only takes its fast path when X already exists:
	// measured 630ms cold start with the deletion vs 234ms without it, on
	// benchmark/testapps/minimal. tmp/ is gitignored (see
	// ensureTmpGitignore), so there's nothing to protect the repo from here.
	root := cmd.NewRootCmd()
	root.SetContext(ctx)
	if err := root.Execute(); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
