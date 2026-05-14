package handlers

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/hertz-contrib/websocket"
	"github.com/juex-ai/chanwire/server/internal/auth"
	"github.com/juex-ai/chanwire/server/internal/hub"
	"github.com/juex-ai/chanwire/server/internal/proto"
	"github.com/juex-ai/chanwire/server/internal/store"
)

// historyQueryTimeout bounds the DB query that replays a recipient's message
// history at WebSocket-connect time. If the DB stalls, the goroutine should
// be reclaimable instead of hanging forever.
const historyQueryTimeout = 10 * time.Second
const historyBatchLimit = 5

var upgrader = websocket.HertzUpgrader{
	CheckOrigin: func(ctx *app.RequestContext) bool { return true },
}

// WSConnect handles GET /api/v1/ws — upgrades to WebSocket, streams history,
// then serves realtime messages until the client disconnects.
func WSConnect(s *store.Store, h *hub.Hub) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		agentID := auth.GetAgentID(ctx)
		if agentID == 0 {
			ctx.AbortWithStatus(401)
			return
		}

		err := upgrader.Upgrade(ctx, func(conn *websocket.Conn) {
			wsConn := hub.NewWSConn(conn)
			// Mark the wrapper closed before the hijack handler returns
			// so any in-flight hub.Deliver fanouts stop touching the
			// underlying connection before Hertz recycles its hijackConn.
			defer wsConn.Close()

			// 1. Stream recent history as one batch.
			//
			// The Hertz request context is not safe to pass into the
			// upgraded handler (it gets recycled once Upgrade returns),
			// so derive a fresh background context bounded by a
			// reasonable deadline.
			dbCtx, cancel := context.WithTimeout(context.Background(), historyQueryTimeout)
			msgs, err := s.GetRecentMessagesForAgent(dbCtx, agentID, historyBatchLimit)
			cancel()
			if err == nil && len(msgs) > 0 {
				batch := make([]proto.HistoryMessage, 0, len(msgs))
				for _, m := range msgs {
					batch = append(batch, proto.HistoryMessage{
						MessageID: m.ID,
						FromAgent: m.FromAgent,
						Content:   m.Content,
						SentAt:    m.CreatedAt,
					})
				}
				if err := wsConn.WriteFrame(proto.Frame{Type: "history_batch", Messages: batch}); err != nil {
					return
				}
			}

			// 2. Register in hub for realtime delivery.
			h.Register(agentID, wsConn)
			defer h.Unregister(agentID, wsConn)

			// 3. Keep the connection open; drain any client-sent frames (ignored).
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					break
				}
			}
		})
		if err != nil {
			// Upgrade failed (client already received an error response).
			return
		}
	}
}
