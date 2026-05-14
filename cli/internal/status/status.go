// Package status formats runtime diagnostics for the CLI and MCP server.
package status

import (
	"fmt"
	"strings"

	"github.com/juex-ai/chanwire/cli/internal/config"
	"github.com/juex-ai/chanwire/cli/internal/store"
)

const labelWidth = 18

// Version formats build metadata only.
func Version(version, commit string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-*s %s\n", labelWidth, "version:", version)
	fmt.Fprintf(&b, "%-*s %s\n", labelWidth, "commit:", commit)
	return b.String()
}

// Runtime formats runtime configuration diagnostics.
func Runtime(version string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-*s %s\n", labelWidth, "version:", version)
	fmt.Fprintf(&b, "%-*s %s\n", labelWidth, fmt.Sprintf("work_dir(%s):", config.DirSource()), config.Dir())
	fmt.Fprintf(&b, "%-*s %s\n", labelWidth, "endpoint:", config.Endpoint())
	fmt.Fprintf(&b, "%-*s %s\n", labelWidth, "agent_name:", agentName())
	return b.String()
}

func agentName() string {
	info, err := store.Read(config.AgentJSONPath())
	if err != nil {
		return ""
	}
	return info.AgentName
}
