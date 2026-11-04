package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/aviator-co/aviator-cli/internal/utils/colors"
	"github.com/spf13/cobra"
)

var evidenceSubmitFlags struct {
	Commit string
	JSON   bool
}

const docsEvidenceSubmit = "https://docs.aviator.co/verify/how-to-guides/submitting-evidence-from-your-own-run"

var evidenceSubmitCmd = &cobra.Command{
	Use:   "submit <id> <manifest>",
	Short: "Submit scenarios and evidence from your own run of the change (e.g. aviator evidence submit r/123 evidence.json)",
	Long: "Upload the scenarios you ran yourself and the evidence each step\n" +
		"produced, for a session created with --evidence-source external.\n" +
		"Aviator judges that evidence instead of collecting its own.\n" +
		"\n" +
		"The manifest is JSON. Each scenario has a summary and steps;\n" +
		"each step may list evidence files with a type (screenshot, dom_snapshot,\n" +
		"console_log, api_response, network_request, client_storage) and a label\n" +
		"saying what the file shows. Paths are relative to the manifest. A\n" +
		"scenario may pin the criteria it covers by stable key or invariant id\n" +
		"(from aviator show); the rest are mapped when a run starts.\n" +
		"\n" +
		"Submitting replaces whatever was submitted before. Files the review\n" +
		"already has from an earlier submission are not uploaded again, so\n" +
		"resubmitting after re-running one scenario only sends what changed.\n" +
		"Submitting starts no run; start one with `aviator verify r/<number>`.\n" +
		"Automatic runs (ready for review, approval, queued) wait until a\n" +
		"submission for the PR's head commit is complete. The evidence is bound\n" +
		"to the full commit SHA it was captured against (`commit` in the manifest\n" +
		"or --commit); a run at another commit judges from code instead.\n" +
		"\n" +
		"Manifest reference: " + docsEvidenceSubmit,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		runbookNumber, err := parseRunbookID(args[0])
		if err != nil {
			return err
		}
		loaded, err := loadEvidenceManifest(args[1], evidenceSubmitFlags.Commit)
		if err != nil {
			return err
		}

		client, err := api.NewClient()
		if err != nil {
			return err
		}
		ctx := cmd.Context()
		submission, err := client.SubmitEvidenceManifest(ctx, runbookNumber, loaded.manifest)
		if err != nil {
			return err
		}

		var ids, reused []int
		var failed []string
		for i, scenario := range submission.Scenarios {
			for j, step := range scenario.Steps {
				for k, slot := range step.Evidence {
					path, ok := loaded.pathAt(i, j, k)
					if !ok {
						return errors.Errorf("server returned an upload slot the manifest did not declare (scenario %d, step %d, evidence %d)", i+1, j+1, k+1)
					}
					if slot.AlreadyUploaded {
						reused = append(reused, slot.ID)
						continue
					}
					if err := client.UploadEvidenceFile(ctx, slot, path); err != nil {
						failed = append(failed, err.Error())
						continue
					}
					ids = append(ids, slot.ID)
				}
			}
		}
		var completion *api.CompleteUploadsResponse
		if len(ids) > 0 {
			completion, err = client.CompleteEvidenceUploads(ctx, runbookNumber, ids)
			if err != nil {
				return err
			}
		}

		id := formatRunbookID(submission.RunbookNumber)
		if evidenceSubmitFlags.JSON {
			return printJSON(newEvidenceSubmitJSON(id, submission, completion, reused, failed))
		}
		fmt.Printf("%s Submitted %d scenario(s) for %s at %s\n",
			colors.Success("✓"), len(submission.Scenarios), id, shortSHA(submission.CommitSHA))
		for i, scenario := range submission.Scenarios {
			fmt.Printf("  %s (scenario %d, %d evidence file(s))\n",
				loaded.manifest.Scenarios[i].Summary, scenario.ID, countSlots(scenario))
		}
		uploaded := 0
		if completion != nil {
			uploaded = len(completion.UploadedEvidenceIDs)
		}
		fmt.Printf("  Uploaded: %d", uploaded)
		if len(reused) > 0 {
			fmt.Printf(", unchanged since the last submission: %d", len(reused))
		}
		if completion != nil {
			if n := len(completion.UnknownEvidenceIDs); n > 0 {
				fmt.Printf(", replaced by another submission: %d", n)
			}
			if n := len(completion.MissingEvidenceIDs); n > 0 {
				fmt.Printf(", not in storage: %d", n)
			}
			if n := len(completion.RejectedEvidenceIDs); n > 0 {
				fmt.Printf(", rejected as empty or over 10 MB: %d", n)
			}
		}
		fmt.Println()
		for _, msg := range failed {
			fmt.Printf("  %s %s\n", colors.Warning("failed:"), msg)
		}
		if len(failed) > 0 || (completion != nil && (len(completion.UnknownEvidenceIDs) > 0 || len(completion.MissingEvidenceIDs) > 0 || len(completion.RejectedEvidenceIDs) > 0)) {
			return errors.New("some evidence did not upload; fix the files and submit the manifest again")
		}
		fmt.Printf("  %s\n", colors.Faint("Start a run with: aviator verify "+id))
		return nil
	},
}

