// Package web embeds and serves the chanwire web console.
package web

import (
	"context"
	"embed"
	"io/fs"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

//go:embed dist/*
var assets embed.FS

// Index serves the single-page web console.
func Index() app.HandlerFunc {
	return func(_ context.Context, ctx *app.RequestContext) {
		data, err := fs.ReadFile(assets, "dist/index.html")
		if err != nil {
			ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": "web asset missing"})
			return
		}
		ctx.Data(consts.StatusOK, "text/html; charset=utf-8", data)
	}
}
