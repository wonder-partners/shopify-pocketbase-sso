package main

import (
	"os"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

// This is the host application's test frontend, not part of the Go auth module.
func registerPublicFrontend(r *router.Router[*core.RequestEvent]) {
	files := apis.Static(os.DirFS("./pb_public"), true)
	r.GET("/{path...}", func(e *core.RequestEvent) error {
		path := e.Request.URL.Path
		if path == "/api" || strings.HasPrefix(path, "/api/") || path == "/_" || strings.HasPrefix(path, "/_/") {
			return e.NotFoundError("Not found.", nil)
		}
		e.Response.Header().Set("Cache-Control", "no-store")
		e.Response.Header().Set("Referrer-Policy", "no-referrer")
		e.Response.Header().Set("X-Content-Type-Options", "nosniff")
		e.Response.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		return files(e)
	})
}
