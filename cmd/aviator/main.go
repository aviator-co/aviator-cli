package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/config"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

var rootFlags struct {
	Debug bool
}

var rootCmd = &cobra.Command{
	Use:     "aviator",
	Short:   "Aviator CLI — submit verifications and create runbooks",
	Version: config.Version,

	// We handle error and usage printing ourselves.
	SilenceErrors: true,
	SilenceUsage:  true,

	CompletionOptions: cobra.CompletionOptions{
		HiddenDefaultCmd: true,
	},

	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		repoConfigDir := discoverRepoConfigDir(cmd.Context())
		if err := config.Load(repoConfigDir); err != nil {
			return errors.Wrap(err, "failed to load configuration")
		}
		return nil
	},
}

func init() {
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.PersistentFlags().BoolVar(
		&rootFlags.Debug, "debug", false, "enable verbose debug logging",
	)
	rootCmd.AddCommand(
		verifyCmd,
		runbookCmd,
		versionCmd,
	)
}

func main() {
	os.Exit(run())
}

func run() int {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%s %s\n", colors.Failure("error:"), err)
		return 1
	}
	return 0
}

// discoverRepoConfigDir best-effort locates a repo-local config dir
// (<git-common-dir>/aviator) so per-repo config can override the global one.
// Returns "" when not inside a git repository.
func discoverRepoConfigDir(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	dir, err := filepath.Abs(strings.TrimSpace(string(out)))
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "aviator")
}
