package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

// Stacked Verify is behind a per-user allowlist, so these commands stay out of
// help and completion while it is; they run when called by name.

var verifyAddFlags struct {
	PR           int
	Criteria     []string
	CriteriaFile string
	BaseVersion  int
	Intent       string
	JSON         bool
}

var verifyAddCmd = &cobra.Command{
	Use:    "add <id> --pr <number> --intent <intent> --criteria-file <file> --base-version <version>",
	Short:  "Add a PR to a draft stacked review (e.g. aviator verify add r/42 --pr 102 ...)",
	Hidden: true,
	Long: "Add a PR to a draft stacked review and replace its intent and acceptance\n" +
		"criteria.\n" +
		"\n" +
		"The intent and criteria cover EVERY PR in the review, not just the new\n" +
		"one: read the current ones with `aviator show`, fold in the new PR, rewrite\n" +
		"any criteria that conflict, duplicate, or got refined, and send them all.\n" +
		"Without --intent the intent stays as it was. --base-version is\n" +
		"the version `aviator show` printed; the add is refused if the criteria\n" +
		"changed since.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		f := &verifyAddFlags
		if f.PR <= 0 {
			return errors.New("--pr is required")
		}
		if !cmd.Flags().Changed("base-version") {
			return errors.New("--base-version is required, the version `aviator show` prints")
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
			PRNumber:    f.PR,
			Criteria:    criteria,
			BaseVersion: f.BaseVersion,
			Intent:      f.Intent,
		})
		if err != nil {
			return stackedVerifyError(err, runbookNumber, f.BaseVersion)
		}
		if f.JSON {
			return printJSON(newStackedSessionJSON(resp))
		}
		id := formatRunbookID(resp.RunbookNumber)
		fmt.Printf("%s PR #%d added to %s%s\n", colors.Success("✓"), f.PR, id, formatVersion(resp.Version))
		fmt.Print(formatMembers(resp.Members))
		fmt.Printf("  %s\n", colors.Faint("Once the user agrees to the grouping: aviator verify ready "+id))
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

var verifyRegroupFlags struct {
	Groups []string
	JSON   bool
}

var verifyRegroupCmd = &cobra.Command{
	Use:    "regroup <id> --group <file> --group <file> [--group <file>]",
	Short:  "Split a stacked review into one new review per group",
	Hidden: true,
	Long: "Split a stacked review into separate reviews. Split only: it needs two\n" +
		"or more --group. To change criteria or intent, use `aviator edit`. Every\n" +
		"open PR of the review must be in exactly one group, and each group must\n" +
		"be a valid stack. The old review is archived and the new ones keep its\n" +
		"state.\n" +
		"\n" +
		"Each --group is a JSON file:\n" +
		"\n" +
		"  {\"pr_numbers\": [101, 102], \"intent\": \"...\",\n" +
		"   \"criteria\": [\"...\"], \"decision_ids\": [3, 4]}\n" +
		"\n" +
		"criteria is the full list for the group's PRs; decision_ids names the\n" +
		"decisions that move to it (from `aviator decisions list`).",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		if len(verifyRegroupFlags.Groups) < 2 {
			return errors.New("regroup only splits a stack: pass two or more --group (use `aviator edit` to change criteria)")
		}
		groups := make([]api.RegroupGroup, 0, len(verifyRegroupFlags.Groups))
		for _, path := range verifyRegroupFlags.Groups {
			g, err := readRegroupGroup(path)
			if err != nil {
				return err
			}
			groups = append(groups, g)
		}
		client, err := api.NewClient()
		if err != nil {
			return err
		}
		resp, err := client.RegroupVerify(cmd.Context(), runbookNumber, api.RegroupVerifyRequest{Groups: groups})
		if err != nil {
			return stackedVerifyError(err, runbookNumber, 0)
		}
		if verifyRegroupFlags.JSON {
			return printJSON(newRegroupJSON(runbookNumber, resp))
		}
		fmt.Printf("%s %s archived, replaced by:\n", colors.Success("✓"), formatRunbookID(runbookNumber))
		for i := range resp.Sessions {
			s := &resp.Sessions[i]
			fmt.Printf("  %s %s — %s\n", formatRunbookID(s.RunbookNumber), deref(s.State), s.URL)
			fmt.Print(formatMembers(s.Members))
		}
		return nil
	},
}

