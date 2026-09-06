package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// parseRunbookID resolves a runbook/verify session ID as displayed to users
// ("r/123"), a bare number ("123"), or a session URL (".../r/123").
func parseRunbookID(arg string) (int, error) {
	s := strings.TrimSpace(arg)
	if strings.Contains(s, "://") {
		if i := strings.LastIndex(s, "/r/"); i >= 0 {
			s = strings.TrimSuffix(s[i+len("/r/"):], "/")
		}
	}
	s = strings.TrimPrefix(s, "r/")
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, errors.Errorf("invalid runbook ID %q, expected r/<number>", arg)
	}
	return n, nil
}

// formatRunbookID renders a runbook number in the user-facing r/<number> form.
func formatRunbookID(runbookNumber int) string {
	return fmt.Sprintf("r/%d", runbookNumber)
}

// parseRepo splits an "owner/repo" string into a Repository.
func parseRepo(s string) (api.Repository, error) {
	parts := strings.SplitN(strings.TrimSpace(s), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return api.Repository{}, errors.Errorf("invalid repo %q, expected owner/repo", s)
	}
	return api.Repository{Org: parts[0], Name: parts[1]}, nil
}

// parseRepos parses each owner/repo entry.
func parseRepos(entries []string) ([]api.Repository, error) {
	repos := make([]api.Repository, 0, len(entries))
	for _, entry := range entries {
		repo, err := parseRepo(entry)
		if err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}
	return repos, nil
}

// readSpecFile loads a spec file from disk, keeping its base name.
func readSpecFile(path string) (*api.SpecFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read spec file %s", path)
	}
	return &api.SpecFile{Filename: filepath.Base(path), Content: string(data)}, nil
}

// registerCriteriaFlags registers the --criteria/--criteria-file pair on cmd
// and marks them mutually exclusive, so every command takes them identically.
func registerCriteriaFlags(cmd *cobra.Command, criteria *[]string, criteriaFile *string) {
	f := cmd.Flags()
	f.StringArrayVar(criteria, "criteria", nil, "acceptance criterion (repeatable)")
	f.StringVar(criteriaFile, "criteria-file", "", "file with one acceptance criterion per line")
	cmd.MarkFlagsMutuallyExclusive("criteria", "criteria-file")
}

// registerBodyFlags registers a --body/--body-file pair on fs, the inline-or-
// file shape registerCriteriaFlags uses for criteria. Callers mark the pair
// mutually exclusive on their command, since that lives on cobra, not pflag.
func registerBodyFlags(fs *pflag.FlagSet, body, bodyFile *string, help string) {
	fs.StringVar(body, "body", "", help)
	fs.StringVar(bodyFile, "body-file", "", "read the body from a file instead")
}

// collectBody returns the body from --body or --body-file, trimming
// surrounding whitespace.
func collectBody(inline, file string) (string, error) {
	if file == "" {
		return strings.TrimSpace(inline), nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "", errors.Wrapf(err, "failed to read body file %s", file)
	}
	return strings.TrimSpace(string(data)), nil
}

// collectCriteria returns the criteria from exactly one source — the inline
// --criteria values, or a --criteria-file (one per line, blank lines and #
// comments ignored) — trimming each entry. registerCriteriaFlags guarantees
// the sources are mutually exclusive.
func collectCriteria(inline []string, file string) ([]string, error) {
	var out []string
	for _, c := range inline {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read criteria file %s", file)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
				out = append(out, line)
			}
		}
	}
	return out, nil
}
