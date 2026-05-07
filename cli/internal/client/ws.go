package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/juex-ai/chanwire/cli/internal/backoff"
)

// Frame represents a server-to-client WebSocket frame.
type Frame struct {
	Type      string `json:"type"`
	MessageID *int64 `json:"message_id,omitempty"`
	FromAgent string `json:"from_agent,omitempty"`
	Content   string `json:"content,omitempty"`
	SentAt    *int64 `json:"sent_at,omitempty"`
}

// SleepFunc is the function used to sleep between reconnect attempts.
// Replaced in tests to avoid real waits.
type SleepFunc func(d time.Duration)

// WSClient manages a persistent WebSocket connection to the server.
type WSClient struct {
	baseURL string
	token   string
	sleep   SleepFunc
	dialer  *websocket.Dialer
}

// NewWS creates a new WSClient. Pass nil sleepFn to use the default (time.Sleep).
func NewWS(baseURL, token string, sleepFn SleepFunc) *WSClient {
	fn := sleepFn
	if fn == nil {
		fn = time.Sleep
	}
	return &WSClient{
		baseURL: baseURL,
		token:   token,
		sleep:   fn,
		dialer:  websocket.DefaultDialer,
	}
}

// wsURL converts an http(s) base URL to a ws(s) URL for /api/v1/ws.
func wsURL(base string) string {
	if strings.HasPrefix(base, "https://") {
		return "wss://" + strings.TrimPrefix(base, "https://") + "/api/v1/ws"
	}
	return "ws://" + strings.TrimPrefix(base, "http://") + "/api/v1/ws"
}

// sleepCh returns a channel that closes after the sleep function returns.
func sleepCh(fn SleepFunc, d time.Duration) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		fn(d)
		close(ch)
	}()
	return ch
}

// connResetError is returned by runOnce when the WebSocket handshake
// succeeded (101) but the session subsequently ended. It signals the
// caller to reset the reconnect backoff sequence.
type connResetError struct{ cause error }

func (e *connResetError) Error() string {
	if e.cause == nil {
		return "connection closed"
	}
	return e.cause.Error()
}

// runOnce dials once and runs the read loop.
//   - Returns *connResetError when the handshake succeeded but the session ended
//     (caller should reset backoff — spec: "reset to the start of the sequence
//     on a successful WS handshake (101)").
//   - Returns a plain error when the dial itself failed (no reset).
//   - Returns nil when ctx is cancelled (caller should stop).
func (c *WSClient) runOnce(ctx context.Context, url string, w io.Writer) error {
	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer "+c.token)

	conn, resp, err := c.dialer.DialContext(ctx, url, hdr)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("dial %s: HTTP %d", url, resp.StatusCode)
		}
		return fmt.Errorf("dial %s: %w", url, err)
	}
	defer conn.Close()

	// Handshake succeeded — return via connResetError so the caller resets.
	return c.readLoop(ctx, conn, w)
}

func (c *WSClient) readLoop(ctx context.Context, conn *websocket.Conn, w io.Writer) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			return &connResetError{cause: err}
		}

		var frame Frame
		if err := json.Unmarshal(msg, &frame); err != nil {
			// Skip malformed frames.
			continue
		}

		printFrame(w, &frame)
	}
}

// printFrame writes a single frame to w in the format defined by the spec.
func printFrame(w io.Writer, f *Frame) {
	switch f.Type {
	case "history":
		fmt.Fprintf(w, "[history]  from %s at %s: %s\n", f.FromAgent, formatTS(f.SentAt), f.Content)
	case "realtime":
		fmt.Fprintf(w, "[realtime] from %s at %s: %s\n", f.FromAgent, formatTS(f.SentAt), f.Content)
	case "history_done":
		fmt.Fprintln(w, "-- end of history --")
	}
}

// formatTS converts unix milliseconds to "2006-01-02 15:04:05" (UTC).
func formatTS(ms *int64) string {
	if ms == nil {
		return "(unknown)"
	}
	return time.UnixMilli(*ms).UTC().Format("2006-01-02 15:04:05")
}

// ConnectWithReset opens a WebSocket, prints incoming frames to w, and
// reconnects with exponential backoff on disconnect. Backoff resets to the
// start of the sequence after every successful WS handshake (101). Runs until
// ctx is cancelled.
func (c *WSClient) ConnectWithReset(ctx context.Context, w io.Writer) error {
	bo := backoff.New()
	url := wsURL(c.baseURL)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := c.runOnce(ctx, url, w)

		// Stop if ctx was cancelled (regardless of error).
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// A *connResetError means the handshake succeeded; reset backoff per spec.
		if _, ok := err.(*connResetError); ok {
			bo.Reset()
		}

		delay := bo.Next()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-sleepCh(c.sleep, delay):
		}
	}
}
