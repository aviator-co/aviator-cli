package main

import (
	"fmt"
	"strings"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

// Stacked Verify is behind a per-user allowlist, so these commands stay out of
// help and completion while it is; they run when called by name.

var verifyAddFlags struct {
	PRs             []int
	Criteria        []string
	CriteriaFile    string
	ExpectedVersion int
	Intent          string
	New             bool
	From            string
	Repo            string
	JSON            bool
}

var verifyAddCmd = &cobra.Command{
	Use: "add <id> --pr <number> [--pr <number>] --intent <intent> --criteria-file <file> --expected-version <version>\n" +
		"  aviator verify add --new --repo <owner/repo> --pr <number> [--pr <number>] --intent <intent> " +
		"--criteria-file <file> [--from <id>]",
	Short:  "Add PRs to a draft stacked review, or open a new review from PRs (e.g. aviator verify add r/42 --pr 102 ...)",
	Hidden: true,
	Long: "Add PRs to a draft stacked review and replace its intent and acceptance\n" +
		"criteria. --pr is repeatable, in any order.\n" +
		"\n" +
		"The intent and criteria cover EVERY PR in the review, not just the new\n" +
		"one: read the current ones with `aviator show`, fold in the new PR, rewrite\n" +
		"any criteria that conflict, duplicate, or got refined, and send them all.\n" +
		"Without --intent the intent stays as it was. --expected-version is\n" +
		"the version `aviator show` printed; the add is refused if the criteria\n" +
		"changed since.\n" +
		"\n" +
		"With --new there is no <id>: it opens a new review holding the --pr PRs\n" +
		"(repeatable, any order) with this intent and criteria. The new review is\n" +
		"ready, not a draft, and starts no run; run it with `aviator verify r/<n>`.\n" +
		"--from links it to the review the PRs were just removed from with\n" +
		"`aviator verify remove`.",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if verifyAddFlags.New {
			if len(args) > 0 {
				return errors.New("--new opens a new review, so it takes no <id>; use --from to link the review the PRs came from")
			}
			return runVerifyAddNew(cmd)
		}
		if len(args) != 1 {
			return errors.New("add needs the review id, e.g. aviator verify add r/42 --pr 102 ... (or --new for a new review)")
		}
		for _, name := range []string{"from", "repo"} {
			if cmd.Flags().Changed(name) {
				return errors.Errorf("--%s only applies with --new", name)
			}
		}
		return runVerifyAdd(cmd, args[0])
	},
}

func runVerifyAdd(cmd *cobra.Command, arg string) error {
	runbookNumber, err := parseRunbookID(arg)
	if err != nil {
		return err
	}
	f := &verifyAddFlags
	prs, err := validPRNumbers(f.PRs)
	if err != nil {
		return err
	}
	if !cmd.Flags().Changed("expected-version") {
		return errors.New("--expected-version is required, the version `aviator show` prints")
	}
	criteria, err := collectCriteria(f.Criteria, f.CriteriaFile)
	if err != nil {
		return err
	}
	if len(criteria) == 0 {
		return errors.New("--criteria-file (or --criteria) is required, with the full list for every PR")
	}
	client, err := api.NewClient()
	if err != nil {
		return err
	}
	resp, err := client.AddVerifyPR(cmd.Context(), runbookNumber, api.AddVerifyPRRequest{
		PRNumbers: prs, Criteria: criteria, BaseVersion: f.ExpectedVersion, Intent: f.Intent,
	})
	if err != nil {
		return stackedVerifyError(err, runbookNumber, f.ExpectedVersion)
	}
	if f.JSON {
		return printJSON(newStackedSessionJSON(resp))
	}
	id := formatRunbookID(resp.RunbookNumber)
	fmt.Printf("%s %s added to %s%s\n", colors.Success("✓"), formatPRList(prs), id, formatVersion(resp.Version))
	fmt.Print(formatMembers(resp.Members))
	fmt.Printf("  %s\n", colors.Faint("Once the user agrees to the grouping: aviator verify ready "+id))
	return nil
}

func runVerifyAddNew(cmd *cobra.Command) error {
	f := &verifyAddFlags
	if cmd.Flags().Changed("expected-version") {
		return errors.New("--expected-version doesn't apply with --new: the review is new")
	}
	if f.Repo == "" {
		return errors.New("--repo is required with --new")
	}
	repo, err := parseRepo(f.Repo)
	if err != nil {
		return err
	}
	prs, err := validPRNumbers(f.PRs)
	if err != nil {
		return err
	}
	intent := strings.TrimSpace(f.Intent)
	if intent == "" {
		return errors.New("--intent is required with --new")
	}
	criteria, err := collectCriteria(f.Criteria, f.CriteriaFile)
	if err != nil {
		return err
	}
	if len(criteria) == 0 {
		return errors.New("--criteria-file (or --criteria) is required, with the full list for these PRs")
	}
	req := api.NewStackedVerifyRequest{Repository: repo, PRNumbers: prs, Intent: intent, Criteria: criteria}
	if f.From != "" {
		from, err := parseRunbookID(f.From)
		if err != nil {
			return err
		}
		req.FromRunbookNumber = &from
	}
	client, err := api.NewClient()
	if err != nil {
		return err
	}
	resp, err := client.NewStackedVerify(cmd.Context(), req)
	if err != nil {
		return stackedVerifyError(err, 0, 0)
	}
	if f.JSON {
		return printJSON(newStackedSessionJSON(resp))
	}
	id := formatRunbookID(resp.RunbookNumber)
	fmt.Printf("%s %s opened with %s — %s\n", colors.Success("✓"), id, formatPRList(prs), resp.URL)
	fmt.Print(formatMembers(resp.Members))
	fmt.Printf("  %s\n", colors.Faint("It's ready and hasn't run: aviator verify "+id))
	return nil
}

