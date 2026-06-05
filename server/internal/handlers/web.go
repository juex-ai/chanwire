package handlers

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/hertz-contrib/websocket"
	"github.com/juex-ai/chanwire/server/internal/hub"
	"github.com/juex-ai/chanwire/server/internal/proto"
	"github.com/juex-ai/chanwire/server/internal/store"
)

const (
	webMessageLimit = 20
	graphWindow     = 7 * 24 * time.Hour
)

var webUpgrader = websocket.HertzUpgrader{}

// WebState handles GET /api/v1/web/state for the public dashboard.
func WebState(s *store.Store, h *hub.Hub) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		onlineIDs := h.OnlineAgentIDs()
		agents := []proto.WebAgent{}
		edges := []proto.WebEdge{}
		if len(onlineIDs) > 0 {
			allAgents, err := s.ListAgents(c)
			if err != nil {
				ctx.JSON(consts.StatusInternalServerError, proto.ErrorResponse{Error: "internal error"})
				return
			}
			agents = onlineAgents(allAgents, onlineIDs)
			edges, err = webEdges(c, s, allAgents, agents)
			if err != nil {
				ctx.JSON(consts.StatusInternalServerError, proto.ErrorResponse{Error: "internal error"})
				return
			}
		}
		messages, err := s.ListMessages(c, webMessageLimit, 0)
		if err != nil {
			ctx.JSON(consts.StatusInternalServerError, proto.ErrorResponse{Error: "internal error"})
			return
		}
		ctx.JSON(consts.StatusOK, proto.WebStateResponse{
			Agents:   agents,
			Edges:    edges,
			Messages: webMessages(messages),
		})
	}
}

// WebMessages handles GET /api/v1/web/messages?before_id=<id>.
func WebMessages(s *store.Store) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		beforeID := int64(0)
		if raw := ctx.Query("before_id"); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed < 0 {
				ctx.JSON(consts.StatusBadRequest, proto.ErrorResponse{Error: "invalid before_id"})
				return
			}
			beforeID = parsed
		}
		messages, err := s.ListMessages(c, webMessageLimit, beforeID)
		if err != nil {
			ctx.JSON(consts.StatusInternalServerError, proto.ErrorResponse{Error: "internal error"})
			return
		}
		ctx.JSON(consts.StatusOK, proto.WebMessagesResponse{Messages: webMessages(messages)})
	}
}

// WebMsgSend handles unauthenticated web-console sends from the special system sender.
func WebMsgSend(s *store.Store, h *hub.Hub) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req proto.WebSendRequest
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

		toAgent, err := s.GetAgentByName(c, req.ToAgent)
		if err != nil {
			if err == sql.ErrNoRows {
				ctx.JSON(consts.StatusNotFound, proto.ErrorResponse{Error: "unknown agent"})
				return
			}
			ctx.JSON(consts.StatusInternalServerError, proto.ErrorResponse{Error: "internal error"})
			return
		}

		msg, err := s.InsertSystemMessage(c, toAgent.ID, req.Content)
		if err != nil {
			ctx.JSON(consts.StatusInternalServerError, proto.ErrorResponse{Error: "internal error"})
			return
		}

		mid := msg.ID
		sat := msg.CreatedAt
		h.Deliver(toAgent.ID, proto.Frame{
			Type:      "realtime",
			MessageID: &mid,
			FromAgent: msg.FromAgent,
			Content:   msg.Content,
			SentAt:    &sat,
		})
		webMsg := proto.WebMessage{
			MessageID: msg.ID,
			FromAgent: msg.FromAgent,
			ToAgent:   msg.ToAgent,
			Content:   msg.Content,
			SentAt:    msg.CreatedAt,
		}
		h.BroadcastWeb(proto.WebFrame{Type: "message", Message: &webMsg})
		ctx.JSON(consts.StatusOK, proto.SendResponse{MessageID: msg.ID, SentAt: msg.CreatedAt})
	}
}

// WebWS handles GET /api/v1/web/ws for public dashboard realtime events.
func WebWS(h *hub.Hub) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		_ = webUpgrader.Upgrade(ctx, func(conn *websocket.Conn) {
			wsConn := hub.NewWSConn(conn)
			defer wsConn.Close()
			h.RegisterWeb(wsConn)
			defer h.UnregisterWeb(wsConn)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					break
				}
			}
		})
	}
}

func onlineAgents(allAgents []store.Agent, onlineIDs []int64) []proto.WebAgent {
	online := map[int64]bool{}
	for _, id := range onlineIDs {
		online[id] = true
	}
	out := make([]proto.WebAgent, 0, len(online))
	for _, agent := range allAgents {
		if online[agent.ID] {
			out = append(out, proto.WebAgent{
				AgentName:    agent.Name,
				AvatarSeed:   agent.Name,
				LastActiveAt: agent.LastActiveAt,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AgentName < out[j].AgentName })
	return out
}

func webEdges(ctx context.Context, s *store.Store, allAgents []store.Agent, agents []proto.WebAgent) ([]proto.WebEdge, error) {
	onlineNames := map[string]bool{}
	for _, agent := range agents {
		onlineNames[agent.AgentName] = true
	}
	if len(onlineNames) == 0 {
		return []proto.WebEdge{}, nil
	}
	idToName := map[int64]string{}
	for _, agent := range allAgents {
		idToName[agent.ID] = agent.Name
	}
	edges, err := s.ListMessageEdgesSince(ctx, time.Now().Add(-graphWindow).UnixMilli())
	if err != nil {
		return nil, err
	}
	out := make([]proto.WebEdge, 0, len(edges))
	for _, edge := range edges {
		from, fromOK := idToName[edge.FromAgentID]
		to, toOK := idToName[edge.ToAgentID]
		if fromOK && toOK && onlineNames[from] && onlineNames[to] {
			out = append(out, proto.WebEdge{FromAgent: from, ToAgent: to})
		}
	}
	return out, nil
}

func webMessages(messages []store.Message) []proto.WebMessage {
	out := make([]proto.WebMessage, 0, len(messages))
	for _, msg := range messages {
		out = append(out, proto.WebMessage{
			MessageID: msg.ID,
			FromAgent: msg.FromAgent,
			ToAgent:   msg.ToAgent,
			Content:   msg.Content,
			SentAt:    msg.CreatedAt,
		})
	}
	return out
}
