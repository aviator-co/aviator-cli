package config

import (
	"os"
	"path/filepath"
	"strings"

	"emperror.dev/errors"
	"github.com/spf13/viper"
)

// Aviator holds the configuration for talking to the Aviator REST API.
type Aviator struct {
	// APIHost is the base URL of the Aviator API. Defaults to
	// https://api.aviator.co. For on-prem installations point this at your
	// instance (e.g. https://aviator.mycompany.com). No trailing slash.
	APIHost string
	// APIToken authenticates to the Aviator API.
	APIToken string
	// APITokenFromEnv reports that APIToken came from AVIATOR_API_TOKEN rather
	// than from a config file. A static token takes precedence over an OAuth
	// session, so commands use this to name the credential that wins.
	APITokenFromEnv bool `mapstructure:"-"`
}

// Aviator is the loaded global configuration.
var Av = struct {
	Aviator Aviator
}{
	Aviator: Aviator{
		APIHost: "https://api.aviator.co",
	},
}

// Load initializes the configuration values from config files and the
// environment. An optional repository-local config dir overrides the global
// config.
func Load(repoConfigDir string) error {
	if err := loadFromFile(repoConfigDir); err != nil {
		return err
	}
	loadFromEnv()
	return nil
}

func loadFromFile(repoConfigDir string) error {
	config := viper.New()
	config.SetConfigName("config")
	config.AddConfigPath("$XDG_CONFIG_HOME/aviator")
	config.AddConfigPath("$HOME/.config/aviator")
	config.AddConfigPath("$HOME/.aviator")
	if strings.TrimSpace(os.Getenv("AVIATOR_HOME")) != "" {
		config.AddConfigPath("$AVIATOR_HOME")
	}

	if err := config.ReadInConfig(); err != nil {
		if !errors.As(err, &viper.ConfigFileNotFoundError{}) {
			return err
		}
	}

	// Merge a per-repo config file (e.g. .git/av/config.yaml) when present so
	// it can override the global config, mirroring the av CLI behavior. Guard
	// the empty dir, else filepath.Join collapses to a relative "config.yaml"
	// resolved against the CWD.
	if repoConfigDir != "" {
		for _, ext := range viper.SupportedExts {
			fp := filepath.Join(repoConfigDir, "config."+ext)
			if stat, err := os.Stat(fp); err == nil && !stat.IsDir() {
				config.SetConfigFile(fp)
				config.SetConfigType(ext)
				if err := config.MergeInConfig(); err != nil {
					return errors.Wrapf(err, "failed to read %s", fp)
				}
				break
			}
		}
	}

	if err := config.Unmarshal(&Av); err != nil {
		return errors.Wrap(err, "failed to read aviator config")
	}
	return nil
}

func loadFromEnv() {
	if token := os.Getenv("AVIATOR_API_TOKEN"); token != "" {
		Av.Aviator.APIToken = token
		Av.Aviator.APITokenFromEnv = true
	}
	if host := os.Getenv("AVIATOR_API_HOST"); host != "" {
		Av.Aviator.APIHost = host
	}
}