func init() {
	f := evidenceSubmitCmd.Flags()
	f.StringVar(&evidenceSubmitFlags.Commit, "commit", "",
		"full commit SHA the evidence was captured against (overrides the manifest's commit)")
	f.BoolVar(&evidenceSubmitFlags.JSON, "json", false,
		"print the result as a single JSON object instead of the human summary")
	evidenceCmd.AddCommand(evidenceSubmitCmd)
}

// manifestFile is the on-disk manifest shape: the API shape plus a file path
// per evidence entry, which the CLI turns into a content type and size.
type manifestFile struct {
	Commit    string             `json:"commit"`
	Scenarios []manifestScenario `json:"scenarios"`
}

type manifestScenario struct {
	Summary  string              `json:"summary"`
	Steps    []manifestStep      `json:"steps"`
	Criteria []manifestCriterion `json:"criteria"`
}

type manifestCriterion struct {
	StableKey           *string `json:"stable_key"`
	BaselineInvariantID *int    `json:"baseline_invariant_id"`
}

type manifestStep struct {
	Text     string             `json:"text"`
	Evidence []manifestEvidence `json:"evidence"`
}

type manifestEvidence struct {
	Type  string `json:"type"`
	File  string `json:"file"`
	Label string `json:"label"`
}

// loadedManifest is the API manifest plus the local path behind each upload
// slot, indexed the way the server answers (scenario, step, evidence).
type loadedManifest struct {
	manifest api.EvidenceManifest
	paths    [][][]string
}

func (m *loadedManifest) pathAt(scenario, step, evidence int) (string, bool) {
	if scenario >= len(m.paths) || step >= len(m.paths[scenario]) || evidence >= len(m.paths[scenario][step]) {
		return "", false
	}
	return m.paths[scenario][step][evidence], true
}

// contentTypes maps evidence file extensions to the content type the server
// accepts for them; screenshots are images, everything else is text or JSON.
var contentTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".json": "application/json",
	".txt":  "text/plain",
	".log":  "text/plain",
	".html": "text/html",
	".htm":  "text/html",
}

