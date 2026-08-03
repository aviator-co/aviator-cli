package main

import (
	"fmt"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/adapter"
	"github.com/aviator-co/aviator-cli/internal/git"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

var hooksFlags struct {
	Agent string
	Scope string
}

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Manage the Aviator Verify pre-PR reminder hooks for AI agents",
}

var hooksRunCmd = &cobra.Command{
	Use:    "run",
	Short:  "Emit the pre-PR reminder for an agent (invoked by the installed hook)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		a, err := resolveAgent()
		if err != nil {
			return err
		}
		return a.EmitReminder(cmd.InOrStdin(), cmd.OutOrStdout())
	},
}

var hooksInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the pre-PR reminder hook for an agent in this repo",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		a, err := resolveAgent()
		if err != nil {
			return err
		}
		scope, err := parseScope(hooksFlags.Scope)
		if err != nil {
			return err
		}
		root, err := git.RepoRoot(cmd.Context())
		if err != nil {
			return err
		}
		change, err := a.Install(scope, root)
		if err != nil {
			return err
		}
		reportChange(a, change)
		return nil
	},
}

var hooksUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove the pre-PR reminder hook for an agent from this repo",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		a, err := resolveAgent()
		if err != nil {
			return err
		}
		scope, err := parseScope(hooksFlags.Scope)
		if err != nil {
			return err
		}
		root, err := git.RepoRoot(cmd.Context())
		if err != nil {
			return err
		}
		change, err := a.Uninstall(scope, root)
		if err != nil {
			return err
		}
		reportChange(a, change)
		return nil
	},
}

func resolveAgent() (adapter.Adapter, error) {
	if hooksFlags.Agent == "" {
		return nil, errors.New("--agent is required")
	}
	a := adapter.Find(hooksFlags.Agent)
	if a == nil {
		return nil, errors.Errorf("unknown agent %q", hooksFlags.Agent)
	}
	return a, nil
}

func parseScope(s string) (adapter.Scope, error) {
	switch s {
	case "", "team", "repo", "shared":
		return adapter.ScopeRepoShared, nil
	case "self", "local":
		return adapter.ScopeRepoLocal, nil
	default:
		return adapter.ScopeRepoShared, errors.Errorf("unknown scope %q (use team or self)", s)
	}
}

func reportChange(a adapter.Adapter, change adapter.Change) {
	var msg string
	switch change {
	case adapter.ChangeAdded:
		msg = "hook installed"
	case adapter.ChangeUpdated:
		msg = "hook updated"
	case adapter.ChangeRemoved:
		msg = "hook removed"
	case adapter.ChangeNone:
		msg = "already up to date"
	}
	fmt.Printf("%s %s: %s\n", colors.Success("✓"), a.DisplayName(), msg)
}

func init() {
	hooksCmd.PersistentFlags().StringVar(&hooksFlags.Agent, "agent", "", "agent id (e.g. claude)")
	hooksInstallCmd.Flags().StringVar(&hooksFlags.Scope, "scope", "team", "team (committed) or self (gitignored)")
	hooksUninstallCmd.Flags().StringVar(&hooksFlags.Scope, "scope", "team", "team (committed) or self (gitignored)")
	hooksCmd.AddCommand(hooksRunCmd, hooksInstallCmd, hooksUninstallCmd)
}
