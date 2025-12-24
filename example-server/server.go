package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// StartServer starts the HTTP server with graceful shutdown
func StartServer(config *ServerConfig) {
	http.HandleFunc("/health", loggingMiddleware(healthHandler))
	http.HandleFunc("/auth", loggingMiddleware(authHandler))
	http.HandleFunc("/sync/profiles", loggingMiddleware(authMiddleware(syncProfilesListHandler)))
	http.HandleFunc("/profiles/", loggingMiddleware(authMiddleware(syncProfileHandler)))

	server := &http.Server{
		Addr:    config.ListenAddr,
		Handler: nil, // uses default mux
	}

	// Channel to listen for interrupt signal
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		log.Printf("Starting server on %s", config.ListenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	<-done
	log.Println("Shutting down server...")

	// Create a context with timeout for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}

// healthHandler handles GET /health
func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`"OK"`))
}
