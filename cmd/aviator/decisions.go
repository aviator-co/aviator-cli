//go:build experimental

package main

import (
	"encoding/json"
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
	"github.com/spf13/pflag"
)

// decisionTypes, decisionOutcomes, and decisionRoles name the server's values
// for flag help. The server validates them, so nothing is checked client-side.
const (
	decisionTypes    = "question, possible_bug, risk, choice"
	decisionOutcomes = "fixed, intended, accepted_risk, follow_up, withdrawn, decided"
	decisionRoles    = "person, coding_agent, review_agent, external_bot"
)

// decisionsFlags holds the flags every decisions subcommand shares.
var decisionsFlags struct {
	JSON bool
}

// authorFlags are the role a write is recorded as, and the agent details
// behind it when known.
type authorFlags struct {
	Role    string
	Model   string
	Harness string
	Skills  []string
}

func (f *authorFlags) author() api.DecisionAuthor {
	return api.DecisionAuthor{Role: f.Role, Model: f.Model, Harness: f.Harness, Skills: f.Skills}
}

var decisionsCmd = &cobra.Command{
	Use:     "decisions",
	Aliases: []string{"decision"},
	Short:   "Read and record the decisions on a session's pull request",
	// Experimental: left out of help and completion.
	Hidden: true,
	Long: "A decision is a question, possible bug, risk, or choice raised about a\n" +
		"session's pull request. Each one has a comment thread and is either open\n" +
		"or settled with an outcome.\n" +
		"\n" +
		"Every write is recorded under a --role (" + decisionRoles + "),\n" +
		"with --model, --harness, and --skill describing the agent when there is\n" +
		"one. edit, comment, settle, and reopen take several decision IDs and apply\n" +
		"to all of them, and apply records a whole JSON batch. Each command sends\n" +
		"one request, so either all of it is recorded or none of it is.\n" +
		"\n" +
		"Writes act as the caller, so they need a user access token. Comments are\n" +
		"credited to aviator-cli when you're signed in with `aviator login`, and\n" +
		"to the API when you use a personal token.",
}

var decisionsListCmd = &cobra.Command{
	Use:   "list <id>",
	Short: "List a session's decisions, open ones first (e.g. aviator decisions list r/123)",
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
		resp, err := client.ListDecisions(cmd.Context(), runbookNumber)
		if err != nil {
			return err
		}
		if decisionsFlags.JSON {
			return printJSON(newDecisionListJSON(runbookNumber, resp.Decisions))
		}
		if len(resp.Decisions) == 0 {
			fmt.Printf("No decisions on %s.\n", formatRunbookID(runbookNumber))
			return nil
		}
		fmt.Print(formatDecisionTable(resp.Decisions))
		return nil
	},
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

var decisionsCreateFlags struct {
	Type     string
	Title    string
	Summary  string
	Path     string
	Lines    string
	Commit   string
	Body     string
	BodyFile string
	Author   authorFlags
}

var decisionsCreateCmd = &cobra.Command{
	Use:   "create <id>",
	Short: "Raise a new decision on a session",
	Long: "Raise a new decision on a session as --role. --body is the first\n" +
		"comment on its thread, explaining why it was raised. --path, --lines,\n" +
		"and --commit optionally pin it to lines of a file.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		f := &decisionsCreateFlags
		body, err := collectBody(f.Body, f.BodyFile)
		if err != nil {
			return err
		}
		if body == "" {
			return errors.New("--body (or --body-file) is required")
		}
		anchor, err := buildAnchor(f.Path, f.Lines, f.Commit)
		if err != nil {
			return err
		}
		author := f.Author.author()
		return recordDecisions(cmd, args[0], []api.DecisionBatchItem{{
			Author:   author,
			Type:     f.Type,
			Title:    f.Title,
			Summary:  f.Summary,
			Anchor:   anchor,
			Comments: []api.DecisionCommentInput{{Author: author, Body: body}},
		}})
	},
}

var decisionsEditFlags struct {
	Type     string
	Title    string
	Summary  string
	Path     string
	Lines    string
	Commit   string
	Body     string
	BodyFile string
	Author   authorFlags
}

