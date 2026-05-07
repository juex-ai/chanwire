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
	b, err := json.Marshal(f)
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

// Hub manages all live WebSocket connections, keyed by agent ID.
type Hub struct {
	mu    sync.RWMutex
	conns map[int64][]*WSConn
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
