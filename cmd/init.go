package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const starterConfig = `watch:
  dirs: [".", "./pkg", "./templates"]
  include: ["*.go", "*.html", "*.env"]
  exclude: ["*_test.go", "vendor/", "*.pb.go"]
  debounce: 50ms

build:
  # -s -w drop the symbol table and DWARF info the reload loop never reads:
  # ~20% faster builds and a ~30% smaller binary. Remove them if you need to
  # attach a debugger.
  cmd: "go build -ldflags=\"-s -w\" -o ./tmp/xgo-app ."
  env: ["CGO_ENABLED=0"]
  timeout: 30s

run:
  cmd: "./tmp/xgo-app"
  args: ["--port", "8080"]
  env: ["PORT=8080", "ENV=development"]
  before: ["go generate ./..."]
  after: []

extra_cmds:
  - cmd: "npm run watch"
    dir: "./frontend"

log:
  timestamps: true
  color: true
  prefix: "[xgo]"
`

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create starter xgo.yaml in current directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "xgo.yaml"
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists", path)
			}
			if err := os.WriteFile(path, []byte(starterConfig), 0o644); err != nil {
				return fmt.Errorf("write starter config: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "created xgo.yaml")
			return nil
		},
	}
}
