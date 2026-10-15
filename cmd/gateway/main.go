// gateway exposes studio routes and explicitly allowed collection APIs to a test tunnel.
package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

func main() {
	target, _ := url.Parse("http://127.0.0.1:8090")
	collections := os.Getenv("STUDIO_PUBLIC_COLLECTIONS")
	if collections == "" {
		collections = "users"
	}
	handler := gatewayHandler(target, strings.Split(collections, ","))
	log.Println("Gateway de test sur http://127.0.0.1:8092, studio et API des collections autorisées uniquement")
	server := &http.Server{Addr: "127.0.0.1:8092", Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}

func gatewayHandler(target *url.URL, collections []string) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)
	allowed := map[string]bool{}
	for _, name := range collections {
		name = strings.TrimSpace(name)
		if name != "" && !strings.HasPrefix(name, "_") && !strings.ContainsAny(name, "/\\.") {
			allowed[name] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		parts := strings.Split(p, "/")
		collectionAPI := len(parts) >= 5 && parts[1] == "api" && parts[2] == "collections" && allowed[parts[3]]
		studio := p == "/studio" || strings.HasPrefix(p, "/studio/")
		if path.Clean(p) != p || (!studio && !collectionAPI && p != "/api/realtime") {
			http.NotFound(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}