func loadEvidenceManifest(path, commit string) (*loadedManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read %s", path)
	}
	var file manifestFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, errors.Wrapf(err, "failed to parse %s as JSON", path)
	}
	if commit == "" {
		commit = file.Commit
	}
	commit = strings.ToLower(strings.TrimSpace(commit))
	if commit == "" {
		return nil, errors.New("no commit: set `commit` in the manifest or pass --commit with the full SHA the evidence was captured against")
	}
	if !fullSHA.MatchString(commit) {
		return nil, errors.Errorf("commit %q must be the full 40-character SHA", commit)
	}
	if len(file.Scenarios) == 0 {
		return nil, errors.New("the manifest lists no scenarios")
	}

	base := filepath.Dir(path)
	loaded := &loadedManifest{manifest: api.EvidenceManifest{CommitSHA: commit}}
	for si, scenario := range file.Scenarios {
		if strings.TrimSpace(scenario.Summary) == "" {
			return nil, errors.Errorf("scenario %d needs a summary", si+1)
		}
		apiScenario := api.ManifestScenario{Summary: scenario.Summary}
		for ci, criterion := range scenario.Criteria {
			if (criterion.StableKey == nil) == (criterion.BaselineInvariantID == nil) {
				return nil, errors.Errorf("scenario %d criteria %d needs exactly one of stable_key or baseline_invariant_id", si+1, ci+1)
			}
			apiScenario.Criteria = append(apiScenario.Criteria, api.ManifestCriterion{
				StableKey:           criterion.StableKey,
				BaselineInvariantID: criterion.BaselineInvariantID,
			})
		}
		var scenarioPaths [][]string
		for ti, step := range scenario.Steps {
			if strings.TrimSpace(step.Text) == "" {
				return nil, errors.Errorf("scenario %d step %d needs text", si+1, ti+1)
			}
			apiStep := api.ManifestStep{Text: step.Text}
			var stepPaths []string
			for ei, item := range step.Evidence {
				where := fmt.Sprintf("scenario %d step %d evidence %d", si+1, ti+1, ei+1)
				if item.File == "" {
					return nil, errors.Errorf("%s needs a file", where)
				}
				if strings.TrimSpace(item.Label) == "" {
					return nil, errors.Errorf("%s needs a label saying what the file shows", where)
				}
				contentType, ok := contentTypes[strings.ToLower(filepath.Ext(item.File))]
				if !ok {
					return nil, errors.Errorf("%s: unsupported file type %q (use png, jpg, webp, json, txt, log, or html)", where, filepath.Ext(item.File))
				}
				full := item.File
				if !filepath.IsAbs(full) {
					full = filepath.Join(base, full)
				}
				info, err := os.Stat(full)
				if err != nil {
					return nil, errors.Wrapf(err, "%s: cannot read %s", where, item.File)
				}
				if info.Size() == 0 {
					return nil, errors.Errorf("%s: %s is empty", where, item.File)
				}
				digest, err := fileSHA256(full)
				if err != nil {
					return nil, errors.Wrapf(err, "%s: cannot read %s", where, item.File)
				}
				apiStep.Evidence = append(apiStep.Evidence, api.ManifestEvidence{
					Type:        item.Type,
					Label:       item.Label,
					ContentType: contentType,
					SizeBytes:   info.Size(),
					SHA256:      digest,
				})
				stepPaths = append(stepPaths, full)
			}
			apiScenario.Steps = append(apiScenario.Steps, apiStep)
			scenarioPaths = append(scenarioPaths, stepPaths)
		}
		if len(apiScenario.Steps) == 0 {
			return nil, errors.Errorf("scenario %d needs at least one step", si+1)
		}
		loaded.manifest.Scenarios = append(loaded.manifest.Scenarios, apiScenario)
		loaded.paths = append(loaded.paths, scenarioPaths)
	}
	return loaded, nil
}

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func countSlots(scenario api.SubmittedScenario) int {
	n := 0
	for _, step := range scenario.Steps {
		n += len(step.Evidence)
	}
	return n
}

type evidenceSubmitJSON struct {
	ID                      string   `json:"id"`
	CommitSHA               string   `json:"commit_sha"`
	ScenarioIDs             []int    `json:"scenario_ids"`
	ScenarioRunIDs          []int    `json:"scenario_run_ids"`
	UploadedEvidenceIDs     []int    `json:"uploaded_evidence_ids"`
	ReusedEvidenceIDs       []int    `json:"reused_evidence_ids"`
	UnknownEvidenceIDs      []int    `json:"unknown_evidence_ids"`
	MissingEvidenceIDs      []int    `json:"missing_evidence_ids"`
	RejectedEvidenceIDs     []int    `json:"rejected_evidence_ids"`
	CompletedScenarioRunIDs []int    `json:"completed_scenario_run_ids"`
	UploadErrors            []string `json:"upload_errors"`
}

func newEvidenceSubmitJSON(
	id string, submission *api.EvidenceSubmission, completion *api.CompleteUploadsResponse, reused []int, failed []string,
) evidenceSubmitJSON {
	out := evidenceSubmitJSON{
		ID:                      id,
		CommitSHA:               submission.CommitSHA,
		ScenarioIDs:             []int{},
		ScenarioRunIDs:          []int{},
		UploadedEvidenceIDs:     []int{},
		ReusedEvidenceIDs:       append([]int{}, reused...),
		UnknownEvidenceIDs:      []int{},
		MissingEvidenceIDs:      []int{},
		RejectedEvidenceIDs:     []int{},
		CompletedScenarioRunIDs: []int{},
		UploadErrors:            failed,
	}
	if out.UploadErrors == nil {
		out.UploadErrors = []string{}
	}
	for _, s := range submission.Scenarios {
		out.ScenarioIDs = append(out.ScenarioIDs, s.ID)
		out.ScenarioRunIDs = append(out.ScenarioRunIDs, s.ScenarioRunID)
	}
	if completion != nil {
		out.UploadedEvidenceIDs = append(out.UploadedEvidenceIDs, completion.UploadedEvidenceIDs...)
		out.UnknownEvidenceIDs = append(out.UnknownEvidenceIDs, completion.UnknownEvidenceIDs...)
		out.MissingEvidenceIDs = append(out.MissingEvidenceIDs, completion.MissingEvidenceIDs...)
		out.RejectedEvidenceIDs = append(out.RejectedEvidenceIDs, completion.RejectedEvidenceIDs...)
		out.CompletedScenarioRunIDs = append(out.CompletedScenarioRunIDs, completion.CompletedScenarioRunIDs...)
	}
	return out
}
