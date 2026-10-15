package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
)

func TestPocketBaseServesPublicFrontendAndCallback(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.ResetBootstrapState() })
	r, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	registerPublicFrontend(r)
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	for _, path := range []string{"/", "/auth/callback?code=must-not-echo&state=must-not-echo", "/app.js", "/style.css", "/vendor/pocketbase.es.mjs"} {
		t.Run(path, func(t *testing.T) {
			response, err := http.Get(server.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, _ := io.ReadAll(response.Body)
			if response.StatusCode != 200 || len(body) == 0 {
				t.Fatalf("public resource returned HTTP %d", response.StatusCode)
			}
			if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Referrer-Policy") != "no-referrer" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("missing OAuth frontend security headers")
			}
			if !strings.Contains(response.Header.Get("Content-Security-Policy"), "script-src 'self'") || len(response.Cookies()) != 0 || bytes.Contains(body, []byte("must-not-echo")) {
				t.Fatal("unsafe frontend response")
			}
		})
	}
	for _, path := range []string{"/api/missing", "/api", "/_"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatalf("reserved path %s fell back to HTML", path)
		}
	}
	response, err := http.Get(server.URL + "/api/collections/users/auth-methods")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		t.Fatal("static route replaced a native PocketBase API")
	}
}

func TestPublicFrontendUsesTheDocumentedOAuthFlow(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	frontend, err := os.ReadFile("pb_public/app.js")
	if err != nil {
		t.Fatal(err)
	}
	_, example, found := strings.Cut(string(readme), "<!-- native-redirect-example:start -->")
	if !found {
		t.Fatal("missing documented OAuth example")
	}
	_, example, _ = strings.Cut(example, "```javascript")
	example, _, _ = strings.Cut(example, "```")
	if !strings.Contains(string(frontend), strings.TrimSpace(example)) {
		t.Fatal("public frontend diverged from the example exercised with the SDK")
	}
	for _, forbidden := range []string{"/api/shopify/auth/", "/studio/session", "authStore.save("} {
		if bytes.Contains(frontend, []byte(forbidden)) {
			t.Fatal("public frontend introduced a custom auth flow")
		}
	}
}
