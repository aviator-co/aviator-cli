//go:build experimental

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/config"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/dustin/go-humanize/english"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// decisionTypes and decisionOutcomes name the server's values for flag help.
// The server validates them, so nothing is checked client-side.
const (
	decisionTypes    = "question, possible_bug, risk, choice"
	decisionOutcomes = "fixed, intended, accepted_risk, follow_up, withdrawn, decided"
)

// decisionsFlags holds the flags every decisions subcommand shares.
var decisionsFlags struct {
	JSON bool
}

// decisionsVia identifies the CLI as the client behind every comment.
const decisionsVia = "aviator-cli"

// actorFlags picks the actor a write is recorded as, and the model behind it.
type actorFlags struct {
	Actor string
	Model string
}

var decisionsCmd = &cobra.Command{
	Use:     "decisions",
	Aliases: []string{"decision"},
	Short:   "Read and record the decisions on a session's pull request",
	// Experimental: left out of help and completion, like actorsCmd.
	Hidden: true,
	Long: "A decision is a question, possible bug, risk, or choice raised about a\n" +
		"session's pull request. Each one has a comment thread and is either open\n" +
		"or settled with an outcome.\n" +
		"\n" +
		"Every write is recorded as an actor from your local actors file (see\n" +
		"`aviator actors`). To write as yourself, use the built-in actor\n" +
		"\"" + personActor + "\"; the server records it under your Aviator name.\n" +
		"Most writes go through apply, which records a whole JSON batch at once.\n" +
		"edit, comment, and settle take several decision IDs and apply to all of\n" +
		"them. The API takes creates, changes, and comments as separate requests,\n" +
		"so a batch is sent in steps; if one is refused, the steps before it\n" +
		"stay recorded and the error names them.\n" +
		"\n" +
		"These commands act as the caller, so they need a user access token (from\n" +
		"`aviator login` or a personal token).",
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
	Actor    actorFlags
}

var decisionsCreateCmd = &cobra.Command{
	Use:   "create <id>",
	Short: "Raise a new decision on a session",
	Long: "Raise a new decision on a session as --actor. --body is the first\n" +
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
		actor, err := resolveActor(f.Actor.Actor)
		if err != nil {
			return err
		}
		return recordDecisions(cmd, args[0], &decisionBatch{
			ModelMetadata: modelMetadata(f.Actor.Model),
			Decisions: []decisionBatchEntry{{
				Decision: batchDecision{
					Type:    f.Type,
					Title:   f.Title,
					Summary: f.Summary,
					Anchor:  anchor,
					Actor:   &actor,
				},
				Comments: []api.DecisionCommentInput{{Actor: actor, Body: body}},
			}},
		})
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
	Actor    actorFlags
}

var decisionsEditCmd = &cobra.Command{
	Use:   "edit <id> <decision-id>...",
	Short: "Change open decisions; every listed one gets the same changes",
	Long: "Change the type, title, summary, or anchor of open decisions as\n" +
		"--actor. --body also adds a comment to each one's thread. Only the user\n" +
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
		if f.Type == "" && f.Title == "" && f.Summary == "" && anchor == nil && body == "" {
			return errors.New("nothing to change: pass --type, --title, --summary, an anchor, or --body")
		}
		actor, err := resolveActor(f.Actor.Actor)
		if err != nil {
			return err
		}
		return recordForEach(cmd, args, f.Actor.Model, func(id int) decisionBatchEntry {
			entry := decisionBatchEntry{Decision: batchDecision{
				ID:      id,
				Type:    f.Type,
				Title:   f.Title,
				Summary: f.Summary,
				Anchor:  anchor,
			}}
			if f.Type != "" || f.Title != "" || f.Summary != "" || anchor != nil {
				entry.Decision.Actor = &actor
			}
			if body != "" {
				entry.Comments = []api.DecisionCommentInput{{Actor: actor, Body: body}}
			}
			return entry
		})
	},
}

var decisionsCommentFlags struct {
	Body     string
	BodyFile string
	Edit     int
	Actor    actorFlags
}

