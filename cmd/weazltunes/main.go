package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"weazltunes.local/web/internal/server"
	"weazltunes.local/web/web"
)

func main() {
	handler, err := server.New(server.Config{DataDir: server.Env("DATA_DIR", "./data"), SecureCookie: os.Getenv("COOKIE_SECURE") == "true"}, web.Files)
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Addr: server.Env("LISTEN_ADDR", "0.0.0.0:4000"), Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Printf("WeazlTunes listening on %s", srv.Addr)
	if err = srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
