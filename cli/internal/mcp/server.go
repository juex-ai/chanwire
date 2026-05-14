package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/juex-ai/chanwire/cli/internal/client"
	"github.com/juex-ai/chanwire/cli/internal/config"
	"github.com/juex-ai/chanwire/cli/internal/store"
)

const notRegisteredMarker = "not registered. run:"

// Server holds the state for our MCP server
type Server struct {
	mcpServer    *mcp.Server
	session      *mcp.ServerSession
	sessionMutex sync.Mutex
	transport    *channelTransport

	mu            sync.Mutex
	runCtx        context.Context
	connectCtx    context.Context
	connectCancel context.CancelFunc
	connectWG     sync.WaitGroup
	agentInfo     *store.AgentInfo
	blocked       bool
}

// NewServer creates a new MCP server
func NewServer() *Server {
	return &Server{}
}

// Run starts the MCP server and runs until context is cancelled
func (s *Server) Run(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	defer s.stopConnect()

	s.mu.Lock()
	s.runCtx = ctx
	s.mu.Unlock()

	// Create the MCP server
	s.mcpServer = mcp.NewServer(&mcp.Implementation{
		Name:    "chanwire",
		Version: "0.1.0",
	}, &mcp.ServerOptions{
		Instructions: `You are connected to the chanwire agent-messaging system.

Incoming messages from other agents arrive as <channel source="chanwire" event_type="message"> tags in your context.

## Available tools

- **chanwire_register_agent** — Register yourself (or a named agent) with the chanwire server.
- **chanwire_list_agents** — List all registered agents and their last-active timestamps.
- **chanwire_send_msg** — Send a message to another agent by name.

## Important
- If you see a "not registered" channel event, call chanwire_register_agent before sending messages.
- Messages stream automatically once registered; no polling needed.`,
		Capabilities: &mcp.ServerCapabilities{
			Tools:        &mcp.ToolCapabilities{ListChanged: true},
			Experimental: map[string]any{"claude/channel": map[string]any{}},
		},
		InitializedHandler: s.onInitialized,
		Logger:             slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
	})

	// Register tools
	s.registerTools()

	s.transport = newChannelTransport(&mcp.StdioTransport{})

	// Run the server
	return s.mcpServer.Run(ctx, s.transport)
}

// registerTools registers all our MCP tools
func (s *Server) registerTools() {
	// chanwire_register_agent
	type registerAgentArgs struct {
		AgentName string `json:"agent_name" jsonschema:"The name to register (e.g. \"alice\")"`
	}
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "chanwire_register_agent",
		Description: "Register a named agent with the chanwire server. Saves credentials locally.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args registerAgentArgs) (*mcp.CallToolResult, any, error) {
		return s.handleRegisterAgent(ctx, args.AgentName)
	})

	// chanwire_list_agents
	type listAgentsArgs struct{}
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "chanwire_list_agents",
		Description: "List all registered agents on the chanwire server, with last-active timestamps.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listAgentsArgs) (*mcp.CallToolResult, any, error) {
		return s.handleListAgents(ctx)
	})

	// chanwire_send_msg
	type sendMsgArgs struct {
		ToAgent string `json:"to_agent" jsonschema:"Recipient agent name"`
		Content string `json:"content" jsonschema:"Message text"`
	}
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "chanwire_send_msg",
		Description: "Send a direct message to another agent by name. Returns the message ID on success.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args sendMsgArgs) (*mcp.CallToolResult, any, error) {
		return s.handleSendMsg(ctx, args.ToAgent, args.Content)
	})
}

// onInitialized is called when the client sends the initialized notification
func (s *Server) onInitialized(ctx context.Context, req *mcp.InitializedRequest) {
	s.log("client initialized — starting connect")
	s.setSession(req.Session)
	s.startConnect(ctx)
}

// setSession stores the server session when connected
func (s *Server) setSession(ss *mcp.ServerSession) {
	s.sessionMutex.Lock()
	defer s.sessionMutex.Unlock()
	s.session = ss
}

// getSession retrieves the current server session
func (s *Server) getSession() *mcp.ServerSession {
	s.sessionMutex.Lock()
	defer s.sessionMutex.Unlock()
	return s.session
}

// handleRegisterAgent handles the chanwire_register_agent tool
func (s *Server) handleRegisterAgent(ctx context.Context, agentName string) (*mcp.CallToolResult, any, error) {
	endpoint := config.Endpoint()
	hc := client.NewHTTP(endpoint, "")
	resp, err := hc.Register(agentName)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("error: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	info := &store.AgentInfo{
		AgentName: resp.AgentName,
		Token:     resp.Token,
		Endpoint:  endpoint,
	}
	if err := store.Write(config.AgentJSONPath(), info); err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("error: saving credentials: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	s.mu.Lock()
	s.agentInfo = info
	s.blocked = false
	s.mu.Unlock()

	s.resetConnect(ctx)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("registered: agent_name=%s", resp.AgentName)},
		},
	}, nil, nil
}

// handleListAgents handles the chanwire_list_agents tool
func (s *Server) handleListAgents(ctx context.Context) (*mcp.CallToolResult, any, error) {
	info, err := s.getAgentInfo()
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("error: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	hc := client.NewHTTP(config.Endpoint(), info.Token)
	resp, err := hc.List()
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("error: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	if len(resp.Agents) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "No agents registered."},
			},
		}, nil, nil
	}

	var lines []string
	for _, a := range resp.Agents {
		lastActive := "(never)"
		if a.LastActiveAt != nil {
			lastActive = safeISO(*a.LastActiveAt)
		}
		lines = append(lines, fmt.Sprintf("%s  last_active=%s", a.AgentName, lastActive))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: join(lines, "\n")},
		},
	}, nil, nil
}