var decisionsEditCmd = &cobra.Command{
	Use:   "edit <id> <decision-id>...",
	Short: "Change open decisions; every listed one gets the same changes",
	Long: "Change the type, title, summary, or anchor of open decisions as\n" +
		"--role. --body also adds a comment to each one's thread. Only the user\n" +
		"who raised a decision can edit it, and settled decisions can't be edited.",
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		f := &decisionsEditFlags
		body, err := collectBody(f.Body, f.BodyFile)
		if err != nil {
			return err
		}
		anchor, err := buildAnchor(f.Path, f.Lines, f.Commit)
		if err != nil {
			return err
		}
		if f.Type == "" && f.Title == "" && f.Summary == "" && anchor.IsZero() && body == "" {
			return errors.New("nothing to change: pass --type, --title, --summary, an anchor, or --body")
		}
		author := f.Author.author()
		return recordForEach(cmd, args, func(id int) api.DecisionBatchItem {
			item := api.DecisionBatchItem{
				ID:      id,
				Author:  author,
				Type:    f.Type,
				Title:   f.Title,
				Summary: f.Summary,
				Anchor:  anchor,
			}
			if body != "" {
				item.Comments = []api.DecisionCommentInput{{Author: author, Body: body}}
			}
			return item
		})
	},
}

var decisionsCommentFlags struct {
	Body     string
	BodyFile string
	Edit     int
	Author   authorFlags
}

var decisionsCommentCmd = &cobra.Command{
	Use:   "comment <id> <decision-id>...",
	Short: "Add the same comment to decisions, or edit one with --edit",
	Long: "Add the same comment to each decision's thread as --role.\n" +
		"\n" +
		"--edit <comment-id> instead replaces the body of one of your comments on\n" +
		"one decision (comment IDs are in `aviator decisions show`). Only the user\n" +
		"who wrote a comment can edit it, and it keeps its author, so --edit takes\n" +
		"no --role.",
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		f := &decisionsCommentFlags
		body, err := collectBody(f.Body, f.BodyFile)
		if err != nil {
			return err
		}
		if body == "" {
			return errors.New("--body (or --body-file) is required")
		}
		if cmd.Flags().Changed("edit") {
			return editDecisionComment(cmd, args, f.Edit, body)
		}
		if f.Author.Role == "" {
			return errors.New("--role is required to add a comment")
		}
		author := f.Author.author()
		return recordForEach(cmd, args, func(id int) api.DecisionBatchItem {
			return api.DecisionBatchItem{
				ID:       id,
				Author:   author,
				Comments: []api.DecisionCommentInput{{Author: author, Body: body}},
			}
		})
	},
}

var decisionsSettleFlags struct {
	Outcome    string
	Summary    string
	Resolution string
	Author     authorFlags
}

var decisionsSettleCmd = &cobra.Command{
	Use:   "settle <id> <decision-id>...",
	Short: "Settle open decisions with an outcome (e.g. aviator decisions settle r/123 7 8 --outcome fixed --resolution why --role person)",
	Long: "Settle open decisions with an outcome (" + decisionOutcomes + ") as\n" +
		"--role. --resolution says why it was settled this way, and --summary\n" +
		"sets the decision's summary of what was decided in the same change. A\n" +
		"settled decision can't be edited or settled again until it is reopened.",
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		f := &decisionsSettleFlags
		author := f.Author.author()
		return recordForEach(cmd, args, func(id int) api.DecisionBatchItem {
			return api.DecisionBatchItem{
				ID:         id,
				Author:     author,
				Summary:    f.Summary,
				Outcome:    api.ValueOf(f.Outcome),
				Resolution: strings.TrimSpace(f.Resolution),
			}
		})
	},
}

var decisionsReopenFlags struct {
	Body     string
	BodyFile string
	Author   authorFlags
}

