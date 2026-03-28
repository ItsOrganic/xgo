package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
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
	defer cleanupBinary()

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

func cleanupBinary() {
	_ = os.Remove(filepath.Join("tmp", "xgo-app"))
	_ = os.Remove(filepath.Join("tmp", "xgo-app.exe"))
}
