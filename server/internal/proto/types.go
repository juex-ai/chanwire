// Package proto holds request/response shapes and WebSocket frame structs.
package proto

// RegisterRequest is the body for POST /api/v1/agent/register.
type RegisterRequest struct {
	AgentName string `json:"agent_name"`
}

// RegisterResponse is the success body for POST /api/v1/agent/register.
type RegisterResponse struct {
	AgentName string `json:"agent_name"`
	Token     string `json:"token"`
}

// AgentInfo is one entry in the /agent/list response.
type AgentInfo struct {
	AgentName    string `json:"agent_name"`
	LastActiveAt *int64 `json:"last_active_at"` // unix millis, null when never active
}

// AgentListResponse is the success body for GET /api/v1/agent/list.
type AgentListResponse struct {
	Agents []AgentInfo `json:"agents"`
}

// SendRequest is the body for POST /api/v1/msg/send.
type SendRequest struct {
	ToAgent string `json:"to_agent"`
	Content string `json:"content"`
}

// SendResponse is the success body for POST /api/v1/msg/send.
type SendResponse struct {
	MessageID int64 `json:"message_id"`
	SentAt    int64 `json:"sent_at"`
}

// ErrorResponse is the shape used for all error responses.
type ErrorResponse struct {
	Error string `json:"error"`
}

// HistoryMessage is one message inside a one-time WebSocket history batch.
type HistoryMessage struct {
	MessageID int64  `json:"message_id"`
	FromAgent string `json:"from_agent"`
	Content   string `json:"content"`
	SentAt    int64  `json:"sent_at"`
}

// Frame is a server-to-client WebSocket message.
// Type is one of "history_batch", "realtime".
type Frame struct {
	Type      string           `json:"type"`
	MessageID *int64           `json:"message_id,omitempty"`
	FromAgent string           `json:"from_agent,omitempty"`
	Content   string           `json:"content,omitempty"`
	SentAt    *int64           `json:"sent_at,omitempty"`
	Messages  []HistoryMessage `json:"messages,omitempty"`
}

// WebMessage is a message shape for the public web console.
type WebMessage struct {
	MessageID int64  `json:"message_id"`
	FromAgent string `json:"from_agent"`
	ToAgent   string `json:"to_agent"`
	Content   string `json:"content"`
	SentAt    int64  `json:"sent_at"`
}

// WebAgent is an online agent node in the public web console.
type WebAgent struct {
	AgentName    string `json:"agent_name"`
	AvatarSeed   string `json:"avatar_seed"`
	LastActiveAt *int64 `json:"last_active_at"`
}

// WebEdge is a directed recent-message relationship between online agents.
type WebEdge struct {
	FromAgent string `json:"from_agent"`
	ToAgent   string `json:"to_agent"`
}

// WebStateResponse is the initial/refresh data for the web console.
type WebStateResponse struct {
	Agents   []WebAgent   `json:"agents"`
	Edges    []WebEdge    `json:"edges"`
	Messages []WebMessage `json:"messages"`
}

// WebMessagesResponse is a paginated message-list response.
type WebMessagesResponse struct {
	Messages []WebMessage `json:"messages"`
}

// WebSendRequest is the body for POST /api/v1/web/msg/send.
type WebSendRequest struct {
	ToAgent string `json:"to_agent"`
	Content string `json:"content"`
}

// WebFrame is sent to unauthenticated web-console WebSocket subscribers.
type WebFrame struct {
	Type    string      `json:"type"`
	Message *WebMessage `json:"message,omitempty"`
}
