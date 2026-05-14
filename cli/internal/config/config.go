// Package config resolves environment-based configuration for the CLI.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultEndpoint = "http://127.0.0.1:12306"
)

var (
	homeDirConfigured bool
	configDir         string
)

// Endpoint returns the active server base URL from CHANWIRE_ENDPOINT,
// falling back to the default.
func Endpoint() string {
	if v := os.Getenv("CHANWIRE_ENDPOINT"); v != "" {
		return v
	}
	return defaultEndpoint
}

// SetHomeDir configures the active chanwire config directory from --homedir.
func SetHomeDir(homeDir string) error {
	homeDirConfigured = false
	configDir = ""
	if homeDir == "" {
		return nil
	}

	dir, err := ResolveDir(homeDir)
	if err != nil {
		return err
	}
	homeDirConfigured = true
	configDir = dir
	return nil
}

// ResolveDir returns the chanwire config directory from --homedir,
// CHANWIRE_DIR, or the current user's home directory.
func ResolveDir(homeDir string) (string, error) {
	base := homeDir
	rejectParentTraversal := homeDir != ""
	if base == "" {
		base = os.Getenv("CHANWIRE_DIR")
	}
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			base = "."
		} else {
			base = home
		}
	}

	if rejectParentTraversal && hasParentTraversal(base) && !filepath.IsAbs(base) {
		return "", fmt.Errorf("--homedir relative path must not contain '..': %s", base)
	}

	absBase, err := absolutePath(base)
	if err != nil {
		return "", err
	}
	return appendConfigChanwire(absBase), nil
}

// Dir returns the active chanwire config directory.
func Dir() string {
	if homeDirConfigured {
		return configDir
	}
	dir, err := ResolveDir("")
	if err != nil {
		return filepath.Join(".", ".config", "chanwire")
	}
	return dir
}

// AgentJSONPath returns the full path to agent.json.
func AgentJSONPath() string {
	return filepath.Join(Dir(), "agent.json")
}

func absolutePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving path %s: %w", path, err)
	}
	return abs, nil
}

func appendConfigChanwire(path string) string {
	clean := filepath.Clean(path)
	if filepath.Base(clean) == "chanwire" && filepath.Base(filepath.Dir(clean)) == ".config" {
		return clean
	}
	if filepath.Base(clean) == ".config" {
		return filepath.Join(clean, "chanwire")
	}
	return filepath.Join(clean, ".config", "chanwire")
}

func hasParentTraversal(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}
