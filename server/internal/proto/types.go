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
	AgentName    string  `json:"agent_name"`
	LastActiveAt *int64  `json:"last_active_at"` // unix millis, null when never active
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

// Frame is a server-to-client WebSocket message.
// Type is one of "history", "realtime", "history_done".
type Frame struct {
	Type      string `json:"type"`
	MessageID *int64 `json:"message_id,omitempty"`
	FromAgent string `json:"from_agent,omitempty"`
	Content   string `json:"content,omitempty"`
	SentAt    *int64 `json:"sent_at,omitempty"`
}