var verifyRemoveFlags struct {
	PRs             []int
	Criteria        []string
	CriteriaFile    string
	ExpectedVersion int
	Intent          string
	JSON            bool
}

var verifyRemoveCmd = &cobra.Command{
	Use:    "remove <id> --pr <number> [--pr <number>] --intent <intent> --criteria-file <file> --expected-version <version>",
	Short:  "Remove PRs from a stacked review (e.g. aviator verify remove r/42 --pr 103 ...)",
	Hidden: true,
	Long: "Remove PRs from a stacked review, draft or ready, and replace its intent\n" +
		"and acceptance criteria. The review stays open with the PRs that are left,\n" +
		"which must still form one restacked stack, and keeps its decisions. Any\n" +
		"verification running on it is cancelled.\n" +
		"\n" +
		"The criteria cover the PRs that STAY: read the current ones with\n" +
		"`aviator show`, drop what only the removed PRs did, and send the full\n" +
		"list. Without --intent the intent stays as it was. --expected-version is the\n" +
		"version `aviator show` printed.\n" +
		"\n" +
		"The removed PRs' checks stay pending until they're in a review again:\n" +
		"`aviator verify add --new --repo <owner/repo> --pr ... --intent ... --criteria-file ... --from <id>`\n" +
		"opens one for them.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		f := &verifyRemoveFlags
		prs, err := validPRNumbers(f.PRs)
		if err != nil {
			return err
		}
		if !cmd.Flags().Changed("expected-version") {
			return errors.New("--expected-version is required, the version `aviator show` prints")
		}
		criteria, err := collectCriteria(f.Criteria, f.CriteriaFile)
		if err != nil {
			return err
		}
		if len(criteria) == 0 {
			return errors.New("--criteria-file (or --criteria) is required, with the full list for the PRs that stay")
		}
		client, err := api.NewClient()
		if err != nil {
			return err
		}
		resp, err := client.RemoveVerifyPRs(cmd.Context(), runbookNumber, api.RemoveVerifyPRsRequest{
			PRNumbers:   prs,
			Criteria:    criteria,
			BaseVersion: f.ExpectedVersion,
			Intent:      f.Intent,
		})
		if err != nil {
			// No base version: the PRs may already be out, so a retry hint would mislead.
			return stackedVerifyError(err, runbookNumber, 0)
		}
		if f.JSON {
			return printJSON(newStackedSessionJSON(resp))
		}
		id := formatRunbookID(resp.RunbookNumber)
		fmt.Printf("%s %s removed from %s%s\n", colors.Success("✓"), formatPRList(prs), id, formatVersion(resp.Version))
		fmt.Print(formatMembers(resp.Members))
		fmt.Printf("  %s\n", colors.Faint("Their checks stay pending until they're in a review: aviator verify add --new "+
			"--repo <owner/repo> "+formatPRFlags(prs)+" --intent ... --criteria-file ... --from "+id))
		return nil
	},
}

var verifyReadyFlags struct {
	JSON bool
}

var verifyReadyCmd = &cobra.Command{
	Use:    "ready <id>",
	Short:  "Mark a draft stacked review ready",
	Hidden: true,
	Long: "Mark a draft stacked review ready after the user agrees to its grouping.\n" +
		"The review becomes active; no PRs can be added after this. It starts\n" +
		"no run: Verify runs on the top PR, against every PR's criteria, when a\n" +
		"PR is marked ready for review, approved, or queued.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		client, err := api.NewClient()
		if err != nil {
			return err
		}
		resp, err := client.MarkVerifyReady(cmd.Context(), runbookNumber)
		if err != nil {
			return stackedVerifyError(err, runbookNumber, 0)
		}
		if verifyReadyFlags.JSON {
			return printJSON(newStackedSessionJSON(resp))
		}
		id := formatRunbookID(resp.RunbookNumber)
		fmt.Printf("%s %s marked ready\n", colors.Success("✓"), id)
		fmt.Print(formatMembers(resp.Members))
		return nil
	},
}

// validPRNumbers checks a repeatable --pr list: at least one, all positive,
// no repeats.
func validPRNumbers(prs []int) ([]int, error) {
	if len(prs) == 0 {
		return nil, errors.New("--pr is required (repeatable)")
	}
	seen := make(map[int]bool, len(prs))
	for _, n := range prs {
		if n <= 0 {
			return nil, errors.Errorf("invalid --pr %d", n)
		}
		if seen[n] {
			return nil, errors.Errorf("--pr %d is given twice", n)
		}
		seen[n] = true
	}
	return prs, nil
}

