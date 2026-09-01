package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/adapter"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/git"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// pluginRepo is where /verify-submit comes from. init never checks whether it's
// installed; it just says where to get it.
const pluginRepo = "https://github.com/aviator-co/agent-plugins"

const setupBranch = "aviator-setup"

var errNotInRepo = errors.Sentinel(
	"`aviator init` sets up a git repository, so run it from inside one")

var initFlags struct {
	Scope  string
	Agents string
	Yes    bool
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Set up your AI agents to capture intent for Aviator Verify before a PR",
	Long: "Set up AI coding agents to capture a change's intent and acceptance\n" +
		"criteria for Aviator Verify as you open a PR.\n\n" +
		"The agent config is written into this repository, so committing it sets\n" +
		"up your team too. --scope self writes your own agent config instead,\n" +
		"covering every repository on this machine and leaving the working tree\n" +
		"untouched.\n\n" +
		"Re-run any time to add agents, update, or reconcile.",
	Args: cobra.NoArgs,
	RunE: runInit,
}

func runInit(cmd *cobra.Command, _ []string) error {
	scope, err := parseScope(initFlags.Scope)
	if err != nil {
		return err
	}
	root, err := git.RepoRoot(cmd.Context())
	if err != nil && scope != adapter.ScopeSelf {
		return errNotInRepo
	}

	agents, err := gatherChoices(cmd.Context(), scope)
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("Cancelled — nothing changed.")
			return nil
		}
		return err
	}
	if len(agents) == 0 {
		fmt.Println("No agents selected — nothing to do.")
		return nil
	}

	fmt.Println()
	written, err := installAgents(agents, scope, root)
	if err != nil {
		return err
	}

	if scope == adapter.ScopeSelf {
		printScopeNote(scope, written, agents)
	} else {
		printCommitSteps(written, agents)
	}
	printPluginNote()
	printAuthNote()
	return nil
}

// installAgents writes each agent's hook and reports the files it changed.
func installAgents(agents []adapter.Adapter, scope adapter.Scope, root string) ([]string, error) {
	var written []string
	for _, a := range agents {
		path := a.HookFile(scope, root)
		if path == "" {
			fmt.Printf("%s %s — couldn't locate its config directory, skipped\n",
				colors.Warning("!"), a.DisplayName())
			continue
		}
		change, err := a.Install(scope, root)
		if err != nil {
			return nil, err
		}
		if change != adapter.ChangeNone {
			written = append(written, displayPath(root, path))
		}
		fmt.Printf("%s %s hook — %s%s\n",
			colors.Success("✓"), a.DisplayName(), displayPath(root, path), changeNote(change))
		if note := a.SetupNote(); note != "" {
			fmt.Printf("  %s %s\n", colors.Faint("·"), note)
		}
	}
	return written, nil
}

// gatherChoices signs the user in and resolves which agents to set up. Nothing
// has been written yet, so an abort here costs nothing.
func gatherChoices(ctx context.Context, scope adapter.Scope) ([]adapter.Adapter, error) {
	ask := interactive() && !initFlags.Yes
	if ask {
		fmt.Println("\nAviator Verify checks that your PRs do what they intend. This sets up your")
		fmt.Println("coding agents to capture that intent as you open a PR.")
		if err := offerLogin(ctx); err != nil {
			return nil, err
		}
	}

	if initFlags.Agents != "" {
		return agentsFromFlag(initFlags.Agents)
	}
	if !ask {
		return defaultAgents(scope), nil
	}
	return askAgents(scope)
}

// offerLogin gets credentials in place before anything is written. A failed
// sign-in only warns, since the setup itself still works.
func offerLogin(ctx context.Context) error {
	if api.HasCredentials() {
		return nil
	}
	signIn := true
	err := huh.NewConfirm().
		Title("Sign in to Aviator?").
		Description("Verify needs credentials before your agents can submit anything.").
		Value(&signIn).
		Run()
	if err != nil {
		return err
	}
	if !signIn {
		return nil
	}
	if err := runLogin(ctx); err != nil {
		fmt.Printf("%s sign-in failed: %v\n", colors.Warning("!"), err)
	}
	return nil
}