var decisionsCommentCmd = &cobra.Command{
	Use:   "comment <id> <decision-id>...",
	Short: "Add the same comment to decisions, or edit one with --edit",
	Long: "Add the same comment to each decision's thread as --actor.\n" +
		"\n" +
		"--edit <comment-id> instead replaces the body of one of your comments on\n" +
		"one decision (comment IDs are in `aviator decisions show`). Only the user\n" +
		"who wrote a comment can edit it, and it keeps its actor, so --edit takes\n" +
		"no --actor.",
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
		if f.Actor.Actor == "" {
			return errors.New("--actor is required to add a comment")
		}
		actor, err := resolveActor(f.Actor.Actor)
		if err != nil {
			return err
		}
		return recordForEach(cmd, args, f.Actor.Model, func(id int) decisionBatchEntry {
			return decisionBatchEntry{
				Decision: batchDecision{ID: id},
				Comments: []api.DecisionCommentInput{{Actor: actor, Body: body}},
			}
		})
	},
}

var decisionsSettleFlags struct {
	Outcome  string
	Summary  string
	Body     string
	BodyFile string
	Actor    actorFlags
}

var decisionsSettleCmd = &cobra.Command{
	Use:   "settle <id> <decision-id>...",
	Short: "Settle open decisions with an outcome (e.g. aviator decisions settle r/123 7 8 --outcome fixed --body why --actor person)",
	Long: "Settle open decisions with an outcome (" + decisionOutcomes + ") as\n" +
		"--actor. --summary sets the decision's summary of what was decided in\n" +
		"the same change, and --body adds a comment saying why it was settled\n" +
		"this way. A settled decision can't be edited or settled again.",
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		f := &decisionsSettleFlags
		body, err := collectBody(f.Body, f.BodyFile)
		if err != nil {
			return err
		}
		actor, err := resolveActor(f.Actor.Actor)
		if err != nil {
			return err
		}
		return recordForEach(cmd, args, f.Actor.Model, func(id int) decisionBatchEntry {
			entry := decisionBatchEntry{
				Decision:   batchDecision{ID: id},
				Settlement: &batchSettlement{Outcome: f.Outcome, Actor: actor, Body: body},
			}
			if f.Summary != "" {
				entry.Decision.Summary = f.Summary
				entry.Decision.Actor = &actor
			}
			return entry
		})
	},
}

var decisionsReopenFlags struct {
	Body     string
	BodyFile string
	Actor    actorFlags
}

var decisionsReopenCmd = &cobra.Command{
	Use:   "reopen <id> <decision-id>...",
	Short: "Reopen settled decisions",
	Long: "Reopen settled decisions as --actor, clearing their outcome. --body adds\n" +
		"a comment saying why. Only the user who raised a decision can reopen it.",
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		f := &decisionsReopenFlags
		body, err := collectBody(f.Body, f.BodyFile)
		if err != nil {
			return err
		}
		actor, err := resolveActor(f.Actor.Actor)
		if err != nil {
			return err
		}
		return recordForEach(cmd, args, f.Actor.Model, func(id int) decisionBatchEntry {
			entry := decisionBatchEntry{Decision: batchDecision{ID: id, Actor: &actor}, Reopen: true}
			if body != "" {
				entry.Comments = []api.DecisionCommentInput{{Actor: actor, Body: body}}
			}
			return entry
		})
	},
}

var decisionsApplyFlags struct {
	File string
}

