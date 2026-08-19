package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/adapter"
	"github.com/aviator-co/aviator-cli/internal/config"
	"github.com/aviator-co/aviator-cli/internal/git"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// pluginRepo is where /verify-submit comes from. init never checks whether it's
// installed; it just says where to get it.
const pluginRepo = "https://github.com/aviator-co/agent-plugins"

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
		"--scope team writes config into this repository for everyone to commit.\n" +
		"--scope self writes your own agent config, covering every repository on\n" +
		"this machine and leaving the working tree untouched.\n\n" +
		"Re-run any time to add agents, update, or reconcile.",
	Args: cobra.NoArgs,
	RunE: runInit,
}

func runInit(cmd *cobra.Command, _ []string) error {
	root, err := git.RepoRoot(cmd.Context())
	if err != nil {
		return err
	}

	agents, scope, err := gatherChoices()
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

	printScopeNote(scope, written)
	printPluginNote()
	printAuthNote()
	return nil
}

// installAgents writes each agent's hook and reports the files it touched.
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
		written = append(written, displayPath(root, path))
		fmt.Printf("%s %s hook — %s%s\n",
			colors.Success("✓"), a.DisplayName(), displayPath(root, path), changeNote(change))
		if note := a.SetupNote(); note != "" {
			fmt.Printf("  %s %s\n", colors.Faint("·"), note)
		}
	}
	return written, nil
}

// gatherChoices resolves scope and agents. Supplied flags win; the form only
// asks what a flag hasn't already answered.
func gatherChoices() ([]adapter.Adapter, adapter.Scope, error) {
	ask := interactive() && !initFlags.Yes

	scope := adapter.ScopeTeam
	switch {
	case initFlags.Scope != "":
		s, err := parseScope(initFlags.Scope)
		if err != nil {
			return nil, 0, err
		}
		scope = s
	case ask:
		if err := introAndAskScope(&scope); err != nil {
			return nil, 0, err
		}
	default:
		return nil, 0, errors.New("pass --scope team|self (nothing to prompt with)")
	}

	if initFlags.Agents != "" {
		agents, err := agentsFromFlag(initFlags.Agents)
		return agents, scope, err
	}
	if !ask {
		return defaultAgents(scope), scope, nil
	}
	agents, err := askAgents(scope)
	return agents, scope, err
}

func introAndAskScope(scope *adapter.Scope) error {
	fmt.Println("\nAviator Verify checks that your PRs do what they intend. This sets up your")
	fmt.Println("coding agents to capture that intent as you open a PR.")

	choice := "team"
	err := huh.NewSelect[string]().
		Title("Who is this for?").
		Description("Everyone: config committed to this repo.  Just me: your own agent\n"+
			"config, covering every repo on this machine.").
		Options(
			huh.NewOption("Everyone on this repo", "team"),
			huh.NewOption("Just me, everywhere on this machine", "self"),
		).
		Value(&choice).
		Run()
	if err != nil {
		return err
	}
	s, err := parseScope(choice)
	if err != nil {
		return err
	}
	*scope = s
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

	desc := "Detected on this machine"
	if scope == adapter.ScopeTeam {
		desc = "Your teammates may not use the same agent you do"
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

func printScopeNote(scope adapter.Scope, written []string) {
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
	fmt.Printf("%s Teammates also need the aviator CLI on their PATH — without it the hook\n",
		colors.Faint("·"))
	fmt.Printf("  does nothing at all. `brew install aviator-co/tap/aviator`\n")
}

func printPluginNote() {
	fmt.Printf("\n%s The hook points at /verify-submit, which ships in the Aviator agent\n",
		colors.Faint("·"))
	fmt.Printf("  plugin rather than this CLI. Install it from %s\n", pluginRepo)
}

func printAuthNote() {
	if config.Av.Aviator.APIToken == "" {
		fmt.Printf("\n%s Verify needs an API token — set AVIATOR_API_TOKEN so `aviator verify` works.\n",
			colors.Warning("!"))
	}
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
		"team (committed to this repo) or self (your config, every repo); prompts if omitted")
	initCmd.Flags().StringVar(&initFlags.Agents, "agents", "",
		"comma-separated agent ids (default: all supported for team, detected for self)")
	initCmd.Flags().BoolVar(&initFlags.Yes, "yes", false, "skip prompts (requires --scope)")
}