var decisionsReopenCmd = &cobra.Command{
	Use:   "reopen <id> <decision-id>...",
	Short: "Reopen settled decisions",
	Long: "Reopen settled decisions as --role, clearing their outcome and\n" +
		"resolution. --body adds a comment saying why. Only the user who raised a\n" +
		"decision can reopen it.",
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		f := &decisionsReopenFlags
		body, err := collectBody(f.Body, f.BodyFile)
		if err != nil {
			return err
		}
		author := f.Author.author()
		return recordForEach(cmd, args, func(id int) api.DecisionBatchItem {
			item := api.DecisionBatchItem{ID: id, Author: author, Outcome: api.Null[string]()}
			if body != "" {
				item.Comments = []api.DecisionCommentInput{{Author: author, Body: body}}
			}
			return item
		})
	},
}

var decisionsApplyFlags struct {
	File string
}

var decisionsApplyCmd = &cobra.Command{
	Use:   "apply <id>",
	Short: "Create, edit, comment on, settle, and reopen decisions from a JSON batch file",
	Long: "Record a batch of decisions from a JSON file (- reads stdin), all or\n" +
		"nothing:\n" +
		"\n" +
		"  {\"decisions\": [\n" +
		"    {\"author\": {\"role\": \"coding_agent\", \"model\": \"claude-fable-5-1\",\n" +
		"                \"harness\": \"claude-code\", \"skills\": [\"go-backend\"]},\n" +
		"     \"type\": \"risk\", \"title\": \"...\", \"summary\": \"...\",\n" +
		"     \"anchor\": {\"commit_sha\": \"abc1234\", \"file_path\": \"a.go\", \"start_line\": 10, \"end_line\": 20},\n" +
		"     \"comments\": [{\"author\": {\"role\": \"coding_agent\"}, \"body\": \"why it was raised\",\n" +
		"                   \"model_metadata\": {\"subagent\": \"reviewer\"}}]},\n" +
		"    {\"id\": 7, \"author\": {\"role\": \"person\"},\n" +
		"     \"comments\": [{\"author\": {\"role\": \"person\"}, \"body\": \"a comment\"}]},\n" +
		"    {\"id\": 8, \"author\": {\"role\": \"person\"}, \"outcome\": \"fixed\", \"resolution\": \"why\"},\n" +
		"    {\"id\": 9, \"author\": {\"role\": \"person\"}, \"outcome\": null}\n" +
		"  ]}\n" +
		"\n" +
		"An item without an id creates a decision and needs a type and title. One\n" +
		"with an id changes only the keys it has: an outcome settles it, a null\n" +
		"outcome reopens it, a null anchor clears its anchor, and comments alone\n" +
		"only add comments. Every item and comment needs an author with a role\n" +
		"(" + decisionRoles + "); add model, harness, and\n" +
		"skills when you know them. Unknown keys are refused before anything is\n" +
		"sent.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		batch, err := readDecisionBatch(decisionsApplyFlags.File)
		if err != nil {
			return err
		}
		return recordDecisions(cmd, args[0], batch.Decisions)
	},
}

// recordForEach builds one item per decision ID in args[1:] and records them
// as a single batch against the session in args[0].
func recordForEach(cmd *cobra.Command, args []string, item func(id int) api.DecisionBatchItem) error {
	items := make([]api.DecisionBatchItem, 0, len(args)-1)
	for _, arg := range args[1:] {
		id, err := parseDecisionID(arg)
		if err != nil {
			return err
		}
		items = append(items, item(id))
	}
	return recordDecisions(cmd, args[0], items)
}

// recordDecisions sends items to the session as one batch, then prints the
// decisions it touched as they now stand.
func recordDecisions(cmd *cobra.Command, sessionArg string, items []api.DecisionBatchItem) error {
	runbookNumber, err := parseRunbookID(sessionArg)
	if err != nil {
		return err
	}
	client, err := api.NewClient()
	if err != nil {
		return err
	}
	resp, err := client.ApplyDecisions(cmd.Context(), runbookNumber, api.DecisionBatchRequest{Decisions: items})
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
}

