// Package client provides HTTP and WebSocket clients for the chanwire server.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPClient wraps the standard HTTP client with chanwire-specific helpers.
type HTTPClient struct {
	base   string
	token  string
	client *http.Client
}

// NewHTTP returns a new HTTPClient.
func NewHTTP(baseURL, token string) *HTTPClient {
	return &HTTPClient{
		base:  baseURL,
		token: token,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// RegisterRequest is the body for POST /agent/register.
type RegisterRequest struct {
	AgentName string `json:"agent_name"`
}

// RegisterResponse is the 200 OK body from POST /agent/register.
type RegisterResponse struct {
	AgentName string `json:"agent_name"`
	Token     string `json:"token"`
}

// Register calls POST /api/v1/agent/register (no auth required).
func (c *HTTPClient) Register(agentName string) (*RegisterResponse, error) {
	body, err := json.Marshal(RegisterRequest{AgentName: agentName})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.base+"/api/v1/agent/register", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("register request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading register response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server error %d: %s", resp.StatusCode, string(raw))
	}

	var out RegisterResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parsing register response: %w", err)
	}
	return &out, nil
}

// Agent represents one entry in the list response.
type Agent struct {
	AgentName    string  `json:"agent_name"`
	LastActiveAt *int64  `json:"last_active_at"` // unix millis, null if never
}

// ListResponse is the 200 OK body from GET /agent/list.
type ListResponse struct {
	Agents []Agent `json:"agents"`
}

// List calls GET /api/v1/agent/list (auth required).
func (c *HTTPClient) List() (*ListResponse, error) {
	req, err := http.NewRequest(http.MethodGet, c.base+"/api/v1/agent/list", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading list response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server error %d: %s", resp.StatusCode, string(raw))
	}

	var out ListResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parsing list response: %w", err)
	}
	return &out, nil
}

// SendRequest is the body for POST /msg/send.
type SendRequest struct {
	ToAgent string `json:"to_agent"`
	Content string `json:"content"`
}

// SendResponse is the 200 OK body from POST /msg/send.
type SendResponse struct {
	MessageID int64 `json:"message_id"`
	SentAt    int64 `json:"sent_at"`
}

// ErrUnknownAgent is returned when the server responds 404.
type ErrUnknownAgent struct {
	Name string
}

func (e *ErrUnknownAgent) Error() string {
	return fmt.Sprintf("no such agent: %s", e.Name)
}

// Send calls POST /api/v1/msg/send (auth required).
// Returns *ErrUnknownAgent on 404.
func (c *HTTPClient) Send(toAgent, content string) (*SendResponse, error) {
	body, err := json.Marshal(SendRequest{ToAgent: toAgent, Content: content})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.base+"/api/v1/msg/send", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading send response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, &ErrUnknownAgent{Name: toAgent}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server error %d: %s", resp.StatusCode, string(raw))
	}

	var out SendResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parsing send response: %w", err)
	}
	return &out, nil
}
