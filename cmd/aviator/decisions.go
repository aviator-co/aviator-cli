package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/dustin/go-humanize/english"
	"github.com/spf13/cobra"
)

// decisionsFlags holds the flags every decisions subcommand shares.
var decisionsFlags struct {
	JSON bool
}

var decisionsCmd = &cobra.Command{
	Use:     "decisions",
	Aliases: []string{"decision"},
	Short:   "Read and record the decisions on a session's pull request",
	// Still changing, so left out of help and completion; it runs when called
	// by name.
	Hidden: true,
	Long: "A decision is a question, possible bug, risk, or choice raised about a\n" +
		"session's pull request. Each one has a comment thread and is either open\n" +
		"or settled with an outcome.\n" +
		"\n" +
		"list and show read them. apply records a JSON batch that creates,\n" +
		"changes, settles, reopens, and comments on decisions in one request, so\n" +
		"either all of it is recorded or none of it is.",
}

var decisionsListFlags struct {
	Status string
	Page   int
	Limit  int
}

var decisionsListCmd = &cobra.Command{
	Use:   "list <id>",
	Short: "List a session's decisions (e.g. aviator decisions list r/123 --status open)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		client, err := api.NewClient()
		if err != nil {
			return err
		}
		f := &decisionsListFlags
		resp, err := client.ListDecisions(cmd.Context(), runbookNumber, api.ListDecisionsParams{
			Status:  f.Status,
			Page:    f.Page,
			PerPage: f.Limit,
		})
		if err != nil {
			return err
		}
		if decisionsFlags.JSON {
			out := newDecisionListJSON(runbookNumber, resp.Decisions)
			out.HasMore = &resp.HasMore
			return printJSON(out)
		}
		if len(resp.Decisions) == 0 {
			fmt.Println(noDecisionsMessage(runbookNumber))
			return nil
		}
		fmt.Print(formatDecisionTable(resp.Decisions))
		if resp.HasMore {
			fmt.Printf("\nSee more decisions --page %d\n", max(f.Page, 1)+1)
		}
		return nil
	},
}

// noDecisionsMessage says what was listed, and past the last page says that
// instead, since "no decisions" there would be a lie.
func noDecisionsMessage(runbookNumber int) string {
	if decisionsListFlags.Page > 1 {
		return fmt.Sprintf("Nothing on page %d, the listing ends before it. Try a lower --page.",
			decisionsListFlags.Page)
	}
	if status := decisionsListFlags.Status; status != "" {
		return fmt.Sprintf("No %s decisions on %s.", status, formatRunbookID(runbookNumber))
	}
	return fmt.Sprintf("No decisions on %s.", formatRunbookID(runbookNumber))
}

var decisionsShowCmd = &cobra.Command{
	Use:   "show <id> <decision-id>",
	Short: "Show one decision with its comment thread",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		decisionID, err := parseDecisionID(args[1])
		if err != nil {
			return err
		}
		client, err := api.NewClient()
		if err != nil {
			return err
		}
		d, err := client.GetDecision(cmd.Context(), runbookNumber, decisionID)
		if err != nil {
			return err
		}
		if decisionsFlags.JSON {
			return printJSON(newDecisionJSON(d))
		}
		fmt.Print(formatDecisionDetail(d))
		return nil
	},
}

var decisionsApplyFlags struct {
	File string
}

