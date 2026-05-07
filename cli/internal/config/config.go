// Package config resolves environment-based configuration for the CLI.
package config

import (
	"os"
	"path/filepath"
)

const (
	defaultEndpoint = "http://127.0.0.1:12306"
)

// Endpoint returns the active server base URL from CHANWIRE_ENDPOINT,
// falling back to the default.
func Endpoint() string {
	if v := os.Getenv("CHANWIRE_ENDPOINT"); v != "" {
		return v
	}
	return defaultEndpoint
}

// Dir returns the chanwire data directory from CHANWIRE_DIR,
// falling back to $HOME/.chanwire.
func Dir() string {
	if v := os.Getenv("CHANWIRE_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory if home is unavailable.
		return ".chanwire"
	}
	return filepath.Join(home, ".chanwire")
}

// AgentJSONPath returns the full path to agent.json.
func AgentJSONPath() string {
	return filepath.Join(Dir(), "agent.json")
}
