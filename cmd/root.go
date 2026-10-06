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
		Use:     "xgo",
		Short:   "xgo is a production-grade Go hot reloader",
		Version: Version,
	}

	root.PersistentFlags().StringVar(&opts.cfgFile, "config", "xgo.yaml", "Config file path")
	root.PersistentFlags().BoolVar(&opts.noColor, "no-color", false, "Disable colored output")
	root.PersistentFlags().BoolVar(&opts.timestamps, "timestamps", false, "Show timestamps in logs")

	runCmd := newRunCmd(opts)
	root.AddCommand(runCmd)
	root.AddCommand(newInitCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newDoctorCmd(opts))

	// Bare `xgo` means `xgo run` - the command users type every session
	// should not require a subcommand. Subcommands still win: cobra resolves
	// `xgo init` (and doctor/status/run) to their own commands before ever
	// reaching root's RunE. AddFlagSet shares the same *pflag.Flag pointers,
	// so both run's bound variables and its per-flag Changed() state are
	// shared between root's flag set and run's.
	root.RunE = runCmd.RunE
	root.Args = cobra.ArbitraryArgs
	root.Flags().AddFlagSet(runCmd.Flags())
	root.Flags().SetInterspersed(false)

	return root
}

// Execute runs the CLI.
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
