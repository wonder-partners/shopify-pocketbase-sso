package main

import (
	"context"
	"log"
	"os"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"studio-sso/internal/studio"
)

func main() {
	// Avoid SQL debug logs containing profile data, including when using go run.
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDev: false})
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
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
		registerPublicFrontend(e.Router)
		return e.Next()
	})
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
