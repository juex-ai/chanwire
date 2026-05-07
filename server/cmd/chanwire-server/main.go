// Command chanwire-server is the chanwire message relay server.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	hserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/juex-ai/chanwire/server/internal/auth"
	"github.com/juex-ai/chanwire/server/internal/config"
	"github.com/juex-ai/chanwire/server/internal/handlers"
	"github.com/juex-ai/chanwire/server/internal/hub"
	"github.com/juex-ai/chanwire/server/internal/store"
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
	api := srv.Group("/api/v1")

	// No auth.
	api.POST("/agent/register", handlers.Register(s))

	// Auth required.
	authMW := auth.Middleware(s)
	api.GET("/agent/list", authMW, handlers.AgentList(s))
	api.POST("/msg/send", authMW, handlers.MsgSend(s, h))
	api.GET("/ws", authMW, handlers.WSConnect(s, h))

	// Graceful shutdown on SIGINT/SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		fmt.Println("shutting down...")
		ctx := context.Background()
		srv.Shutdown(ctx) //nolint:errcheck
	}()

	srv.Spin()
}