var decisionsApplyCmd = &cobra.Command{
	Use:   "apply <id>",
	Short: "Create, change, settle, reopen, and comment on decisions from a JSON batch file",
	Long: "Record a batch of decisions from a JSON file (- reads stdin), all or\n" +
		"nothing. The file is sent as is and the server checks it:\n" +
		"\n" +
		"  {\"decisions\": [\n" +
		"    {\"author\": {\"role\": \"coding_agent\", \"model\": \"claude-fable-5-1\",\n" +
		"                \"harness\": \"claude-code\", \"skills\": [\"go-backend\"]},\n" +
		"     \"type\": \"risk\", \"title\": \"...\", \"summary\": \"...\",\n" +
		"     \"anchor\": {\"commit_sha\": \"abc1234\", \"file_path\": \"a.go\", \"start_line\": 10, \"end_line\": 20},\n" +
		"     \"comments\": [{\"body\": \"why it was raised\", \"model_metadata\": {\"subagent\": \"reviewer\"}}]},\n" +
		"    {\"id\": 7, \"comments\": [{\"author\": {\"role\": \"person\"}, \"body\": \"a comment\"},\n" +
		"                         {\"id\": 23, \"body\": \"an edited comment\"}]},\n" +
		"    {\"id\": 8, \"author\": {\"role\": \"person\"}, \"outcome\": \"fixed\", \"resolution\": \"why\"},\n" +
		"    {\"id\": 9, \"author\": {\"role\": \"person\"}, \"outcome\": null,\n" +
		"     \"comments\": [{\"body\": \"why it was reopened\"}]}\n" +
		"  ]}\n" +
		"\n" +
		"An item without an id creates a decision and needs a type and title. One\n" +
		"with an id changes only the keys it has: an outcome settles it and needs\n" +
		"a resolution, a null outcome reopens it and needs a comment saying why,\n" +
		"and null clears summary or anchor. An item that creates or changes a\n" +
		"decision needs an author with a role (person, coding_agent,\n" +
		"review_agent); add model, harness, and skills when you know them. A new\n" +
		"comment without an author takes the item's. A comment with an id edits\n" +
		"that comment and has only id and body.\n" +
		"\n" +
		"Anyone can settle, reopen, and comment. Only the user who raised a\n" +
		"decision can change its type, title, summary, or anchor, and a settled\n" +
		"decision has to be reopened first.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		body, err := readDecisionBatch(decisionsApplyFlags.File)
		if err != nil {
			return err
		}
		client, err := api.NewClient()
		if err != nil {
			return err
		}
		resp, err := client.ApplyDecisions(cmd.Context(), runbookNumber, body)
		if err != nil {
			return err
		}
		if decisionsFlags.JSON {
			return printJSON(newDecisionListJSON(runbookNumber, resp.Decisions))
		}
		fmt.Printf("%s %s: %s recorded\n", colors.Success("✓"),
			formatRunbookID(runbookNumber), english.Plural(len(resp.Decisions), "decision", ""))
		fmt.Print(formatDecisionTable(resp.Decisions))
		return nil
	},
}

// parseDecisionID accepts a bare number or the #<number> form the CLI prints.
func parseDecisionID(arg string) (int, error) {
	s := strings.TrimPrefix(strings.TrimSpace(arg), "#")
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, errors.Errorf("invalid decision ID %q, expected a positive number", arg)
	}
	return n, nil
}

// readDecisionBatch reads apply's file, or stdin for -, unparsed.
func readDecisionBatch(path string) ([]byte, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read %s", path)
	}
	return data, nil
}

func init() {
	decisionsCmd.PersistentFlags().BoolVar(&decisionsFlags.JSON, "json", false,
		"print the result as a single JSON object instead of the human summary")

	lf := decisionsListCmd.Flags()
	lf.StringVar(&decisionsListFlags.Status, "status", "", "only open or settled decisions")
	lf.IntVar(&decisionsListFlags.Page, "page", 1, "which page to show")
	lf.IntVar(&decisionsListFlags.Limit, "limit", 20, "decisions per page (max 100)")

	decisionsApplyCmd.Flags().StringVar(&decisionsApplyFlags.File, "file", "", "the JSON batch file, or - for stdin")
	_ = decisionsApplyCmd.MarkFlagRequired("file")

	decisionsCmd.AddCommand(
		decisionsListCmd,
		decisionsShowCmd,
		decisionsApplyCmd,
	)
	rootCmd.AddCommand(decisionsCmd)
}
