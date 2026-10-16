package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"studio-sso/internal/studio"
)

func main() {
	// Avoid SQL debug logs containing profile data, including when using go run.
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDev: false})
	var publicDir string
	app.RootCmd.PersistentFlags().StringVar(&publicDir, "publicDir", "./pb_public", "directory containing the test frontend, relative to the working directory")
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		publicPath, err := filepath.Abs(publicDir)
		if err != nil {
			return err
		}
		cfg := studio.Config{
			ShopURL:           os.Getenv("SHOPIFY_SHOP_URL"),
			RedirectURL:       os.Getenv("SHOPIFY_REDIRECT_URL"),
			ClientID:          os.Getenv("SHOPIFY_CUSTOMER_CLIENT_ID"),
			ClientSecret:      os.Getenv("SHOPIFY_CUSTOMER_CLIENT_SECRET"),
			AllowLoopbackHTTP: os.Getenv("SSO_ALLOW_LOOPBACK_HTTP") == "1",
		}
		s, err := studio.New(context.Background(), app, cfg)
		if err != nil {
			return err
		}
		s.Register(e.Router)
		registerFrontend(e, publicPath)
		return e.Next()
	})
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

// Static files belong to this host, not to the Shopify authentication package.
func registerFrontend(e *core.ServeEvent, publicDir string) {
	files := os.DirFS(publicDir)
	headers := func(e *core.RequestEvent) error {
		e.Response.Header().Set("Cache-Control", "no-store")
		e.Response.Header().Set("Referrer-Policy", "no-referrer")
		e.Response.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'sha256-YjjOxNiFu7bE8Zns5JaPDOQ3Fomcy/V1h2oT40EgXWc=' https://cdn.jsdelivr.net/npm/pocketbase@0.28.1/dist/pocketbase.es.mjs; style-src 'sha256-yei8oNOF9sHbF446OM3MLvsCf260AGn0UerMMalr/cI='; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		return e.Next()
	}
	// Only this callback gets the index page. Unknown API/admin paths stay 404.
	e.Router.GET("/auth/callback", func(e *core.RequestEvent) error {
		return e.FileFS(files, "index.html")
	}).BindFunc(headers).Bind(apis.SkipSuccessActivityLog())
	static := apis.Static(files, false)
	e.Router.GET("/{path...}", func(e *core.RequestEvent) error {
		path := e.Request.URL.Path
		for _, prefix := range []string{"/api", "/_", "/studio"} {
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				return e.NotFoundError("Not found.", nil)
			}
		}
		return static(e)
	}).BindFunc(headers)
}
