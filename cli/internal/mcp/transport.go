package mcp

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type channelNotification struct {
	Content string                  `json:"content"`
	Meta    channelNotificationMeta `json:"meta"`
}

type channelNotificationMeta struct {
	EventType string `json:"event_type"`
}

// channelTransport wraps an SDK transport and exposes a small, synchronized
// path for chanwire's Claude Code channel extension notification.
type channelTransport struct {
	base sdkmcp.Transport

	connMu  sync.Mutex
	writeMu sync.Mutex
	conn    *channelConnection
}

func newChannelTransport(base sdkmcp.Transport) *channelTransport {
	return &channelTransport{base: base}
}

func (t *channelTransport) Connect(ctx context.Context) (sdkmcp.Connection, error) {
	conn, err := t.base.Connect(ctx)
	if err != nil {
		return nil, err
	}

	wrapped := &channelConnection{
		inner:   conn,
		writeMu: &t.writeMu,
	}

	t.connMu.Lock()
	t.conn = wrapped
	t.connMu.Unlock()

	return wrapped, nil
}

func (t *channelTransport) Notify(ctx context.Context, method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}

	t.connMu.Lock()
	conn := t.conn
	t.connMu.Unlock()
	if conn == nil {
		return sdkmcp.ErrConnectionClosed
	}

	return conn.Write(ctx, &jsonrpc.Request{
		Method: method,
		Params: raw,
	})
}

type channelConnection struct {
	inner   sdkmcp.Connection
	writeMu *sync.Mutex
}

func (c *channelConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	return c.inner.Read(ctx)
}

func (c *channelConnection) Write(ctx context.Context, msg jsonrpc.Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.inner.Write(ctx, msg)
}

func (c *channelConnection) Close() error {
	return c.inner.Close()
}

func (c *channelConnection) SessionID() string {
	return c.inner.SessionID()
}