// editDecisionComment replaces the body of comment commentID on the one
// decision in args[1].
func editDecisionComment(cmd *cobra.Command, args []string, commentID int, body string) error {
	if len(args) != 2 {
		return errors.New("--edit takes exactly one decision ID")
	}
	if commentID <= 0 {
		return errors.Errorf("invalid --edit %d, expected a comment ID", commentID)
	}
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
	c, err := client.EditDecisionComment(cmd.Context(), runbookNumber, decisionID, commentID, body)
	if err != nil {
		return err
	}
	if decisionsFlags.JSON {
		return printJSON(newDecisionCommentJSON(c))
	}
	fmt.Printf("%s %s: comment #%d on #%d edited\n", colors.Success("✓"),
		formatRunbookID(runbookNumber), c.ID, decisionID)
	fmt.Println(indent(strings.TrimSpace(c.Body), "  "))
	return nil
}

// buildAnchor turns the anchor flags into an anchor, left out when none were
// passed.
func buildAnchor(path, lines, commit string) (api.Nullable[api.DecisionAnchor], error) {
	if path == "" && lines == "" && commit == "" {
		return api.Nullable[api.DecisionAnchor]{}, nil
	}
	if lines != "" && path == "" {
		return api.Nullable[api.DecisionAnchor]{}, errors.New("--lines needs --path")
	}
	anchor := api.DecisionAnchor{FilePath: path, CommitSHA: commit}
	if lines != "" {
		start, end, err := parseLineRange(lines)
		if err != nil {
			return api.Nullable[api.DecisionAnchor]{}, err
		}
		anchor.StartLine, anchor.EndLine = start, end
	}
	return api.ValueOf(anchor), nil
}

