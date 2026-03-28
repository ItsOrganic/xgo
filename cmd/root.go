package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

type rootOptions struct {
	cfgFile    string
	noColor    bool
	timestamps bool
}

// NewRootCmd returns the root cobra command.
func NewRootCmd() *cobra.Command {
	opts := &rootOptions{}
	root := &cobra.Command{
		Use:   "xgo",
		Short: "xgo is a production-grade Go hot reloader",
	}

	root.PersistentFlags().StringVar(&opts.cfgFile, "config", "xgo.yaml", "Config file path")
	root.PersistentFlags().BoolVar(&opts.noColor, "no-color", false, "Disable colored output")
	root.PersistentFlags().BoolVar(&opts.timestamps, "timestamps", false, "Show timestamps in logs")

	root.AddCommand(newRunCmd(opts))
	root.AddCommand(newInitCmd())
	root.AddCommand(newStatusCmd())
	return root
}

// Execute runs the CLI.
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