var decisionsApplyCmd = &cobra.Command{
	Use:   "apply <id>",
	Short: "Create, edit, comment on, and settle decisions from a JSON batch file",
	Long: "Record a batch of decisions from a JSON file (- reads stdin):\n" +
		"\n" +
		"  {\n" +
		"    \"model_metadata\": {\"model\": \"claude-fable-5-1\"},\n" +
		"    \"decisions\": [\n" +
		"      {\"decision\": {\"type\": \"risk\", \"title\": \"...\", \"actor\": \"Front End Agent\"},\n" +
		"       \"comments\": [{\"actor\": \"Front End Agent\", \"body\": \"why it was raised\"}]},\n" +
		"      {\"decision\": {\"id\": 7},\n" +
		"       \"comments\": [{\"actor\": \"Backend Agent\", \"body\": \"a comment\"}]},\n" +
		"      {\"decision\": {\"id\": 8},\n" +
		"       \"settlement\": {\"outcome\": \"fixed\", \"actor\": \"person\", \"body\": \"why\"}}\n" +
		"    ]\n" +
		"  }\n" +
		"\n" +
		"A decision without an id is created; one with an id is edited, or only\n" +
		"picked when it sets nothing else. Every action names an actor from your\n" +
		"local actors file, or \"" + personActor + "\" for yourself: creating or\n" +
		"changing a decision, each comment, and each settlement. via is set to \"" + decisionsVia + "\" for you. Unknown\n" +
		"keys and unknown actors are refused before anything is sent.\n" +
		"\n" +
		"The batch is sent in steps: one request creating every new decision\n" +
		"(settled ones are created with their outcome), a change for each\n" +
		"existing decision it edits or settles, then each decision's comments,\n" +
		"with a settlement's body last as a comment from its actor. If a step is\n" +
		"refused, the ones before it stay recorded and the error names them, so\n" +
		"don't resend the whole batch.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		batch, err := readDecisionBatch(decisionsApplyFlags.File)
		if err != nil {
			return err
		}
		refs, err := batchActors(batch.Decisions)
		if err != nil {
			return err
		}
		if err := resolveActors(refs); err != nil {
			return err
		}
		return recordDecisions(cmd, args[0], batch)
	},
}

// decisionBatch is apply's input file. Its shape is the CLI's own, not the
// API's: applyBatch splits it into create, change, and comment requests.
type decisionBatch struct {
	Via           string               `json:"via,omitempty"`
	ModelMetadata *api.ModelMetadata   `json:"model_metadata,omitempty"`
	Decisions     []decisionBatchEntry `json:"decisions"`
}

// decisionBatchEntry is one decision in a batch: the decision itself, comments
// to add to its thread, and optionally a settlement that closes it.
type decisionBatchEntry struct {
	Decision   batchDecision              `json:"decision"`
	Comments   []api.DecisionCommentInput `json:"comments,omitempty"`
	Settlement *batchSettlement           `json:"settlement,omitempty"`
	// Reopen clears a settled decision's outcome as Decision.Actor. Only
	// `decisions reopen` sets it; batch files can't.
	Reopen bool `json:"-"`
}

// batchDecision is the decision's own fields. A zero ID creates a decision; a
// set ID changes an open one, or only picks it when nothing else is set. Actor
// names who raised or changed it.
type batchDecision struct {
	ID      int                 `json:"id,omitempty"`
	Type    string              `json:"type,omitempty"`
	Title   string              `json:"title,omitempty"`
	Summary string              `json:"summary,omitempty"`
	Anchor  *api.DecisionAnchor `json:"anchor,omitempty"`
	Actor   *api.ActorRef       `json:"actor,omitempty"`
}

// changes reports whether d creates or changes a decision, rather than only
// picking one.
func (d *batchDecision) changes() bool {
	return d.ID == 0 || d.Type != "" || d.Title != "" || d.Summary != "" || d.Anchor != nil
}

// batchSettlement closes a decision with an outcome. Body, the reason, is
// recorded as a comment from Actor.
type batchSettlement struct {
	Outcome string       `json:"outcome"`
	Actor   api.ActorRef `json:"actor"`
	Body    string       `json:"body,omitempty"`
}

// recordForEach builds one entry per decision ID in args[1:] and records them
// as a single batch against the session in args[0].
func recordForEach(cmd *cobra.Command, args []string, model string, entry func(id int) decisionBatchEntry) error {
	entries := make([]decisionBatchEntry, 0, len(args)-1)
	for _, arg := range args[1:] {
		id, err := parseDecisionID(arg)
		if err != nil {
			return err
		}
		entries = append(entries, entry(id))
	}
	return recordDecisions(cmd, args[0], &decisionBatch{
		ModelMetadata: modelMetadata(model),
		Decisions:     entries,
	})
}

