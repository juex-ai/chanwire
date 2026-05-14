// Package status formats runtime diagnostics for the CLI and MCP server.
package status

import (
	"fmt"
	"strings"

	"github.com/juex-ai/chanwire/cli/internal/config"
	"github.com/juex-ai/chanwire/cli/internal/store"
)

// Version formats build metadata only.
func Version(version, commit string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "version:         %s\n", version)
	fmt.Fprintf(&b, "commit:          %s\n", commit)
	return b.String()
}

// Runtime formats runtime configuration diagnostics.
func Runtime(version string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "version:         %s\n", version)
	fmt.Fprintf(&b, "%-16s %s\n", fmt.Sprintf("work_dir(%s):", config.DirSource()), config.Dir())
	fmt.Fprintf(&b, "endpoint:        %s\n", config.Endpoint())
	fmt.Fprintf(&b, "agent_name:      %s\n", agentName())
	return b.String()
}

func agentName() string {
	info, err := store.Read(config.AgentJSONPath())
	if err != nil {
		return ""
	}
	return info.AgentName
}
