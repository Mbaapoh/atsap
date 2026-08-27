// Command atsap-api connects to Asterisk over ARI (call control) and AMI
// (events), and exposes a small HTTP API for health checks.
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"atsap-api/internal/ami"
	"atsap-api/internal/ari"
	"atsap-api/internal/config"
	"atsap-api/internal/logging"
	"atsap-api/internal/server"
)

func main() {
	logger := logging.New(envOr("LOG_LEVEL", "info"))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ariClient := ari.New(cfg.ARIURL, cfg.ARIUsername, cfg.ARIPassword, cfg.ARIAppName)

	amiClient := ami.New(cfg.AMIAddr, cfg.AMIUsername, cfg.AMIPassword, func(msg ami.Message) {
		logger.Info("ami event", "event", msg["Event"], "channel", msg["Channel"])
	}, logger)

	go func() {
		for {
			if err := amiClient.Connect(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				logger.Warn("ami: connection lost, retrying", "error", err)
				time.Sleep(5 * time.Second)
				continue
			}
		}
	}()

	go func() {
		err := ariClient.StreamEvents(ctx, func(ev ari.Event) {
			handleARIEvent(ctx, logger, ariClient, ev)
		}, logger)
		if err != nil && ctx.Err() == nil {
			logger.Error("ari: event stream stopped", "error", err)
		}
	}()

	httpSrv := server.New(cfg.HTTPAddr)
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil {
			logger.Info("http server stopped", "error", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	_ = server.Shutdown(context.Background(), httpSrv, 10*time.Second)
	_ = amiClient.Close()
}

// handleARIEvent is the default Stasis application behavior: answer every
// inbound channel that enters the app. Replace with real call-routing
// logic.
func handleARIEvent(ctx context.Context, logger *slog.Logger, client *ari.Client, ev ari.Event) {
	if ev.Type != "StasisStart" {
		return
	}

	var payload struct {
		Channel struct {
			ID string `json:"id"`
		} `json:"channel"`
	}
	if err := json.Unmarshal(ev.Raw, &payload); err != nil {
		logger.Warn("ari: failed to decode StasisStart", "error", err)
		return
	}

	logger.Info("stasis start", "channel_id", payload.Channel.ID)
	if err := client.AnswerChannel(ctx, payload.Channel.ID); err != nil {
		logger.Error("ari: failed to answer channel", "error", err, "channel_id", payload.Channel.ID)
	}
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