func formatPRList(prs []int) string {
	parts := make([]string, 0, len(prs))
	for _, n := range prs {
		parts = append(parts, fmt.Sprintf("#%d", n))
	}
	return strings.Join(parts, ", ")
}

func formatPRFlags(prs []int) string {
	parts := make([]string, 0, len(prs))
	for _, n := range prs {
		parts = append(parts, fmt.Sprintf("--pr %d", n))
	}
	return strings.Join(parts, " ")
}

// stackedVerifyError turns the stacked-session refusals into the next step to
// take. baseVersion is the version an add was based on, 0 otherwise.
func stackedVerifyError(err error, runbookNumber, baseVersion int) error {
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	// runbookNumber is 0 for add --new, which has no review yet.
	id := formatRunbookID(runbookNumber)
	stackOf := "the stack of " + id
	if runbookNumber == 0 {
		stackOf = "the stack"
	}
	switch apiErr.Code {
	case "session-draft":
		// The server says how this draft ends: `verify ready` or submitted evidence.
		return errors.New(apiErr.Message)
	case "stale-runbook-version":
		if baseVersion > 0 {
			return errors.Errorf("the criteria of %s changed since version %d; re-read them with "+
				"`aviator show %s`, rewrite the full list, and retry with the new --expected-version: %s",
				id, baseVersion, id, apiErr.Message)
		}
	case "stack-invalid":
		return errors.Errorf("%s isn't valid: %s", stackOf, apiErr.Message)
	case "stack-check-unavailable":
		return errors.Errorf("GitHub couldn't check %s; retry in a minute: %s", stackOf, apiErr.Message)
	}
	return err
}

// formatMembers lists a stacked session's PRs, bottom first, or "" for none.
func formatMembers(members []api.SessionMember) string {
	if len(members) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("  Members:\n")
	for _, m := range members {
		sha := ""
		if s := shortSHA(deref(m.HeadSHA)); s != "" {
			sha = " " + s
		}
		fmt.Fprintf(&b, "    %d. #%d%s\n", m.Position, m.PRNumber, sha)
	}
	return b.String()
}

type memberJSON struct {
	PRNumber int     `json:"pr_number"`
	Position int     `json:"position"`
	HeadSHA  *string `json:"head_sha"`
}

func newMembersJSON(members []api.SessionMember) []memberJSON {
	out := make([]memberJSON, 0, len(members))
	for _, m := range members {
		out = append(out, memberJSON(m))
	}
	return out
}

type stackedSessionJSON struct {
	sessionRef
	State   *string      `json:"state"`
	Version *int         `json:"version"`
	Members []memberJSON `json:"members"`
}

func newStackedSessionJSON(s *api.StackedVerifySession) stackedSessionJSON {
	return stackedSessionJSON{
		sessionRef: newSessionRef(s.RunbookNumber, s.URL),
		State:      s.State,
		Version:    s.Version,
		Members:    newMembersJSON(s.Members),
	}
}

func init() {
	af := verifyAddCmd.Flags()
	registerCriteriaFlags(verifyAddCmd, &verifyAddFlags.Criteria, &verifyAddFlags.CriteriaFile)
	af.IntSliceVar(&verifyAddFlags.PRs, "pr", nil, "number of a PR to add (repeatable)")
	af.StringVar(&verifyAddFlags.Intent, "intent", "", "new intent covering every PR in the review, the new one included")
	af.IntVar(&verifyAddFlags.ExpectedVersion, "expected-version", 0,
		"criteria version the new list is based on, as aviator show prints it")
	af.BoolVar(&verifyAddFlags.New, "new", false, "open a new ready review holding the --pr PRs instead of adding to one")
	af.StringVar(&verifyAddFlags.From, "from", "", "with --new, the review the PRs were removed from (e.g. r/42)")
	af.StringVar(&verifyAddFlags.Repo, "repo", "", "with --new, the repository as owner/repo")
	af.BoolVar(&verifyAddFlags.JSON, "json", false, "print the result as a single JSON object instead of the human summary")

	rmf := verifyRemoveCmd.Flags()
	registerCriteriaFlags(verifyRemoveCmd, &verifyRemoveFlags.Criteria, &verifyRemoveFlags.CriteriaFile)
	rmf.IntSliceVar(&verifyRemoveFlags.PRs, "pr", nil, "number of a PR to remove (repeatable)")
	rmf.StringVar(&verifyRemoveFlags.Intent, "intent", "", "new intent covering the PRs that stay")
	rmf.IntVar(&verifyRemoveFlags.ExpectedVersion, "expected-version", 0,
		"criteria version the new list is based on, as aviator show prints it")
	rmf.BoolVar(&verifyRemoveFlags.JSON, "json", false, "print the result as a single JSON object instead of the human summary")

	cf := verifyReadyCmd.Flags()
	cf.BoolVar(&verifyReadyFlags.JSON, "json", false, "print the result as a single JSON object instead of the human summary")

	verifyCmd.AddCommand(verifyAddCmd, verifyReadyCmd, verifyRemoveCmd)
}
