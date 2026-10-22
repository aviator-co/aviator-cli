// Package update tells users when a newer aviator release is available.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aviator-co/aviator-cli/internal/config"
	"github.com/mattn/go-isatty"
	"golang.org/x/mod/semver"
)

const (
	latestReleaseURL = "https://api.github.com/repos/aviator-co/aviator-cli/releases/latest"
	releaseURLPrefix = "https://github.com/aviator-co/aviator-cli/releases/tag/"
	checkInterval    = 24 * time.Hour
)

// Enabled reports whether this invocation should check for a newer release.
func Enabled() bool {
	if config.Version == config.VersionDev || os.Getenv("AVIATOR_NO_UPDATE_NOTIFIER") != "" || os.Getenv("CI") != "" {
		return false
	}
	return isatty.IsTerminal(os.Stdout.Fd()) && isatty.IsTerminal(os.Stderr.Fd())
}

// Notice returns the message to show when a newer release exists, or "".
func Notice() string {
	latest := latestVersion()
	if !semver.IsValid(latest) || semver.Compare(base(config.Version), base(latest)) >= 0 {
		return ""
	}
	how := releaseURLPrefix + latest
	if formula := brewFormula(); formula != "" {
		how = "To upgrade, run:\nbrew update && brew upgrade aviator-co/tap/" + formula
	}
	return fmt.Sprintf("A new version of aviator is available: %s → %s\n%s", config.Version, latest, how)
}

func base(v string) string {
	return strings.TrimSuffix(v, semver.Prerelease(v))
}

// latestVersion returns the newest release tag, trying GitHub at most once per checkInterval even when a try never finishes.
func latestVersion() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	cacheFile := filepath.Join(dir, "aviator", "latest-release")
	if stat, err := os.Stat(cacheFile); err == nil && time.Since(stat.ModTime()) < checkInterval {
		data, _ := os.ReadFile(cacheFile)
		return string(data)
	}
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		return ""
	}
	if err := os.WriteFile(cacheFile, nil, 0o600); err != nil {
		return ""
	}
	latest := fetchLatestVersion()
	_ = os.WriteFile(cacheFile, []byte(latest), 0o600)
	return latest
}

func fetchLatestVersion() string {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return ""
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	var release struct {
		TagName string `json:"tag_name"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&release) != nil {
		return ""
	}
	return release.TagName
}

// brewFormula returns the Homebrew formula this binary was installed from, or "" if none.
func brewFormula() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}
	_, keg, ok := strings.Cut(exe, "/Cellar/")
	if !ok {
		return ""
	}
	formula, _, _ := strings.Cut(keg, "/")
	return formula
}
