// gateway exposes only the prototype's /studio routes to an HTTPS tunnel.
package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

func main() {
	target, _ := url.Parse("http://127.0.0.1:8090")
	proxy := httputil.NewSingleHostReverseProxy(target)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/studio" && !strings.HasPrefix(r.URL.Path, "/studio/") {
			http.NotFound(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	})
	log.Println("Gateway de test sur http://127.0.0.1:8092, routes /studio uniquement")
	server := &http.Server{Addr: "127.0.0.1:8092", Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}
