// Package store manages the agent.json credential file.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// AgentInfo is the schema for agent.json.
type AgentInfo struct {
	AgentName string `json:"agent_name"`
	Token     string `json:"token"`
	Endpoint  string `json:"endpoint"`
}

// ErrNotRegistered is returned when agent.json is missing.
var ErrNotRegistered = errors.New("not registered. run: chanwire agent register --agent_name <name>")

// Read loads agent.json from the given path. Returns ErrNotRegistered
// if the file does not exist. Returns an error if the file is present
// but cannot be parsed or is missing required fields.
func Read(path string) (*AgentInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotRegistered
		}
		return nil, fmt.Errorf("reading agent.json: %w", err)
	}

	var info AgentInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("parsing agent.json: %w", err)
	}

	if info.AgentName == "" || info.Token == "" {
		return nil, fmt.Errorf("agent.json is missing required fields (agent_name, token)")
	}

	return &info, nil
}

// Write persists AgentInfo to the given path, creating the directory if needed.
func Write(path string, info *AgentInfo) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling agent.json: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing agent.json: %w", err)
	}

	return nil
}
