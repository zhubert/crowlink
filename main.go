package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zhubert/crowlink/internal/config"
	"github.com/zhubert/crowlink/internal/server"
	"github.com/zhubert/crowlink/internal/store"
)

const version = "crowlink dev"

func main() {
	cfg, err := config.Get()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	var st store.Store
	switch cfg.Store {
	case "mem":
		st = store.NewMemStore()
	default:
		// config.Get already validates Store, so this should be unreachable.
		log.Fatalf("unknown store %q", cfg.Store)
	}

	handler := server.New(st, cfg.BaseURL)

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: handler,
	}

	// Start server in a goroutine so shutdown signal can be handled.
	go func() {
		log.Printf("%s listening on %s", version, cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Wait for interrupt or termination signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down…")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("graceful shutdown failed: %v", err)
	}
	log.Println("stopped")
}
