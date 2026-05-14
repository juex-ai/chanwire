// Package status formats runtime diagnostics for the CLI and MCP server.
package status

import (
	"fmt"
	"strings"

	"github.com/juex-ai/chanwire/cli/internal/config"
	"github.com/juex-ai/chanwire/cli/internal/store"
)

const labelWidth = 18

// Info is the machine-readable runtime status shape.
type Info struct {
	Version       string `json:"version"`
	WorkDirSource string `json:"work_dir_source"`
	WorkDir       string `json:"work_dir"`
	Endpoint      string `json:"endpoint"`
	AgentName     string `json:"agent_name"`
}

// Version formats build metadata only.
func Version(version, commit string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-*s %s\n", labelWidth, "version:", version)
	fmt.Fprintf(&b, "%-*s %s\n", labelWidth, "commit:", commit)
	return b.String()
}

// Runtime formats runtime configuration diagnostics.
func Runtime(version string) string {
	info := RuntimeInfo(version)
	var b strings.Builder
	fmt.Fprintf(&b, "%-*s %s\n", labelWidth, "version:", info.Version)
	fmt.Fprintf(&b, "%-*s %s\n", labelWidth, fmt.Sprintf("work_dir(%s):", info.WorkDirSource), info.WorkDir)
	fmt.Fprintf(&b, "%-*s %s\n", labelWidth, "endpoint:", info.Endpoint)
	fmt.Fprintf(&b, "%-*s %s\n", labelWidth, "agent_name:", info.AgentName)
	return b.String()
}

// RuntimeInfo returns runtime configuration diagnostics as structured data.
func RuntimeInfo(version string) Info {
	return Info{
		Version:       version,
		WorkDirSource: config.DirSource(),
		WorkDir:       config.Dir(),
		Endpoint:      config.Endpoint(),
		AgentName:     agentName(),
	}
}

func agentName() string {
	info, err := store.Read(config.AgentJSONPath())
	if err != nil {
		return ""
	}
	return info.AgentName
}
