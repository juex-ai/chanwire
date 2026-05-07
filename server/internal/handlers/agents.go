package handlers

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/juex-ai/chanwire/server/internal/proto"
	"github.com/juex-ai/chanwire/server/internal/store"
)

// AgentList handles GET /api/v1/agent/list.
// Requires auth (middleware already ran and updated last_active_at).
func AgentList(s *store.Store) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		agents, err := s.ListAgents(c)
		if err != nil {
			ctx.JSON(consts.StatusInternalServerError, proto.ErrorResponse{Error: "internal error"})
			return
		}

		infos := make([]proto.AgentInfo, len(agents))
		for i, a := range agents {
			infos[i] = proto.AgentInfo{
				AgentName:    a.Name,
				LastActiveAt: a.LastActiveAt,
			}
		}

		ctx.JSON(consts.StatusOK, proto.AgentListResponse{Agents: infos})
	}
}