// recordDecisions records batch on the session, then prints the decisions it
// touched as they now stand.
func recordDecisions(cmd *cobra.Command, sessionArg string, batch *decisionBatch) error {
	runbookNumber, err := parseRunbookID(sessionArg)
	if err != nil {
		return err
	}
	client, err := api.NewClient()
	if err != nil {
		return err
	}
	ids, err := applyBatch(cmd.Context(), client, runbookNumber, batch)
	if err != nil {
		return err
	}
	resp, err := client.ListDecisions(cmd.Context(), runbookNumber)
	if err != nil {
		return errors.Wrap(err, "recorded the batch, but couldn't fetch the result")
	}
	byID := make(map[int]api.Decision, len(resp.Decisions))
	for _, d := range resp.Decisions {
		byID[d.ID] = d
	}
	decisions := make([]api.Decision, 0, len(ids))
	for _, id := range ids {
		if d, ok := byID[id]; ok {
			decisions = append(decisions, d)
		}
	}
	if decisionsFlags.JSON {
		return printJSON(newDecisionListJSON(runbookNumber, decisions))
	}
	fmt.Printf("%s %s: %s recorded\n", colors.Success("✓"),
		formatRunbookID(runbookNumber), english.Plural(len(ids), "decision", ""))
	fmt.Print(formatDecisionTable(decisions))
	return nil
}

