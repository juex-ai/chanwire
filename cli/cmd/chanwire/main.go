// Command chanwire is the CLI for the chanwire agent-messaging server.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/juex-ai/chanwire/cli/internal/client"
	"github.com/juex-ai/chanwire/cli/internal/config"
	"github.com/juex-ai/chanwire/cli/internal/mcp"
	"github.com/juex-ai/chanwire/cli/internal/store"
)

// Build-time metadata injected via:
//
//	go build -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT"
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var homeDir string

	root := &cobra.Command{
		Use:   "chanwire",
		Short: "chanwire — agent-to-agent messaging CLI",
		// Don't print usage on errors; the sub-commands handle their own.
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return config.SetHomeDir(homeDir)
		},
	}

	root.PersistentFlags().StringVar(&homeDir, "homedir", "", "Base directory for chanwire config; final path is .config/chanwire")

	root.AddCommand(versionCmd())
	root.AddCommand(agentCmd())
	root.AddCommand(msgCmd())
	root.AddCommand(connectCmd())
	root.AddCommand(mcpCmd())

	return root
}

func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the chanwire MCP server (stdio)",
		RunE: func(cmd *cobra.Command, args []string) error {
			srv := mcp.NewServer()
			return srv.Run(cmd.Context())
		},
	}
}

// ── version ──────────────────────────────────────────────────────────────────

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "cli version:      %s\n", version)
			fmt.Fprintf(out, "git commit:       %s\n", commit)
			fmt.Fprintf(out, "CHANWIRE_DIR:     %s\n", config.Dir())
			fmt.Fprintf(out, "endpoint (env):   %s\n", config.Endpoint())

			// Saved endpoint is best-effort — print "(not registered)" if
			// agent.json is missing or unreadable; spec says version is
			// purely diagnostic and must never fail because of it.
			saved := "(not registered)"
			if info, err := store.Read(config.AgentJSONPath()); err == nil {
				if info.Endpoint != "" {
					saved = info.Endpoint
				} else {
					saved = "(unset)"
				}
			}
			fmt.Fprintf(out, "endpoint (saved): %s\n", saved)
			return nil
		},
	}
}

// ── agent ─────────────────────────────────────────────────────────────────────

func agentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage agents",
	}
	cmd.AddCommand(agentRegisterCmd())
	cmd.AddCommand(agentListCmd())
	return cmd
}

func agentRegisterCmd() *cobra.Command {
	var agentName string

	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register this agent with the server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if agentName == "" {
				return fmt.Errorf("--agent_name is required")
			}

			endpoint := config.Endpoint()
			hc := client.NewHTTP(endpoint, "")

			resp, err := hc.Register(agentName)
			if err != nil {
				return fmt.Errorf("registration failed: %w", err)
			}

			info := &store.AgentInfo{
				AgentName: resp.AgentName,
				Token:     resp.Token,
				Endpoint:  endpoint,
			}
			if err := store.Write(config.AgentJSONPath(), info); err != nil {
				return fmt.Errorf("saving credentials: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "registered: agent_name=%s\n", resp.AgentName)
			return nil
		},
	}

	cmd.Flags().StringVar(&agentName, "agent_name", "", "Name to register (required)")
	return cmd
}

// agentListCmd implements `chanwire agent list`.
//
// Default (human) format — two columns, space-padded:
//
//	NAME                  LAST_ACTIVE
//	alice                 2026-05-07 19:42:33
//	bob                   (never)
//
// NAME column is left-padded to width 20. LAST_ACTIVE is either the literal
// string "(never)" or a UTC timestamp formatted "2006-01-02 15:04:05".
//
// With --json, output is one line of JSON matching the wire schema:
//
//	{"agents":[{"agent_name":"alice","last_active_at":1778154123456}, ...]}
//
// The plugin (T5) parses --json output.
func agentListCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := requireToken()
			if err != nil {
				return err
			}

			hc := client.NewHTTP(config.Endpoint(), info.Token)
			resp, err := hc.List()
			if err != nil {
				return fmt.Errorf("listing agents: %w", err)
			}

			out := cmd.OutOrStdout()

			if jsonOut {
				data, err := json.Marshal(resp)
				if err != nil {
					return fmt.Errorf("encoding JSON: %w", err)
				}
				fmt.Fprintln(out, string(data))
				return nil
			}

			fmt.Fprintf(out, "%-20s  %s\n", "NAME", "LAST_ACTIVE")
			for _, a := range resp.Agents {
				lastActive := "(never)"
				if a.LastActiveAt != nil {
					t := time.UnixMilli(*a.LastActiveAt).UTC()
					lastActive = t.Format("2006-01-02 15:04:05")
				}
				fmt.Fprintf(out, "%-20s  %s\n", a.AgentName, lastActive)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit one line of JSON instead of the table")
	return cmd
}

// ── msg ───────────────────────────────────────────────────────────────────────

func msgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "msg",
		Short: "Manage messages",
	}
	cmd.AddCommand(msgSendCmd())
	return cmd
}

func msgSendCmd() *cobra.Command {
	var toAgent, content string

	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send a message to another agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			if toAgent == "" {
				return fmt.Errorf("--to_agent is required")
			}
			if content == "" {
				return fmt.Errorf("--content is required")
			}

			info, err := requireToken()
			if err != nil {
				return err
			}

			hc := client.NewHTTP(config.Endpoint(), info.Token)
			resp, err := hc.Send(toAgent, content)
			if err != nil {
				var unknownErr *client.ErrUnknownAgent
				if errors.As(err, &unknownErr) {
					// Return a plain error so Cobra prints it and exits non-zero;
					// don't call os.Exit directly from inside RunE.
					return fmt.Errorf("no such agent: %s", toAgent)
				}
				return fmt.Errorf("send failed: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "ok: message_id=%d\n", resp.MessageID)
			return nil
		},
	}

	cmd.Flags().StringVar(&toAgent, "to_agent", "", "Recipient agent name (required)")
	cmd.Flags().StringVar(&content, "content", "", "Message content (required)")
	return cmd
}

// ── connect ───────────────────────────────────────────────────────────────────

func connectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "connect",
		Short: "Connect to the server and stream incoming messages",
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := requireToken()
			if err != nil {
				return err
			}

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			wsc := client.NewWS(config.Endpoint(), info.Token, nil)
			if err := wsc.ConnectWithReset(ctx, cmd.OutOrStdout()); err != nil {
				// ctx.Err() (Canceled / DeadlineExceeded) is the normal exit path
				// when the user hits Ctrl-C; don't surface it as a failure.
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil
				}
				return err
			}
			return nil
		},
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// requireToken loads agent.json. The returned error is propagated up to
// Cobra so the command exits non-zero with the standard error path.
// Callers should simply `return err` when this fails.
func requireToken() (*store.AgentInfo, error) {
	return store.Read(config.AgentJSONPath())
}
