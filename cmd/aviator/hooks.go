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
	Agent  string
	Agents string
	Scope  string
}

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Manage the Aviator Verify pre-PR reminder hooks for AI agents",
}

// Both callbacks run inside an agent's loop and need neither config nor the
// repo, so they skip the root command's loading.
var noSetup = func(*cobra.Command, []string) error { return nil }

var hooksSessionStartCmd = &cobra.Command{
	Use:               "session-start",
	Short:             "Emit the standing Verify instruction (invoked by the installed hook)",
	Hidden:            true,
	Args:              cobra.NoArgs,
	PersistentPreRunE: noSetup,
	RunE: func(cmd *cobra.Command, _ []string) error {
		a, err := resolveAgent()
		if err != nil {
			return err
		}
		return a.EmitSessionStart(cmd.OutOrStdout())
	},
}

var hooksPostToolUseCmd = &cobra.Command{
	Use:               "post-tool-use",
	Short:             "Emit the post-commit Verify directive (invoked by the installed hook)",
	Hidden:            true,
	Args:              cobra.NoArgs,
	PersistentPreRunE: noSetup,
	RunE: func(cmd *cobra.Command, _ []string) error {
		a, err := resolveAgent()
		if err != nil {
			return err
		}
		return a.EmitPostToolUse(cmd.InOrStdin(), cmd.OutOrStdout())
	},
}

var hooksPreToolUseCmd = &cobra.Command{
	Use:               "pre-tool-use",
	Short:             "Emit the pre-PR reminder for an agent (invoked by the installed hook)",
	Hidden:            true,
	Args:              cobra.NoArgs,
	PersistentPreRunE: noSetup,
	RunE: func(cmd *cobra.Command, _ []string) error {
		a, err := resolveAgent()
		if err != nil {
			return err
		}
		return a.EmitPreToolUse(cmd.InOrStdin(), cmd.OutOrStdout())
	},
}

// hooksInstallCmd is `aviator init` without the questions.
var hooksInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the pre-PR reminder hooks",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := git.RepoRoot(cmd.Context())
		if err != nil {
			return err
		}
		if hooksFlags.Scope == "" {
			return errors.New("pass --scope team|self")
		}
		scope, err := parseScope(hooksFlags.Scope)
		if err != nil {
			return err
		}
		agents := defaultAgents(scope)
		if hooksFlags.Agents != "" {
			if agents, err = agentsFromFlag(hooksFlags.Agents); err != nil {
				return err
			}
		}
		if len(agents) == 0 {
			fmt.Println("No agents to set up.")
			return nil
		}
		written, err := installAgents(agents, scope, root)
		if err != nil {
			return err
		}
		printScopeNote(scope, written)
		return nil
	},
}

var hooksUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove the pre-PR reminder hooks",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := git.RepoRoot(cmd.Context())
		if err != nil {
			return err
		}
		scopes, err := scopesToClear()
		if err != nil {
			return err
		}
		removed := 0
		for _, a := range adapter.All() {
			for _, scope := range scopes {
				path := a.HookFile(scope, root)
				if path == "" {
					continue
				}
				change, err := a.Uninstall(scope, root)
				if err != nil {
					return err
				}
				if change == adapter.ChangeRemoved {
					removed++
					fmt.Printf("%s %s hook removed from %s\n",
						colors.Success("✓"), a.DisplayName(), displayPath(root, path))
				}
			}
		}
		if removed == 0 {
			fmt.Println("No Aviator hooks to remove.")
		}
		return nil
	},
}

// scopesToClear defaults to both, since self scope now lives outside the repo
// and a bare uninstall that silently skipped it was the old bug.
func scopesToClear() ([]adapter.Scope, error) {
	if hooksFlags.Scope == "" {
		return []adapter.Scope{adapter.ScopeTeam, adapter.ScopeSelf}, nil
	}
	scope, err := parseScope(hooksFlags.Scope)
	if err != nil {
		return nil, err
	}
	return []adapter.Scope{scope}, nil
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
	case "", "team":
		return adapter.ScopeTeam, nil
	case "self":
		return adapter.ScopeSelf, nil
	default:
		return adapter.ScopeTeam, errors.Errorf("unknown scope %q (use team or self)", s)
	}
}

func init() {
	for _, c := range []*cobra.Command{hooksSessionStartCmd, hooksPostToolUseCmd, hooksPreToolUseCmd} {
		c.Flags().StringVar(&hooksFlags.Agent, "agent", "", "agent id (e.g. claude)")
	}
	hooksInstallCmd.Flags().StringVar(&hooksFlags.Scope, "scope", "",
		"team (committed to this repo) or self (your config, every repo)")
	hooksInstallCmd.Flags().StringVar(&hooksFlags.Agents, "agents", "",
		"comma-separated agent ids (default: all supported for team, detected for self)")
	hooksUninstallCmd.Flags().StringVar(&hooksFlags.Scope, "scope", "",
		"team or self; both are cleared if omitted")
	hooksCmd.AddCommand(hooksSessionStartCmd, hooksPostToolUseCmd, hooksPreToolUseCmd,
		hooksInstallCmd, hooksUninstallCmd)
}
