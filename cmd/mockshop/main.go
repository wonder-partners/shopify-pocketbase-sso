package main

import (
	"log"
	"net/http"
	"studio-sso/internal/mockshop"
	"time"
)

func main() {
	m, err := mockshop.New("http://127.0.0.1:8091", "http://127.0.0.1:8090/studio/callback", "", "")
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Simulation locale uniquement : http://127.0.0.1:8091")
	s := &http.Server{Addr: "127.0.0.1:8091", Handler: m, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(s.ListenAndServe())
}
