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

// FrameHandler receives decoded WebSocket frames.
type FrameHandler func(*Frame)

// Waiter blocks for the requested duration or until ctx is cancelled.
// It returns ctx.Err() if cancelled, otherwise nil. Replaced in tests
// to avoid real waits.
type Waiter func(ctx context.Context, d time.Duration) error

// defaultWaiter sleeps using a real timer that respects ctx cancellation.
func defaultWaiter(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// WSClient manages a persistent WebSocket connection to the server.
type WSClient struct {
	baseURL string
	token   string
	wait    Waiter
	dialer  *websocket.Dialer
}

// NewWS creates a new WSClient. Pass nil waitFn to use the default
// (a context-aware time.NewTimer-based wait).
func NewWS(baseURL, token string, waitFn Waiter) *WSClient {
	fn := waitFn
	if fn == nil {
		fn = defaultWaiter
	}
	return &WSClient{
		baseURL: baseURL,
		token:   token,
		wait:    fn,
		dialer:  websocket.DefaultDialer,
	}
}

// wsURL converts an http(s) base URL to a ws(s) URL for /api/v1/ws.
// A trailing slash on base is stripped to avoid producing "//api/v1/ws".
func wsURL(base string) string {
	base = strings.TrimSuffix(base, "/")
	if strings.HasPrefix(base, "https://") {
		return "wss://" + strings.TrimPrefix(base, "https://") + "/api/v1/ws"
	}
	return "ws://" + strings.TrimPrefix(base, "http://") + "/api/v1/ws"
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

// runOnce dials once and runs the read loop. Return-value paths:
//   - Dial fails with no response: returns the wrapped dial error.
//   - Dial fails with an HTTP response: returns a non-nil error reporting
//     the status code (no reset).
//   - Handshake succeeds and the session later ends: returns *connResetError
//     (caller should reset backoff per spec: "reset to the start of the
//     sequence on a successful WS handshake (101)").
//   - Handshake succeeds and ctx is cancelled before/while reading:
//     returns nil (clean shutdown).
func (c *WSClient) runOnce(ctx context.Context, url string, handle FrameHandler) error {
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

	// conn.ReadMessage blocks and does not natively respect ctx.
	// Force-close the connection on ctx.Done so the reader unblocks.
	// stop is closed when readLoop returns so this watcher exits cleanly.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-stop:
		}
	}()

	return c.readLoop(ctx, conn, handle)
}

func (c *WSClient) readLoop(ctx context.Context, conn *websocket.Conn, handle FrameHandler) error {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			// If ctx is done, treat the error as a clean shutdown.
			if ctx.Err() != nil {
				return nil
			}
			return &connResetError{cause: err}
		}

		var frame Frame
		if err := json.Unmarshal(msg, &frame); err != nil {
			// Skip malformed frames.
			continue
		}

		handle(&frame)
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
	return c.ConnectFramesWithReset(ctx, func(f *Frame) {
		printFrame(w, f)
	})
}

// ConnectFramesWithReset opens a WebSocket, passes incoming frames to handle,
// and reconnects with the same backoff behavior as ConnectWithReset.
func (c *WSClient) ConnectFramesWithReset(ctx context.Context, handle FrameHandler) error {
	if handle == nil {
		handle = func(*Frame) {}
	}

	bo := backoff.New()
	url := wsURL(c.baseURL)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := c.runOnce(ctx, url, handle)

		// Stop if ctx was cancelled (regardless of error).
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// A *connResetError means the handshake succeeded; reset backoff per spec.
		if _, ok := err.(*connResetError); ok {
			bo.Reset()
		}

		delay := bo.Next()

		// Wait for delay or ctx — c.wait returns early if ctx is cancelled,
		// so the reconnect goroutine for the timer is always cleaned up.
		if werr := c.wait(ctx, delay); werr != nil {
			return werr
		}
	}
}
