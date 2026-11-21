package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/agy"
	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/auth"
	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/config"
	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/server"
	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/session"
)

func main() {
	defaultAddr := os.Getenv("AGY_LISTEN_ADDR")
	if defaultAddr == "" {
		port := os.Getenv("PORT")
		if port == "" {
			port = "18765"
		}
		defaultAddr = "0.0.0.0:" + port
	}

	addr := flag.String("addr", defaultAddr, "HTTP listen address")
	flag.Parse()

	paths := config.ResolvePaths()
	cfgStore, err := config.NewStore(paths)
	if err != nil {
		log.Fatalf("failed to initialize config store: %v", err)
	}

	sessStore, err := session.NewStore(paths.SessionsFile)
	if err != nil {
		log.Fatalf("failed to initialize session store: %v", err)
	}

	authMgr := auth.NewManager(cfgStore)
	agyMgr := agy.NewManager(cfgStore)
	srv := server.New(cfgStore, authMgr, agyMgr, sessStore)

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv,
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("Antigravity for fnOS listening on http://%s (data_dir=%s)", *addr, paths.DataDir)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down Antigravity for fnOS...")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown error: %v", err)
	}
}
