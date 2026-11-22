package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
	defaultSocket := os.Getenv("SOCKET_PATH")
	defaultPrefix := os.Getenv("GATEWAY_PREFIX")
	if defaultPrefix == "" {
		defaultPrefix = "/app/antigravity-cli"
	}

	addr := flag.String("addr", defaultAddr, "HTTP TCP listen address (used when -socket is empty)")
	socketPath := flag.String("socket", defaultSocket, "Unix domain socket path for fnOS unified gateway")
	gatewayPrefix := flag.String("gateway-prefix", defaultPrefix, "fnOS unified gateway URL prefix")
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
	agyMgr.StartBackgroundServices()
	srv := server.New(cfgStore, authMgr, agyMgr, sessStore, *gatewayPrefix)

	httpServer := &http.Server{
		Handler:           srv,
		ReadHeaderTimeout: 15 * time.Second,
	}

	var ln net.Listener
	sock := strings.TrimSpace(*socketPath)
	if sock != "" {
		if info, err := os.Lstat(sock); err == nil {
			if info.Mode()&os.ModeSocket == 0 {
				log.Fatalf("refusing to remove non-socket file: %s", sock)
			}
			_ = os.Remove(sock)
		}
		ln, err = net.Listen("unix", sock)
		if err != nil {
			log.Fatalf("failed to listen on unix socket %s: %v", sock, err)
		}
		// fnOS gateway runs under a different system user than the package user.
		if err := os.Chmod(sock, 0o666); err != nil {
			log.Fatalf("failed to chmod unix socket %s: %v", sock, err)
		}
		defer os.Remove(sock)
		log.Printf("Antigravity for fnOS listening on unix://%s (prefix=%s, data_dir=%s)", sock, *gatewayPrefix, paths.DataDir)
	} else {
		ln, err = net.Listen("tcp", *addr)
		if err != nil {
			log.Fatalf("failed to listen on %s: %v", *addr, err)
		}
		log.Printf("Antigravity for fnOS listening on http://%s (prefix=%s, data_dir=%s)", *addr, *gatewayPrefix, paths.DataDir)
	}

	go func() {
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down Antigravity for fnOS...")
	agyMgr.CloseAll()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown error: %v", err)
	}
}
