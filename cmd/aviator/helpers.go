package main

import (
	"os"
	"path/filepath"
	"strings"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/api"
)

// parseRepo splits an "owner/repo" string into a Repository.
func parseRepo(s string) (api.Repository, error) {
	parts := strings.SplitN(strings.TrimSpace(s), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return api.Repository{}, errors.Errorf("invalid repo %q, expected owner/repo", s)
	}
	return api.Repository{Org: parts[0], Name: parts[1]}, nil
}

// readSpecFile loads a spec file from disk, keeping its base name.
func readSpecFile(path string) (*api.SpecFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read spec file %s", path)
	}
	return &api.SpecFile{Filename: filepath.Base(path), Content: string(data)}, nil
}

// collectCriteria merges inline --criteria values with a --criteria-file (one
// per line, blank lines and # comments ignored), trimming each entry.
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
