package main

import (
	"bytes"
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

var evidenceSubmitCmd = &cobra.Command{
	Use:   "submit <id> <manifest>",
	Short: "Submit scenarios and evidence from your own run of the change (e.g. aviator evidence submit r/123 evidence.json)",
	Long: "Upload the scenarios you ran yourself and the evidence each step\n" +
		"produced. Aviator judges that evidence instead of collecting its own.\n" +
		"Any session takes a submission; one created with --external-evidence\n" +
		"is a draft that waits for its first one.\n" +
		"\n" +
		"The manifest is JSON. Each scenario has a summary and steps;\n" +
		"each step may list evidence files with a type (screenshot, dom_snapshot,\n" +
		"console_log, api_response, network_request, client_storage) and a label\n" +
		"saying what the file shows. Paths are relative to the manifest. A\n" +
		"scenario may pin the criteria it covers by stable key or invariant id\n" +
		"(from aviator show); the rest are mapped when a run starts.\n" +
		"\n" +
		"Submitting replaces whatever was submitted before. Files already\n" +
		"in the submission it replaces, or in one a run judged, are not\n" +
		"uploaded again, so\n" +
		"resubmitting after re-running one scenario only sends what changed.\n" +
		"Once every file is uploaded, a draft session becomes active and a run\n" +
		"starts when the evidence is for the PR's head commit. Until then runs\n" +
		"wait: automatic ones are held, and `aviator verify r/<number>` is\n" +
		"refused unless you pass --force. The evidence is bound\n" +
		"to the full commit SHA it was captured against (`commit_sha` in the manifest\n" +
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

		var ids []int
		var files []*submittedFile
		byID := map[int]*submittedFile{}
		for i, scenario := range submission.Scenarios {
			for j, step := range scenario.Steps {
				for k, slot := range step.Evidence {
					path, ok := loaded.pathAt(i, j, k)
					if !ok {
						return errors.Errorf("server returned an upload slot the manifest did not declare (scenario %d, step %d, evidence %d)", i+1, j+1, k+1)
					}
					file := &submittedFile{Path: path, EvidenceID: slot.ID, Status: fileNotConfirmed}
					files = append(files, file)
					byID[slot.ID] = file
					if slot.AlreadyUploaded {
						file.Status = fileReused
						continue
					}
					if err := client.UploadEvidenceFile(ctx, slot, path); err != nil {
						file.Status = fileUploadFailed
						file.Error = err.Error()
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
			for status, evidenceIDs := range map[string][]int{
				fileUploaded: completion.UploadedEvidenceIDs,
				fileUnknown:  completion.UnknownEvidenceIDs,
				fileMissing:  completion.MissingEvidenceIDs,
				fileRejected: completion.RejectedEvidenceIDs,
			} {
				for _, evidenceID := range evidenceIDs {
					if file, ok := byID[evidenceID]; ok {
						file.Status = status
					}
				}
			}
		}

		id := formatRunbookID(submission.RunbookNumber)
		counts := map[string]int{}
		for _, file := range files {
			counts[file.Status]++
		}
		incomplete := counts[fileUploaded]+counts[fileReused] != len(files)
		errIncomplete := errors.New("some evidence did not upload; fix the files and submit the manifest again")
		if evidenceSubmitFlags.JSON {
			if err := printJSON(newEvidenceSubmitJSON(id, submission, completion, files)); err != nil {
				return err
			}
			if incomplete {
				return errIncomplete
			}
			return nil
		}
		fmt.Printf("%s Submitted %d scenario(s) for %s at %s\n",
			colors.Success("✓"), len(submission.Scenarios), id, shortSHA(submission.CommitSHA))
		for i, scenario := range submission.Scenarios {
			fmt.Printf("  %s (scenario %d, %d evidence file(s))\n",
				loaded.manifest.Scenarios[i].Summary, scenario.ID, countSlots(scenario))
		}
		fmt.Printf("  Uploaded: %d", counts[fileUploaded])
		if n := counts[fileReused]; n > 0 {
			fmt.Printf(", unchanged since the last submission: %d", n)
		}
		if n := counts[fileUnknown]; n > 0 {
			fmt.Printf(", replaced by another submission: %d", n)
		}
		if n := counts[fileMissing]; n > 0 {
			fmt.Printf(", not in storage: %d", n)
		}
		if n := counts[fileRejected]; n > 0 {
			fmt.Printf(", rejected: %d", n)
		}
		fmt.Println()
		for _, file := range files {
			switch file.Status {
			case fileUploadFailed:
				fmt.Printf("  %s %s: %s\n", colors.Warning("failed:"), file.Path, file.Error)
			case fileMissing, fileNotConfirmed:
				fmt.Printf("  %s %s\n", colors.Warning("not in storage:"), file.Path)
			case fileUnknown:
				fmt.Printf("  %s %s\n", colors.Warning("replaced by another submission:"), file.Path)
			case fileRejected:
				fmt.Printf("  %s %s (empty, over the size or pixel limit, or not matching its sha256)\n",
					colors.Warning("rejected:"), file.Path)
			}
		}
		if incomplete {
			return errIncomplete
		}
		if runID := startedRunID(submission, completion); runID != nil {
			fmt.Printf("  Verification started (run %d). Follow it with: aviator results %s\n", *runID, id)
		} else {
			fmt.Printf("  %s\n", colors.Faint("No run started: one starts when evidence is complete for the pull request's head commit."))
		}
		return nil
	},
}

func init() {
	f := evidenceSubmitCmd.Flags()
	f.StringVar(&evidenceSubmitFlags.Commit, "commit", "",
		"full commit SHA the evidence was captured against (overrides the manifest's commit_sha)")
	f.BoolVar(&evidenceSubmitFlags.JSON, "json", false,
		"print the result as a single JSON object instead of the human summary")
	evidenceCmd.AddCommand(evidenceSubmitCmd)
}

// manifestFile is the on-disk manifest shape: the API shape plus a file path
// per evidence entry, which the CLI turns into a content type and size.
type manifestFile struct {
	CommitSHA string             `json:"commit_sha"`
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
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return nil, errors.Wrapf(err, "failed to parse %s as a JSON manifest", path)
	}
	if commit == "" {
		commit = file.CommitSHA
	}
	commit = strings.ToLower(strings.TrimSpace(commit))
	if commit == "" {
		return nil, errors.New("no commit: set `commit_sha` in the manifest or pass --commit with the full SHA the evidence was captured against")
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

const (
	fileUploaded     = "uploaded"
	fileReused       = "reused"
	fileUploadFailed = "upload_failed"
	fileMissing      = "missing"
	fileRejected     = "rejected"
	fileUnknown      = "unknown"
	fileNotConfirmed = "not_confirmed"
)

// submittedFile is what became of one evidence file the manifest declared.
type submittedFile struct {
	Path       string `json:"path"`
	EvidenceID int    `json:"evidence_id"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

type evidenceSubmitJSON struct {
	ID             string           `json:"id"`
	CommitSHA      string           `json:"commit_sha"`
	ScenarioIDs    []int            `json:"scenario_ids"`
	ScenarioRunIDs []int            `json:"scenario_run_ids"`
	Files          []*submittedFile `json:"files"`
	StartedRunID   *int             `json:"started_run_id"`
}

// startedRunID is the run the submission started: from the confirm call, or
// from the submission itself when every file was already uploaded.
func startedRunID(submission *api.EvidenceSubmission, completion *api.CompleteUploadsResponse) *int {
	if completion != nil && completion.StartedRunID != nil {
		return completion.StartedRunID
	}
	return submission.StartedRunID
}

func newEvidenceSubmitJSON(
	id string, submission *api.EvidenceSubmission, completion *api.CompleteUploadsResponse, files []*submittedFile,
) evidenceSubmitJSON {
	out := evidenceSubmitJSON{
		ID:             id,
		CommitSHA:      submission.CommitSHA,
		ScenarioIDs:    []int{},
		ScenarioRunIDs: []int{},
		Files:          append([]*submittedFile{}, files...),
		StartedRunID:   startedRunID(submission, completion),
	}
	for _, s := range submission.Scenarios {
		out.ScenarioIDs = append(out.ScenarioIDs, s.ID)
		out.ScenarioRunIDs = append(out.ScenarioRunIDs, s.ScenarioRunID)
	}
	return out
}