// applyBatch sends a batch as the API's separate requests, in this order: one
// POST creating every new decision (a settled one is created with its
// outcome), a PATCH for each existing decision the batch changes or settles,
// then a comments POST for each decision with commentary, a settlement's body
// last. It returns each entry's decision ID. A refused request stops the rest,
// and the error lists what was already recorded, since that can't be undone.
func applyBatch(ctx context.Context, client *api.Client, runbookNumber int, batch *decisionBatch) ([]int, error) {
	ids := make([]int, len(batch.Decisions))
	var creates []api.DecisionCreate
	var createdAt []int
	for i := range batch.Decisions {
		e := &batch.Decisions[i]
		d := &e.Decision
		if d.ID != 0 {
			ids[i] = d.ID
			continue
		}
		create := api.DecisionCreate{
			Type: d.Type, Title: d.Title, Summary: d.Summary, Anchor: d.Anchor, Actor: *d.Actor,
		}
		if e.Settlement != nil {
			create.Outcome = e.Settlement.Outcome
		}
		creates = append(creates, create)
		createdAt = append(createdAt, i)
	}

	var done []string
	refused := func(err error, step string) error {
		if len(done) == 0 {
			return errors.Wrap(err, step)
		}
		return errors.Wrapf(err, "%s was refused after recording %s; don't resend those",
			step, strings.Join(done, ", "))
	}

	if len(creates) > 0 {
		resp, err := client.CreateDecisions(ctx, runbookNumber, api.CreateDecisionsRequest{Decisions: creates})
		if err != nil {
			return nil, refused(err, "creating decisions")
		}
		if len(resp.Decisions) != len(creates) {
			return nil, errors.Errorf("asked to create %d decisions, but the server returned %d",
				len(creates), len(resp.Decisions))
		}
		created := make([]string, len(resp.Decisions))
		for j, d := range resp.Decisions {
			ids[createdAt[j]] = d.ID
			created[j] = fmt.Sprintf("#%d", d.ID)
		}
		done = append(done, "created "+strings.Join(created, " "))
	}

	for i := range batch.Decisions {
		e := &batch.Decisions[i]
		d := &e.Decision
		if e.Reopen {
			if _, err := client.ReopenDecision(ctx, runbookNumber, d.ID, *d.Actor); err != nil {
				return nil, refused(err, fmt.Sprintf("reopening #%d", d.ID))
			}
			done = append(done, fmt.Sprintf("reopened #%d", d.ID))
			continue
		}
		if d.ID == 0 || (!d.changes() && e.Settlement == nil) {
			continue
		}
		patch := api.DecisionPatch{Type: d.Type, Title: d.Title, Summary: d.Summary, Anchor: d.Anchor}
		if d.changes() {
			patch.Actor = *d.Actor
		} else {
			patch.Actor = e.Settlement.Actor
		}
		if e.Settlement != nil {
			patch.Outcome = e.Settlement.Outcome
		}
		if _, err := client.UpdateDecision(ctx, runbookNumber, d.ID, patch); err != nil {
			return nil, refused(err, fmt.Sprintf("changing #%d", d.ID))
		}
		done = append(done, fmt.Sprintf("changed #%d", d.ID))
	}

	for i := range batch.Decisions {
		e := &batch.Decisions[i]
		comments := e.Comments
		if s := e.Settlement; s != nil && s.Body != "" {
			comments = append(comments, api.DecisionCommentInput{Actor: s.Actor, Body: s.Body})
		}
		if len(comments) == 0 {
			continue
		}
		if _, err := client.AddDecisionComments(ctx, runbookNumber, ids[i], api.AddDecisionCommentsRequest{
			Via:           decisionsVia,
			ModelMetadata: batch.ModelMetadata,
			Comments:      comments,
		}); err != nil {
			return nil, refused(err, fmt.Sprintf("commenting on #%d", ids[i]))
		}
		done = append(done, fmt.Sprintf("commented on #%d", ids[i]))
	}
	return ids, nil
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

func modelMetadata(model string) *api.ModelMetadata {
	if model == "" {
		return nil
	}
	return &api.ModelMetadata{Model: model}
}

// batchActors returns every actor a batch names, and refuses an action that
// names none (creating or changing a decision, a comment, or a settlement) and
// a settlement without an outcome.
func batchActors(entries []decisionBatchEntry) ([]*api.ActorRef, error) {
	var refs []*api.ActorRef
	for i := range entries {
		e := &entries[i]
		at := fmt.Sprintf("decisions[%d]", i)
		d := e.Decision
		changes := d.changes()
		switch {
		case changes && (d.Actor == nil || d.Actor.Name == ""):
			return nil, errors.Errorf("%s.decision: actor is required to create or change a decision", at)
		case !changes && len(e.Comments) == 0 && e.Settlement == nil:
			return nil, errors.Errorf("%s: decision #%d has nothing to do; add a change, comments, or a settlement", at, d.ID)
		}
		if d.Actor != nil {
			refs = append(refs, d.Actor)
		}
		for j := range e.Comments {
			if e.Comments[j].Actor.Name == "" {
				return nil, errors.Errorf("%s.comments[%d]: actor is required", at, j)
			}
			refs = append(refs, &e.Comments[j].Actor)
		}
		if s := e.Settlement; s != nil {
			if s.Actor.Name == "" {
				return nil, errors.Errorf("%s.settlement: actor is required", at)
			}
			if s.Outcome == "" {
				return nil, errors.Errorf("%s.settlement: outcome is required", at)
			}
			refs = append(refs, &s.Actor)
		}
	}
	return refs, nil
}

// resolveActor looks up one actor by name in the local actors file.
func resolveActor(name string) (api.ActorRef, error) {
	ref := api.ActorRef{Name: name}
	if err := resolveActors([]*api.ActorRef{&ref}); err != nil {
		return api.ActorRef{}, err
	}
	return ref, nil
}

// resolveActors fills each actor's role from the local actors file, refusing
// any name that isn't there so a typo is caught before the batch is sent. The
// built-in person actor needs no entry and is sent without a name.
func resolveActors(refs []*api.ActorRef) error {
	path, err := config.ActorsFile()
	if err != nil {
		return err
	}
	actors, err := config.LoadActors(path)
	if err != nil {
		return err
	}
	roles := make(map[string]string, len(actors))
	for _, a := range actors {
		roles[a.Name] = a.Role
	}
	for _, ref := range refs {
		if ref.Name == personActor {
			*ref = api.ActorRef{Role: "person"}
			continue
		}
		role, ok := roles[ref.Name]
		switch {
		case !ok:
			return errors.Errorf("no actor %q in %s; add it with `aviator actors edit`, or use %q for yourself",
				ref.Name, path, personActor)
		case role == "person":
			return errors.Errorf("%q in %s is a person; write as yourself with the built-in actor %q, "+
				"and remove %q from the file", ref.Name, path, personActor, ref.Name)
		}
		ref.Role = role
	}
	return nil
}

// buildAnchor turns the anchor flags into an anchor, or nil when none were
// passed.
func buildAnchor(path, lines, commit string) (*api.DecisionAnchor, error) {
	if path == "" && lines == "" && commit == "" {
		return nil, nil
	}
	if lines != "" && path == "" {
		return nil, errors.New("--lines needs --path")
	}
	anchor := &api.DecisionAnchor{FilePath: path, CommitSHA: commit}
	if lines != "" {
		start, end, err := parseLineRange(lines)
		if err != nil {
			return nil, err
		}
		anchor.StartLine, anchor.EndLine = start, end
	}
	return anchor, nil
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

// readDecisionBatch reads apply's file. via may be left out, but anything
// other than the CLI's own value is refused rather than silently replaced.
func readDecisionBatch(path string) (*decisionBatch, error) {
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
	var req decisionBatch
	if err := dec.Decode(&req); err != nil {
		return nil, errors.Wrapf(err, "failed to parse %s", path)
	}
	if req.Via != "" && req.Via != decisionsVia {
		return nil, errors.Errorf("%s: via must be %q (or left out), not %q", path, decisionsVia, req.Via)
	}
	if len(req.Decisions) == 0 {
		return nil, errors.Errorf("%s has no decisions", path)
	}
	return &req, nil
}

// registerActorFlags registers --actor and --model; required marks --actor
// required, for commands where every use writes as an actor.
func registerActorFlags(cmd *cobra.Command, f *actorFlags, required bool) {
	fs := cmd.Flags()
	fs.StringVar(&f.Actor, "actor", "", "record this as the named actor from your local actors file, or "+personActor+" for yourself")
	fs.StringVar(&f.Model, "model", "", "the model that drafted this, sent as model_metadata on comments")
	if required {
		_ = cmd.MarkFlagRequired("actor")
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
	registerActorFlags(decisionsCreateCmd, &decisionsCreateFlags.Actor, true)
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
	registerActorFlags(decisionsEditCmd, &decisionsEditFlags.Actor, true)
	decisionsEditCmd.MarkFlagsMutuallyExclusive("body", "body-file")

	registerBodyFlags(decisionsCommentCmd.Flags(), &decisionsCommentFlags.Body, &decisionsCommentFlags.BodyFile,
		"the comment")
	decisionsCommentCmd.Flags().IntVar(&decisionsCommentFlags.Edit, "edit", 0,
		"replace the body of this comment instead of adding one")
	registerActorFlags(decisionsCommentCmd, &decisionsCommentFlags.Actor, false)
	decisionsCommentCmd.MarkFlagsMutuallyExclusive("body", "body-file")
	decisionsCommentCmd.MarkFlagsMutuallyExclusive("edit", "actor")
	decisionsCommentCmd.MarkFlagsMutuallyExclusive("edit", "model")

	registerBodyFlags(decisionsReopenCmd.Flags(), &decisionsReopenFlags.Body, &decisionsReopenFlags.BodyFile,
		"add a comment saying why it was reopened")
	registerActorFlags(decisionsReopenCmd, &decisionsReopenFlags.Actor, true)
	decisionsReopenCmd.MarkFlagsMutuallyExclusive("body", "body-file")

	sf := decisionsSettleCmd.Flags()
	sf.StringVar(&decisionsSettleFlags.Outcome, "outcome", "", "how it was settled ("+decisionOutcomes+")")
	sf.StringVar(&decisionsSettleFlags.Summary, "summary", "", "set the decision's summary of what was decided")
	registerBodyFlags(sf, &decisionsSettleFlags.Body, &decisionsSettleFlags.BodyFile,
		"add a comment saying why it was settled this way")
	registerActorFlags(decisionsSettleCmd, &decisionsSettleFlags.Actor, true)
	_ = decisionsSettleCmd.MarkFlagRequired("outcome")
	decisionsSettleCmd.MarkFlagsMutuallyExclusive("body", "body-file")

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
