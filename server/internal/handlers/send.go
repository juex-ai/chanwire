package handlers

import (
	"context"
	"database/sql"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/juex-ai/chanwire/server/internal/auth"
	"github.com/juex-ai/chanwire/server/internal/hub"
	"github.com/juex-ai/chanwire/server/internal/proto"
	"github.com/juex-ai/chanwire/server/internal/store"
)

// MsgSend handles POST /api/v1/msg/send.
func MsgSend(s *store.Store, h *hub.Hub) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		// Sender ID and name are already known from the auth middleware,
		// so we don't need an extra SELECT to resolve the sender's name.
		fromAgentID := auth.GetAgentID(ctx)
		fromAgentName := auth.GetAgentName(ctx)

		var req proto.SendRequest
		if err := ctx.BindJSON(&req); err != nil {
			ctx.JSON(consts.StatusBadRequest, proto.ErrorResponse{Error: "invalid request body"})
			return
		}

		if req.ToAgent == "" {
			ctx.JSON(consts.StatusBadRequest, proto.ErrorResponse{Error: "to_agent is required"})
			return
		}
		if req.Content == "" {
			ctx.JSON(consts.StatusBadRequest, proto.ErrorResponse{Error: "content is required"})
			return
		}
		if isSystemAgent(req.ToAgent) {
			ctx.JSON(consts.StatusBadRequest, proto.ErrorResponse{Error: sendToSystemError()})
			return
		}

		// Resolve to_agent by name.
		toAgent, err := s.GetAgentByName(c, req.ToAgent)
		if err != nil {
			if err == sql.ErrNoRows {
				ctx.JSON(consts.StatusNotFound, proto.ErrorResponse{Error: "unknown agent"})
				return
			}
			ctx.JSON(consts.StatusInternalServerError, proto.ErrorResponse{Error: "internal error"})
			return
		}

		msg, err := s.InsertMessage(c, fromAgentID, fromAgentName, toAgent.ID, req.Content)
		if err != nil {
			ctx.JSON(consts.StatusInternalServerError, proto.ErrorResponse{Error: "internal error"})
			return
		}

		// Fan out to any live connections.
		mid := msg.ID
		sat := msg.CreatedAt
		h.Deliver(toAgent.ID, proto.Frame{
			Type:      "realtime",
			MessageID: &mid,
			FromAgent: msg.FromAgent,
			Content:   msg.Content,
			SentAt:    &sat,
		})

		webMsg := webMessage(msg)
		h.BroadcastWeb(proto.WebFrame{Type: "message", Message: &webMsg})

		ctx.JSON(consts.StatusOK, proto.SendResponse{
			MessageID: msg.ID,
			SentAt:    msg.CreatedAt,
		})
	}
}