// handleSendMsg handles the chanwire_send_msg tool
func (s *Server) handleSendMsg(ctx context.Context, toAgent, content string) (*mcp.CallToolResult, any, error) {
	info, err := s.getAgentInfo()
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("error: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	hc := client.NewHTTP(config.Endpoint(), info.Token)
	resp, err := hc.Send(toAgent, content)
	if err != nil {
		var unknownErr *client.ErrUnknownAgent
		if errors.As(err, &unknownErr) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf("error: no such agent: %s", toAgent)},
				},
				IsError: true,
			}, nil, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("error: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("ok: message_id=%d", resp.MessageID)},
		},
	}, nil, nil
}

// getAgentInfo returns the stored agent info, loading it from disk if needed
func (s *Server) getAgentInfo() (*store.AgentInfo, error) {
	s.mu.Lock()
	if s.agentInfo != nil {
		info := s.agentInfo
		s.mu.Unlock()
		return info, nil
	}
	s.mu.Unlock()

	info, err := store.Read(config.AgentJSONPath())
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.agentInfo = info
	s.mu.Unlock()
	return info, nil
}

// startConnect starts the WebSocket connection to receive messages
func (s *Server) startConnect(ctx context.Context) {
	s.mu.Lock()
	if s.connectCtx != nil {
		s.mu.Unlock()
		return
	}
	baseCtx := s.runCtx
	if baseCtx == nil {
		baseCtx = ctx
	}
	connectCtx, cancel := context.WithCancel(baseCtx)
	s.connectCtx = connectCtx
	s.connectCancel = cancel
	s.mu.Unlock()

	s.connectWG.Add(1)
	go func() {
		defer s.connectWG.Done()
		s.runConnect(connectCtx)
	}()
}

// stopConnect stops the WebSocket connection
func (s *Server) stopConnect() {
	s.mu.Lock()
	cancel := s.connectCancel
	s.connectCtx = nil
	s.connectCancel = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
		s.connectWG.Wait()
	}
}

// resetConnect stops and restarts the WebSocket connection
func (s *Server) resetConnect(ctx context.Context) {
	s.stopConnect()
	s.startConnect(ctx)
}

// runConnect runs the WebSocket connection loop
func (s *Server) runConnect(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		s.mu.Lock()
		blocked := s.blocked
		s.mu.Unlock()

		if blocked {
			s.log("connect blocked — agent not registered")
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}

		s.runConnectOnce(ctx)
	}
}

// runConnectOnce runs one WebSocket connection
func (s *Server) runConnectOnce(ctx context.Context) {
	info, err := s.getAgentInfo()
	if err != nil {
		s.log("connect: %v", err)
		s.setBlocked()
		return
	}

	lineWriter := &lineWriter{
		server: s,
	}

	wsc := client.NewWS(config.Endpoint(), info.Token, nil)
	s.log("starting ws connect")

	if err := wsc.ConnectWithReset(ctx, lineWriter); err != nil {
		if ctx.Err() == nil {
			s.log("connect exited unexpectedly: %v", err)
		}
	}
}

// setBlocked sets the blocked state and sends a notification
func (s *Server) setBlocked() {
	s.mu.Lock()
	s.blocked = true
	s.mu.Unlock()

	s.sendChannelNotification(
		"chanwire: agent not registered. Use the chanwire_register_agent tool to register, then messages will stream automatically.",
		"not_registered",
	)
}

// handleLine handles a single line from the WebSocket connection
func (s *Server) handleLine(line string) {
	trimmed := trimSpace(line)
	if trimmed == "" {
		return
	}

	if contains(trimmed, notRegisteredMarker) {
		s.setBlocked()
		return
	}

	s.sendChannelNotification(trimmed, "message")
}

// sendChannelNotification sends a notification to the client
func (s *Server) sendChannelNotification(content string, eventType string) {
	if s.getSession() == nil || s.transport == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	params := channelNotification{
		Content: content,
		Meta: channelNotificationMeta{
			EventType: eventType,
		},
	}
	if err := s.transport.Notify(ctx, "notifications/claude/channel", params); err != nil {
		s.log("failed to send channel notification: %v", err)
	}
}

// log writes a log message to stderr
func (s *Server) log(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[chanwire] "+format+"\n", args...)
}

// lineWriter is a writer that calls handleLine for each line
type lineWriter struct {
	server *Server
	buf    []byte
}

func (lw *lineWriter) Write(p []byte) (n int, err error) {
	n = len(p)
	for i := 0; i < len(p); i++ {
		if p[i] == '\n' {
			line := string(lw.buf)
			lw.buf = lw.buf[:0]
			lw.server.handleLine(line)
		} else {
			lw.buf = append(lw.buf, p[i])
		}
	}
	return n, nil
}

// Helper functions
func safeISO(ms int64) string {
	t := time.UnixMilli(ms)
	return t.UTC().Format(time.RFC3339)
}

func join(lines []string, sep string) string {
	return strings.Join(lines, sep)
}

func trimSpace(s string) string {
	return strings.TrimSpace(s)
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
