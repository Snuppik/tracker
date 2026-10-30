// Точка входа API: конфиг, БД, схема, роутер, graceful shutdown.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tracker/db"
	"tracker/internal/auth"
	"tracker/internal/config"
	"tracker/internal/push"
	"tracker/internal/scheduler"
	"tracker/internal/service"
	"tracker/internal/store"
	"tracker/internal/transport/httpapi"
)

func main() {
	if err := run(); err != nil {
		slog.Error("остановка приложения", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	conn, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := store.Migrate(ctx, conn, db.Schema); err != nil {
		return err
	}

	tokens := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTExpiry)
	authSvc := service.NewAuthService(store.NewUserStore(conn), tokens)
	taskSvc := service.NewTaskService(conn)
	reminderSvc := service.NewReminderService(conn)
	notificationSvc := service.NewNotificationService(conn)
	statsSvc := service.NewStatsService(conn)

	sender := push.New(cfg.VAPIDPublicKey, cfg.VAPIDPrivateKey, cfg.VAPIDSubject)
	if !sender.Enabled() {
		logger.Warn("web push отключён: VAPID-ключи не заданы, работает только in-app лента")
	}
	pushSvc := service.NewPushService(conn, sender.PublicKey(), sender.Enabled())

	sched := scheduler.New(conn, logger, sender, cfg.ReminderTick, cfg.ReminderGrace)
	schedulerCtx, stopScheduler := context.WithCancel(ctx)
	defer stopScheduler()
	go sched.Run(schedulerCtx)

	server := httpapi.NewServer(cfg, logger, conn, tokens, authSvc, taskSvc,
		reminderSvc, notificationSvc, pushSvc, statsSvc)
	httpServer := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("HTTP-сервер запущен", "addr", cfg.Addr(), "env", cfg.Env)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("получен сигнал остановки")
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

func newLogger(cfg *config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.Env == "production" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
