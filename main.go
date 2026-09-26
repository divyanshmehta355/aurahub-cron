package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aurahub-cron/config"
	"aurahub-cron/services"
)

var startTime = time.Now()

func main() {
	// Initialize structured logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg := config.LoadConfig()

	slog.Info("===================================================")
	slog.Info("  Aurahub Auto-Clone Microservice (Go Native)")
	slog.Info("===================================================")

	// Initialize MongoDB
	if _, err := config.InitDB(cfg.MongoURI); err != nil {
		slog.Error("Fatal: Failed to connect to MongoDB", "error", err.Error())
		os.Exit(1)
	}
	defer func() {
		if config.MongoClient != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = config.MongoClient.Disconnect(ctx)
		}
	}()

	// Initialize Redis
	config.InitRedis(cfg.RedisURL)
	defer func() {
		if config.RedisClient != nil {
			_ = config.RedisClient.Close()
		}
	}()

	// Optional background ticker (if AUTO_CLONE_INTERVAL_HOURS > 0)
	if cfg.AutoCloneIntervalHours > 0 {
		ticker := time.NewTicker(time.Duration(cfg.AutoCloneIntervalHours) * time.Hour)
		go func() {
			slog.Info("Internal cron ticker enabled", "intervalHours", cfg.AutoCloneIntervalHours)
			for range ticker.C {
				slog.Info("Internal ticker triggered auto-clone cycle")
				services.RunAutoCloneCycle(context.Background(), cfg)
			}
		}()
	}

	// Router setup using Go 1.22+ ServeMux
	mux := http.NewServeMux()

	// Logging & Timing Middleware
	loggedMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		mux.ServeHTTP(w, r)
		duration := time.Since(start)
		slog.Info("HTTP Request", "method", r.Method, "path", r.URL.Path, "duration", duration.String())
	})

	// Public Health & Status Endpoints
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"service":               "aurahub-cron",
			"runtime":               "Go Native (Render Ready)",
			"status":                "online",
			"uptime":                time.Since(startTime).String(),
			"timestamp":             time.Now().UTC().Format(time.RFC3339),
			"agingMinutesThreshold": cfg.AgingMinutesThreshold,
			"maxClonesPerRun":       cfg.MaxClonesPerRun,
			"endpoints": map[string]string{
				"health":           "GET /health",
				"triggerAutoClone": "POST or GET /api/cron/auto-clone?key=YOUR_KEY",
			},
		}
		writeJSON(w, http.StatusOK, response)
	})

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "ok",
			"uptime": time.Since(startTime).Seconds(),
		})
	})

	// Protected Trigger Endpoint (Supports GET & POST for cron-job.org or manual calls)
	triggerHandler := func(w http.ResponseWriter, r *http.Request) {
		authKey := r.Header.Get("x-cron-key")
		if authKey == "" {
			authKey = r.URL.Query().Get("key")
		}

		if authKey != cfg.CronSecret {
			slog.Warn("Unauthorized access attempt", "ip", r.RemoteAddr, "path", r.URL.Path)
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"success": false,
				"error":   "Unauthorized: Invalid or missing x-cron-key header or key query parameter",
			})
			return
		}

		slog.Info("Cron trigger received", "remoteAddr", r.RemoteAddr, "method", r.Method)

		// Detach context from HTTP request so premature client disconnects do not abort DB writes or clones
		cycleCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Minute)
		defer cancel()

		report := services.RunAutoCloneCycle(cycleCtx, cfg)

		status := http.StatusOK
		if !report.Success {
			status = http.StatusInternalServerError
		}
		writeJSON(w, status, report)
	}

	mux.HandleFunc("POST /api/cron/auto-clone", triggerHandler)
	mux.HandleFunc("GET /api/cron/auto-clone", triggerHandler)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      loggedMux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 180 * time.Second,
	}

	// Graceful shutdown channel
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info(fmt.Sprintf("Server listening on port %s (http://localhost:%s)", cfg.Port, cfg.Port))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP Server error", "error", err.Error())
			os.Exit(1)
		}
	}()

	<-stopChan
	slog.Info("Shutting down gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server shutdown failed", "error", err.Error())
	}
	slog.Info("Server stopped successfully")
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
