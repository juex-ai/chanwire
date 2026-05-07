// Package handlers implements all HTTP handlers for the chanwire server.
package handlers

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/juex-ai/chanwire/server/internal/proto"
	"github.com/juex-ai/chanwire/server/internal/store"
)

// Register handles POST /api/v1/agent/register.
// Idempotent: re-registering an existing name returns the original token.
func Register(s *store.Store) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req proto.RegisterRequest
		if err := ctx.BindJSON(&req); err != nil {
			ctx.JSON(consts.StatusBadRequest, proto.ErrorResponse{Error: "invalid request body"})
			return
		}

		req.AgentName = strings.TrimSpace(req.AgentName)
		if req.AgentName == "" {
			ctx.JSON(consts.StatusBadRequest, proto.ErrorResponse{Error: "agent_name is required"})
			return
		}

		agent, err := s.RegisterAgent(c, req.AgentName)
		if err != nil {
			ctx.JSON(consts.StatusInternalServerError, proto.ErrorResponse{Error: "internal error"})
			return
		}

		ctx.JSON(consts.StatusOK, proto.RegisterResponse{
			AgentName: agent.Name,
			Token:     agent.Token,
		})
	}
}
