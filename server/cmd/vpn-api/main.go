package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"registry.app.circle360.net/ssh-vpn/control-plane/internal/api"
	"registry.app.circle360.net/ssh-vpn/control-plane/internal/audit"
	"registry.app.circle360.net/ssh-vpn/control-plane/internal/config"
	"registry.app.circle360.net/ssh-vpn/control-plane/internal/middleware"
	"registry.app.circle360.net/ssh-vpn/control-plane/internal/session"
)

func main() {
	cfg := config.Load()

	// ── Structured logger ────────────────────────────────────────────────────
	logLevel := slog.LevelInfo
	if cfg.Debug {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	// ── Audit log ────────────────────────────────────────────────────────────
	auditLog, err := audit.NewLogger(cfg.AuditLogPath)
	if err != nil {
		slog.Error("failed to open audit log", "err", err)
		os.Exit(1)
	}
	defer auditLog.Close()

	// ── Session store ────────────────────────────────────────────────────────
	sessionStore := session.NewStore(cfg.SessionSecret, cfg.SessionMaxAge)

	// ── Gin router ───────────────────────────────────────────────────────────
	if !cfg.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestLogger(logger))
	r.Use(middleware.RateLimit(cfg.RateLimit))
	r.Use(middleware.SecurityHeaders(cfg.PublicMode))

	// ── Load HTML templates and static assets ────────────────────────────────
	r.LoadHTMLGlob("/app/web/templates/*.html")
	r.Static("/static", "/app/web/static")

	// ── Register routes ───────────────────────────────────────────────────────
	api.RegisterRoutes(r, cfg, sessionStore, auditLog)

	// ── TLS vs plain HTTP ────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         cfg.Listen,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("vpn-api starting", "addr", cfg.Listen, "public_mode", cfg.PublicMode)
		var err error
		if cfg.TLSCert != "" && cfg.TLSKey != "" {
			err = srv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	// ── Graceful shutdown ────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down vpn-api")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "err", err)
	}
}
