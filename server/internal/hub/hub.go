// Package hub implements the in-memory message hub and WebSocket connection wrapper.
package hub

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/hertz-contrib/websocket"
	"github.com/juex-ai/chanwire/server/internal/proto"
)

// WSConn wraps a single WebSocket connection. Writes are serialised by mu;
// the closed flag (also under mu) lets late deliveries from the hub be
// dropped safely once the connection's hijack handler is on its way out.
type WSConn struct {
	mu     sync.Mutex
	conn   *websocket.Conn
	closed bool
}

// errClosed is returned by WriteFrame when the connection has been Closed.
var errClosed = errors.New("ws conn closed")

// NewWSConn wraps a websocket.Conn.
func NewWSConn(c *websocket.Conn) *WSConn {
	return &WSConn{conn: c}
}

// WriteFrame serialises frame as JSON and sends it as a single WebSocket text
// message. If Close has been called it returns errClosed without touching the
// underlying connection — important to avoid racing the Hertz engine when it
// recycles the hijacked connection after the WS handler returns.
func (w *WSConn) WriteFrame(f proto.Frame) error {
	return w.writeJSON(f)
}

// WriteWebFrame serialises a public web-console frame as JSON.
func (w *WSConn) WriteWebFrame(f proto.WebFrame) error {
	return w.writeJSON(f)
}

func (w *WSConn) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errClosed
	}
	return w.conn.WriteMessage(websocket.TextMessage, b)
}

// Close marks the connection as closed so subsequent WriteFrame calls become
// no-ops. It does NOT close the underlying websocket.Conn; the Hertz hijack
// handler is responsible for that lifecycle. This MUST be called before the
// hijack handler returns so the hub's references stop touching the underlying
// connection before the engine recycles it.
func (w *WSConn) Close() {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
}

// Hub manages all live WebSocket connections, keyed by agent ID, plus public
// web-console observers that need a global realtime feed.
type Hub struct {
	mu       sync.RWMutex
	conns    map[int64][]*WSConn
	webConns []*WSConn
}

// New creates an empty Hub.
func New() *Hub {
	return &Hub{conns: make(map[int64][]*WSConn)}
}

// Register adds conn to the set of connections for agentID.
func (h *Hub) Register(agentID int64, conn *WSConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns[agentID] = append(h.conns[agentID], conn)
}

// Unregister removes conn from the set for agentID.
func (h *Hub) Unregister(agentID int64, conn *WSConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns := h.conns[agentID]
	updated := conns[:0]
	for _, c := range conns {
		if c != conn {
			updated = append(updated, c)
		}
	}
	if len(updated) == 0 {
		delete(h.conns, agentID)
	} else {
		h.conns[agentID] = updated
	}
}

// RegisterWeb adds a public web-console observer connection.
func (h *Hub) RegisterWeb(conn *WSConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.webConns = append(h.webConns, conn)
}

// UnregisterWeb removes a public web-console observer connection.
func (h *Hub) UnregisterWeb(conn *WSConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	updated := h.webConns[:0]
	for _, c := range h.webConns {
		if c != conn {
			updated = append(updated, c)
		}
	}
	for i := len(updated); i < len(h.webConns); i++ {
		h.webConns[i] = nil
	}
	h.webConns = updated
}

// OnlineAgentIDs returns agent IDs that currently have one or more live client
// WebSocket connections.
func (h *Hub) OnlineAgentIDs() []int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]int64, 0, len(h.conns))
	for id := range h.conns {
		ids = append(ids, id)
	}
	return ids
}

// Deliver sends frame to all live connections for agentID.
// Connections that fail are silently skipped (they will clean up on disconnect).
func (h *Hub) Deliver(agentID int64, frame proto.Frame) {
	h.mu.RLock()
	conns := make([]*WSConn, len(h.conns[agentID]))
	copy(conns, h.conns[agentID])
	h.mu.RUnlock()

	for _, c := range conns {
		_ = c.WriteFrame(frame)
	}
}

// BroadcastWeb sends frame to every connected web-console observer.
func (h *Hub) BroadcastWeb(frame proto.WebFrame) {
	h.mu.RLock()
	conns := make([]*WSConn, len(h.webConns))
	copy(conns, h.webConns)
	h.mu.RUnlock()

	for _, c := range conns {
		_ = c.WriteWebFrame(frame)
	}
}