// parseLineRange reads --lines as <line> or <start>-<end>.
func parseLineRange(s string) (start, end int, err error) {
	first, last, isRange := strings.Cut(strings.TrimSpace(s), "-")
	if !isRange {
		last = first
	}
	start, errStart := strconv.Atoi(strings.TrimSpace(first))
	end, errEnd := strconv.Atoi(strings.TrimSpace(last))
	if errStart != nil || errEnd != nil || start < 1 || end < start {
		return 0, 0, errors.Errorf("invalid --lines %q, expected <line> or <start>-<end>", s)
	}
	return start, end, nil
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

// readDecisionBatch reads apply's file, which is the API's batch body. Unknown
// keys are refused, so a typo, or a via or batch-level model_metadata from the
// old format, is caught before anything is sent.
func readDecisionBatch(path string) (*api.DecisionBatchRequest, error) {
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
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var req api.DecisionBatchRequest
	if err := dec.Decode(&req); err != nil {
		return nil, errors.Wrapf(err, "failed to parse %s", path)
	}
	if len(req.Decisions) == 0 {
		return nil, errors.Errorf("%s has no decisions", path)
	}
	return &req, nil
}

// registerAuthorFlags registers --role, --model, --harness, and --skill;
// required marks --role required, for commands where every use writes.
func registerAuthorFlags(cmd *cobra.Command, f *authorFlags, required bool) {
	fs := cmd.Flags()
	fs.StringVar(&f.Role, "role", "", "record this under a role ("+decisionRoles+")")
	fs.StringVar(&f.Model, "model", "", "the model behind this, for an agent")
	fs.StringVar(&f.Harness, "harness", "", "the harness the agent runs in (e.g. claude-code)")
	fs.StringArrayVar(&f.Skills, "skill", nil, "a skill the agent used (repeatable)")
	if required {
		_ = cmd.MarkFlagRequired("role")
	}
}

func registerAnchorFlags(fs *pflag.FlagSet, path, lines, commit *string) {
	fs.StringVar(path, "path", "", "pin to this file path")
	fs.StringVar(lines, "lines", "", "pin to <line> or <start>-<end> in --path")
	fs.StringVar(commit, "commit", "", "the commit SHA the anchor refers to")
}

func init() {
	decisionsCmd.PersistentFlags().BoolVar(&decisionsFlags.JSON, "json", false,
		"print the result as a single JSON object instead of the human summary")

	cf := decisionsCreateCmd.Flags()
	cf.StringVar(&decisionsCreateFlags.Type, "type", "", "the kind of decision ("+decisionTypes+")")
	cf.StringVar(&decisionsCreateFlags.Title, "title", "", "a one-line title")
	cf.StringVar(&decisionsCreateFlags.Summary, "summary", "", "an optional longer description")
	registerAnchorFlags(cf, &decisionsCreateFlags.Path, &decisionsCreateFlags.Lines, &decisionsCreateFlags.Commit)
	registerBodyFlags(cf, &decisionsCreateFlags.Body, &decisionsCreateFlags.BodyFile,
		"the first comment, explaining the decision")
	registerAuthorFlags(decisionsCreateCmd, &decisionsCreateFlags.Author, true)
	_ = decisionsCreateCmd.MarkFlagRequired("type")
	_ = decisionsCreateCmd.MarkFlagRequired("title")
	decisionsCreateCmd.MarkFlagsMutuallyExclusive("body", "body-file")

	ef := decisionsEditCmd.Flags()
	ef.StringVar(&decisionsEditFlags.Type, "type", "", "change the kind of decision ("+decisionTypes+")")
	ef.StringVar(&decisionsEditFlags.Title, "title", "", "change the title")
	ef.StringVar(&decisionsEditFlags.Summary, "summary", "", "change the summary")
	registerAnchorFlags(ef, &decisionsEditFlags.Path, &decisionsEditFlags.Lines, &decisionsEditFlags.Commit)
	registerBodyFlags(ef, &decisionsEditFlags.Body, &decisionsEditFlags.BodyFile,
		"also add this comment to each decision")
	registerAuthorFlags(decisionsEditCmd, &decisionsEditFlags.Author, true)
	decisionsEditCmd.MarkFlagsMutuallyExclusive("body", "body-file")

	registerBodyFlags(decisionsCommentCmd.Flags(), &decisionsCommentFlags.Body, &decisionsCommentFlags.BodyFile,
		"the comment")
	decisionsCommentCmd.Flags().IntVar(&decisionsCommentFlags.Edit, "edit", 0,
		"replace the body of this comment instead of adding one")
	registerAuthorFlags(decisionsCommentCmd, &decisionsCommentFlags.Author, false)
	decisionsCommentCmd.MarkFlagsMutuallyExclusive("body", "body-file")
	for _, name := range []string{"role", "model", "harness", "skill"} {
		decisionsCommentCmd.MarkFlagsMutuallyExclusive("edit", name)
	}

	registerBodyFlags(decisionsReopenCmd.Flags(), &decisionsReopenFlags.Body, &decisionsReopenFlags.BodyFile,
		"add a comment saying why it was reopened")
	registerAuthorFlags(decisionsReopenCmd, &decisionsReopenFlags.Author, true)
	decisionsReopenCmd.MarkFlagsMutuallyExclusive("body", "body-file")

	sf := decisionsSettleCmd.Flags()
	sf.StringVar(&decisionsSettleFlags.Outcome, "outcome", "", "how it was settled ("+decisionOutcomes+")")
	sf.StringVar(&decisionsSettleFlags.Summary, "summary", "", "set the decision's summary of what was decided")
	sf.StringVar(&decisionsSettleFlags.Resolution, "resolution", "", "why it was settled this way")
	registerAuthorFlags(decisionsSettleCmd, &decisionsSettleFlags.Author, true)
	_ = decisionsSettleCmd.MarkFlagRequired("outcome")

	decisionsApplyCmd.Flags().StringVar(&decisionsApplyFlags.File, "file", "", "the JSON batch file, or - for stdin")
	_ = decisionsApplyCmd.MarkFlagRequired("file")

	decisionsCmd.AddCommand(
		decisionsListCmd,
		decisionsShowCmd,
		decisionsCreateCmd,
		decisionsEditCmd,
		decisionsCommentCmd,
		decisionsSettleCmd,
		decisionsReopenCmd,
		decisionsApplyCmd,
	)
	rootCmd.AddCommand(decisionsCmd)
}
