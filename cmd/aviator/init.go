package main

import (
	"fmt"
	"os"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/adapter"
	"github.com/aviator-co/aviator-cli/internal/config"
	"github.com/aviator-co/aviator-cli/internal/git"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

var initFlags struct {
	Scope string
	Yes   bool
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Set up Aviator Verify pre-PR reminders for your AI agents in this repo",
	Long: "Detect the AI coding agents on this machine and install a pre-PR reminder\n" +
		"that nudges them to submit intent + acceptance criteria to Aviator Verify\n" +
		"before opening a pull request. Re-run any time to update or reconcile.",
	Args: cobra.NoArgs,
	RunE: runInit,
}

func runInit(cmd *cobra.Command, _ []string) error {
	root, err := git.RepoRoot(cmd.Context())
	if err != nil {
		return err
	}

	agents := adapter.Detected()
	if len(agents) == 0 {
		fmt.Printf("%s No supported AI agents detected.\n", colors.Warning("!"))
		return nil
	}

	scope, err := resolveInitScope()
	if err != nil {
		return err
	}

	fmt.Printf("Aviator Verify reminders (%s):\n", scopeLabel(scope))
	for _, a := range agents {
		st, err := a.Status(scope, root)
		if err != nil {
			return err
		}
		fmt.Printf("  %s %s — %s\n", colors.Faint("•"), a.DisplayName(), stateLabel(st))
	}

	proceed, err := confirmInit()
	if err != nil {
		return err
	}
	if !proceed {
		fmt.Println("Nothing changed.")
		return nil
	}

	fmt.Println()
	for _, a := range agents {
		change, err := a.Install(scope, root)
		if err != nil {
			return err
		}
		reportChange(a, change)
	}

	if config.Av.Aviator.APIToken == "" {
		fmt.Printf("\n%s Verify needs an API token — set AVIATOR_API_TOKEN so `aviator verify` works.\n",
			colors.Warning("!"))
	}
	return nil
}

// resolveInitScope returns the scope from --scope, or prompts for it when
// interactive. Without a flag and without a terminal it errors rather than
// guessing where a team-wide vs personal change should land.
func resolveInitScope() (adapter.Scope, error) {
	if initFlags.Scope != "" {
		return parseScope(initFlags.Scope)
	}
	if !interactive() {
		return adapter.ScopeRepoShared, errors.New("not a terminal: pass --scope team|self")
	}
	var choice string
	err := huh.NewSelect[string]().
		Title("Who are these reminders for?").
		Options(
			huh.NewOption("The whole team (committed to the repo)", "team"),
			huh.NewOption("Just me (gitignored)", "self"),
		).
		Value(&choice).
		Run()
	if err != nil {
		return adapter.ScopeRepoShared, err
	}
	return parseScope(choice)
}

// confirmInit asks whether to apply the changes. It auto-proceeds with --yes or
// when not interactive (the scope was given explicitly by then).
func confirmInit() (bool, error) {
	if initFlags.Yes || !interactive() {
		return true, nil
	}
	ok := true
	if err := huh.NewConfirm().Title("Apply these changes?").Value(&ok).Run(); err != nil {
		return false, err
	}
	return ok, nil
}

func interactive() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}

func scopeLabel(s adapter.Scope) string {
	if s == adapter.ScopeRepoLocal {
		return "just me, gitignored"
	}
	return "team, committed"
}

func stateLabel(st adapter.Status) string {
	switch {
	case st.Installed && st.Stale:
		return "installed, will update"
	case st.Installed:
		return "already installed"
	default:
		return "will install"
	}
}

func init() {
	initCmd.Flags().StringVar(&initFlags.Scope, "scope", "",
		"team (committed) or self (gitignored); prompts if omitted")
	initCmd.Flags().BoolVar(&initFlags.Yes, "yes", false, "skip the confirmation prompt")
}