// readRegroupGroup reads one --group file. Unknown keys are refused so a typo
// can't silently drop a group's criteria or decisions.
func readRegroupGroup(path string) (api.RegroupGroup, error) {
	var g api.RegroupGroup
	data, err := os.ReadFile(path)
	if err != nil {
		return g, errors.Wrapf(err, "failed to read group file %s", path)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		return g, errors.Wrapf(err, "invalid group file %s", path)
	}
	g.Intent = strings.TrimSpace(g.Intent)
	switch {
	case len(g.PRNumbers) == 0:
		return g, errors.Errorf("group file %s: pr_numbers is required", path)
	case g.Intent == "":
		return g, errors.Errorf("group file %s: intent is required", path)
	case len(g.Criteria) == 0:
		return g, errors.Errorf("group file %s: criteria is required", path)
	}
	if g.DecisionIDs == nil {
		g.DecisionIDs = []int{}
	}
	return g, nil
}

// stackedVerifyError turns the stacked-session refusals into the next step to
// take. baseVersion is the version an add was based on, 0 otherwise.
func stackedVerifyError(err error, runbookNumber, baseVersion int) error {
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	id := formatRunbookID(runbookNumber)
	switch apiErr.Code {
	case "session-draft":
		return errors.Errorf("%s is a draft and doesn't run until marked ready; once the user agrees "+
			"to the grouping, run `aviator verify ready %s`", id, id)
	case "stale-runbook-version":
		if baseVersion > 0 {
			return errors.Errorf("the criteria of %s changed since version %d; re-read them with "+
				"`aviator show %s`, rewrite the full list, and retry with the new --base-version: %s",
				id, baseVersion, id, apiErr.Message)
		}
	case "stack-invalid":
		return errors.Errorf("the stack of %s isn't valid: %s", id, apiErr.Message)
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

type regroupJSON struct {
	Archived string               `json:"archived"`
	Sessions []stackedSessionJSON `json:"sessions"`
}

func newRegroupJSON(runbookNumber int, resp *api.RegroupVerifyResponse) regroupJSON {
	out := regroupJSON{
		Archived: formatRunbookID(runbookNumber),
		Sessions: make([]stackedSessionJSON, 0, len(resp.Sessions)),
	}
	for i := range resp.Sessions {
		out.Sessions = append(out.Sessions, newStackedSessionJSON(&resp.Sessions[i]))
	}
	return out
}

func init() {
	af := verifyAddCmd.Flags()
	registerCriteriaFlags(verifyAddCmd, &verifyAddFlags.Criteria, &verifyAddFlags.CriteriaFile)
	af.IntVar(&verifyAddFlags.PR, "pr", 0, "number of the PR to add")
	af.StringVar(&verifyAddFlags.Intent, "intent", "", "new intent covering every PR in the review, the new one included")
	af.IntVar(&verifyAddFlags.BaseVersion, "base-version", 0,
		"criteria version the new list is based on, as aviator show prints it")
	af.BoolVar(&verifyAddFlags.JSON, "json", false, "print the result as a single JSON object instead of the human summary")

	cf := verifyReadyCmd.Flags()
	cf.BoolVar(&verifyReadyFlags.JSON, "json", false, "print the result as a single JSON object instead of the human summary")

	rf := verifyRegroupCmd.Flags()
	rf.StringArrayVar(&verifyRegroupFlags.Groups, "group", nil, "JSON file describing one new review (repeatable)")
	rf.BoolVar(&verifyRegroupFlags.JSON, "json", false, "print the result as a single JSON object instead of the human summary")

	verifyCmd.AddCommand(verifyAddCmd, verifyReadyCmd, verifyRegroupCmd)
}