func askAgents(scope adapter.Scope) ([]adapter.Adapter, error) {
	here := map[string]bool{}
	for _, a := range adapter.Detected() {
		here[a.ID()] = true
	}
	all := adapter.All()
	byID := make(map[string]adapter.Adapter, len(all))
	opts := make([]huh.Option[string], 0, len(all))
	for _, a := range all {
		byID[a.ID()] = a
		label := a.DisplayName()
		if !here[a.ID()] {
			label += " (not on this machine)"
		}
		opts = append(opts, huh.NewOption(label, a.ID()).
			Selected(scope == adapter.ScopeTeam || here[a.ID()]))
	}

	desc := "This adds hooks to the repository. Select all agents that are used here."
	if scope == adapter.ScopeSelf {
		desc = "Detected on this machine"
	}

	var selected []string
	err := huh.NewMultiSelect[string]().
		Title("Set up which agents?").
		Description(desc).
		Options(opts...).
		Validate(func(s []string) error {
			if len(s) == 0 {
				return errors.New("select at least one agent")
			}
			return nil
		}).
		Value(&selected).
		Run()
	if err != nil {
		return nil, err
	}

	agents := make([]adapter.Adapter, 0, len(selected))
	for _, id := range selected {
		agents = append(agents, byID[id])
	}
	return agents, nil
}

// defaultAgents covers every supported agent for team scope, since what's
// installed on this machine says nothing about what teammates run.
func defaultAgents(scope adapter.Scope) []adapter.Adapter {
	if scope == adapter.ScopeTeam {
		return adapter.All()
	}
	return adapter.Detected()
}

func agentsFromFlag(list string) ([]adapter.Adapter, error) {
	var out []adapter.Adapter
	for id := range strings.SplitSeq(list, ",") {
		a := adapter.Find(strings.TrimSpace(id))
		if a == nil {
			return nil, errors.Errorf("unknown agent %q", id)
		}
		out = append(out, a)
	}
	return out, nil
}

func changeNote(c adapter.Change) string {
	switch c {
	case adapter.ChangeUpdated:
		return " " + colors.Faint("(updated)")
	case adapter.ChangeNone:
		return " " + colors.Faint("(already set up)")
	case adapter.ChangeAdded, adapter.ChangeRemoved:
		return ""
	}
	return ""
}

func printScopeNote(scope adapter.Scope, written []string, agents []adapter.Adapter) {
	if len(written) == 0 {
		return
	}
	if scope == adapter.ScopeSelf {
		fmt.Printf("\nSet up for you across every repository on this machine. Nothing was added\n" +
			"to this repo.\n")
		return
	}
	fmt.Printf("\nNew files in your repo: %s\n", strings.Join(written, ", "))
	fmt.Println("Commit them to share the setup with your team.")
	printTeammateNote(agents)
}

func printCommitSteps(written []string, agents []adapter.Adapter) {
	if len(written) == 0 {
		return
	}
	fmt.Println("\nCommit these so your team gets the setup:")
	fmt.Printf("\n  git switch -c %s\n", setupBranch)
	fmt.Printf("  git add %s\n", strings.Join(written, " "))
	fmt.Printf("  git commit -m %q\n", "set up aviator verify hooks")
	fmt.Printf("  git push -u origin %s\n", setupBranch)
	fmt.Println("\nThen open a PR.")
	printTeammateNote(agents)
}

// printTeammateNote covers what each teammate has to do for themselves. A
// SetupNote is one of those: the agents store hook trust per user, so committing
// the hook doesn't carry it.
func printTeammateNote(agents []adapter.Adapter) {
	fmt.Printf("\n%s Teammates each need the aviator CLI and a sign-in of their own — "+
		"without\n", colors.Faint("·"))
	fmt.Printf("  the CLI on their PATH the hook does nothing at all:\n")
	fmt.Printf("    brew trust aviator-co/tap  # Homebrew 6+\n")
	fmt.Printf("    brew install aviator-co/tap/aviator\n")
	fmt.Printf("    aviator login\n")
	for _, a := range agents {
		if note := a.SetupNote(); note != "" {
			fmt.Printf("\n%s Everyone using %s, not just you:\n",
				colors.Faint("·"), a.DisplayName())
			fmt.Printf("    %s\n", note)
		}
	}
}

func printPluginNote() {
	fmt.Printf("\n%s The hook points at /verify-submit, which ships in the Aviator agent\n",
		colors.Faint("·"))
	fmt.Printf("  plugin rather than this CLI. Install it from %s\n", pluginRepo)
}

func printAuthNote() {
	if api.HasCredentials() {
		return
	}
	fmt.Printf("\n%s Verify still needs credentials — run `aviator login`, "+
		"or set AVIATOR_API_TOKEN.\n", colors.Warning("!"))
}

func interactive() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}

// displayPath shows a repo path relative, and anything else ~-abbreviated.
func displayPath(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

func init() {
	initCmd.Flags().StringVar(&initFlags.Scope, "scope", "",
		"team (committed to this repo, the default) or self (your config, every repo)")
	initCmd.Flags().StringVar(&initFlags.Agents, "agents", "",
		"comma-separated agent ids (default: all supported for team, detected for self)")
	initCmd.Flags().BoolVar(&initFlags.Yes, "yes", false, "skip prompts")
}
