// Package auth provides the Bearer-token authentication middleware for Hertz.
package auth

import (
	"context"
	"database/sql"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/juex-ai/chanwire/server/internal/store"
)

// ContextKey is the key used to store the authenticated agent ID in the request context.
const ContextKey = "agent_id"

// Middleware returns a Hertz handler that validates the Bearer token, injects
// the agent ID into the request context, and updates last_active_at.
func Middleware(s *store.Store) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		header := string(ctx.GetHeader("Authorization"))
		token, ok := parseBearerToken(header)
		if !ok {
			ctx.AbortWithStatusJSON(consts.StatusUnauthorized, map[string]string{
				"error": "missing or malformed Authorization header",
			})
			return
		}

		agent, err := s.GetAgentByToken(c, token)
		if err != nil {
			if err == sql.ErrNoRows {
				ctx.AbortWithStatusJSON(consts.StatusUnauthorized, map[string]string{
					"error": "invalid token",
				})
				return
			}
			ctx.AbortWithStatusJSON(consts.StatusInternalServerError, map[string]string{
				"error": "internal error",
			})
			return
		}

		// Update last_active_at (best-effort; don't fail the request on error).
		_ = s.UpdateLastActive(c, agent.ID)

		ctx.Set(ContextKey, agent.ID)
		ctx.Next(c)
	}
}

// GetAgentID extracts the authenticated agent ID from the request context.
// Returns 0 if not present.
func GetAgentID(ctx *app.RequestContext) int64 {
	v, exists := ctx.Get(ContextKey)
	if !exists {
		return 0
	}
	id, _ := v.(int64)
	return id
}

// parseBearerToken extracts the token from "Bearer <token>".
func parseBearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}
