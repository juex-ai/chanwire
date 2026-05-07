// Package version holds build-time metadata injected via ldflags.
package version

// These variables are set at build time via:
//
//	go build -ldflags "-X github.com/juex-ai/chanwire/cli/internal/version.Version=$VERSION -X github.com/juex-ai/chanwire/cli/internal/version.Commit=$COMMIT"
var (
	Version = "dev"
	Commit  = "unknown"
)
