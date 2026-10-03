package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"weazltunes.local/web/internal/server"
	"weazltunes.local/web/web"
)

func main() {
	handler, err := server.New(server.Config{DataDir: server.Env("DATA_DIR", "./data"), SecureCookie: os.Getenv("COOKIE_SECURE") == "true", AccountBudgetBytes: positiveEnv("CAPTURE_ACCOUNT_BUDGET_BYTES", 10<<30), CaptureBudgetBytes: positiveEnv("CAPTURE_BUDGET_BYTES", 20<<30), ReserveBytes: positiveEnv("CAPTURE_RESERVE_BYTES", 1<<30), RetentionDays: int(positiveEnv("CAPTURE_RETENTION_DAYS", 0))}, web.Files)
	if err != nil {
		log.Fatal(err)
	}
	defer handler.(interface{ Close() }).Close()
	srv := &http.Server{Addr: server.Env("LISTEN_ADDR", "0.0.0.0:4000"), Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Printf("WeazlTunes listening on %s", srv.Addr)
	if err = srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	<-shutdownDone
}

func positiveEnv(key string, defaultValue int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	n, e := strconv.ParseInt(value, 10, 64)
	if e != nil || n < 0 {
		log.Fatalf("%s must be a nonnegative integer", key)
	}
	return n
}
