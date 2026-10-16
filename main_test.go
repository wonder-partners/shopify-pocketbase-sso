package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/pocketbase/pocketbase/ui"
	"studio-sso/internal/mockshop"
	"studio-sso/internal/studio"
)

func frontendFixture(t *testing.T) (string, string, http.Handler) {
	t.Helper()
	api := httptest.NewUnstartedServer(nil)
	shop := httptest.NewUnstartedServer(nil)
	apiURL := "http://" + api.Listener.Addr().String()
	shopURL := "http://" + shop.Listener.Addr().String()
	mock, err := mockshop.New(shopURL, apiURL+"/auth/callback", "", "numeric:")
	if err != nil {
		t.Fatal(err)
	}
	shop.Config.Handler = mock
	shop.Start()
	t.Cleanup(shop.Close)
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir(), DefaultDev: false})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	app.Settings().Logs.MaxDays = 0
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	users.ViewRule = types.Pointer("id = @request.auth.id")
	if err := app.Save(users); err != nil {
		t.Fatal(err)
	}
	s, err := studio.New(context.Background(), app, studio.Config{
		ShopURL: shopURL, RedirectURL: apiURL + "/auth/callback",
		ClientID: mockshop.ClientID, AllowLoopbackHTTP: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	s.Register(r)
	// apis.Serve registers this native admin route before OnServe in production.
	if ui.DistDirFS != nil {
		r.GET("/_/{path...}", apis.Static(ui.DistDirFS, false))
	}
	publicDir, err := filepath.Abs("pb_public")
	if err != nil {
		t.Fatal(err)
	}
	registerFrontend(&core.ServeEvent{Router: r}, publicDir)
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	api.Config.Handler = mux
	api.Start()
	t.Cleanup(api.Close)
	return apiURL, shopURL, mux
}

func TestFrontendRoutes(t *testing.T) {
	_, _, handler := frontendFixture(t)
	for _, path := range []string{"/", "/auth/callback?code=private-code&state=private-state"} {
		t.Run(path, func(t *testing.T) {
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, httptest.NewRequest("GET", path, nil))
			if res.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", res.Code)
			}
			if res.Header().Get("Cache-Control") != "no-store" || res.Header().Get("Referrer-Policy") != "no-referrer" || res.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("missing frontend security headers")
			}
			if len(res.Result().Cookies()) != 0 || strings.Contains(res.Body.String(), "private-code") || strings.Contains(res.Body.String(), "private-state") {
				t.Fatal("callback data reflected or cookie added")
			}
			html := strings.ReplaceAll(res.Body.String(), "\r\n", "\n")
			for _, tag := range []string{"<style>", "<script type=\"module\">"} {
				_, content, found := strings.Cut(html, tag)
				content, _, _ = strings.Cut(content, "</")
				hash := sha256.Sum256([]byte(content))
				if !found || !strings.Contains(res.Header().Get("Content-Security-Policy"), "'sha256-"+base64.StdEncoding.EncodeToString(hash[:])+"'") {
					t.Fatal("inline script/style changed: update its CSP hash in main.go")
				}
			}
		})
	}
	for _, path := range []string{"/api/missing", "/_/missing", "/studio", "/studio/callback", "/studio/session", "/api/shopify/auth/start", "/api/shopify/auth/complete", "/missing.js", "/app.js", "/style.css"} {
		for _, method := range []string{"GET", "POST"} {
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, httptest.NewRequest(method, path, nil))
			if res.Code != http.StatusNotFound || strings.Contains(res.Body.String(), "<!doctype") {
				t.Fatalf("%s %s: expected native 404, got %d", method, path, res.Code)
			}
		}
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest("GET", "/api/health", nil))
	if res.Code != http.StatusOK || !strings.Contains(res.Header().Get("Content-Type"), "application/json") {
		t.Fatal("native API replaced by frontend")
	}
	if ui.DistDirFS != nil {
		res = httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest("GET", "/_/", nil))
		if res.Code != http.StatusOK || strings.Contains(res.Body.String(), "Connexion Shopify") {
			t.Fatal("native admin replaced by frontend")
		}
	}
}

func TestFrontendMatchesREADMEAndPinnedSDK(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(data), "\r\n", "\n")
	}
	readme := read("README.md")
	_, example, _ := strings.Cut(readme, "<!-- native-redirect-example:start -->")
	_, example, _ = strings.Cut(example, "```javascript\n")
	example, _, _ = strings.Cut(example, "\n```")
	if example == "" || !strings.Contains(read("pb_public/index.html"), example) {
		t.Fatal("frontend OAuth functions diverge from tested README")
	}
	var pkg struct {
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal([]byte(read("integration/package.json")), &pkg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read("pb_public/index.html"), "https://cdn.jsdelivr.net/npm/pocketbase@"+pkg.DevDependencies["pocketbase"]+"/dist/pocketbase.es.mjs") {
		t.Fatal("CDN SDK version differs from pinned integration dependency")
	}
}

func TestFrontendWithOfficialSDK(t *testing.T) {
	if os.Getenv("RUN_JS_SDK_TEST") != "1" {
		t.Skip("set RUN_JS_SDK_TEST=1 after npm ci --prefix integration")
	}
	apiURL, shopURL, _ := frontendFixture(t)
	input, _ := json.Marshal(map[string]string{"url": apiURL, "shopURL": shopURL})
	cmd := exec.Command("node", "integration/frontend.mjs")
	cmd.Stdin = bytes.NewReader(input)
	if err := cmd.Run(); err != nil {
		// SDK errors may contain credentials. Never print subprocess output.
		t.Fatal("frontend integration with official SDK failed:", err)
	}
}
