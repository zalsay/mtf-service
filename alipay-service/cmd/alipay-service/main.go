package main

import (
	"log"
	"net/http"

	"alipay-service/internal/config"
	"alipay-service/internal/payment"
	"alipay-service/internal/server"
)

func main() {
	cfg := config.Load()
	verifier := payment.NewVerifier(cfg.Payment)
	handler := server.New(cfg.Server, verifier)

	addr := ":" + cfg.Port
	log.Printf("alipay-service listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}
