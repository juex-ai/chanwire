// Command chanwire-server is the chanwire message relay server.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	hserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/juex-ai/chanwire/server/internal/auth"
	"github.com/juex-ai/chanwire/server/internal/config"
	"github.com/juex-ai/chanwire/server/internal/handlers"
	"github.com/juex-ai/chanwire/server/internal/hub"
	"github.com/juex-ai/chanwire/server/internal/store"
	"github.com/juex-ai/chanwire/server/web"
)

// Build-time metadata injected via -ldflags.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	cfg := config.Load()

	s, err := store.Open(cfg.DB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	h := hub.New()

	addr := "0.0.0.0:" + cfg.Port
	srv := hserver.Default(hserver.WithHostPorts(addr))

	// Log version info.
	fmt.Printf("chanwire-server version=%s commit=%s addr=%s db=%s\n",
		version, commit, addr, cfg.DB)

	// Routes.
	srv.GET("/", web.Index())
	srv.GET("/web", web.Index())
	srv.GET("/settings", web.Index())

	api := srv.Group("/api/v1")

	// No auth.
	api.POST("/agent/register", handlers.Register(s))

	// Auth required.
	authMW := auth.Middleware(s)
	api.GET("/agent/list", authMW, handlers.AgentList(s))
	api.POST("/msg/send", authMW, handlers.MsgSend(s, h))
	api.GET("/ws", authMW, handlers.WSConnect(s, h))

	// Public web console API.
	api.GET("/web/state", handlers.WebState(s, h))
	api.GET("/web/messages", handlers.WebMessages(s))
	api.POST("/web/msg/send", handlers.WebMsgSend(s, h))
	api.GET("/web/settings/agents", handlers.WebSettingsAgents(s, h))
	api.DELETE("/web/settings/agents/:agent_name", handlers.WebSettingsAgentDelete(s, h))
	api.GET("/web/ws", handlers.WebWS(h))

	// Graceful shutdown on SIGINT/SIGTERM, bounded by a 10s deadline so a
	// stuck connection or slow DB cannot hang the process forever.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		fmt.Println("shutting down...")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	srv.Spin()
}
