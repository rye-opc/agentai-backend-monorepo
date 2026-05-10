package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ryeng/agentai-backend-monorepo/backend/libs/logging"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := logging.New("worker")
	log.Info("worker started (scaffold)")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down")
			return
		case <-ticker.C:
			log.Debug("worker heartbeat", slog.String("status", "idle"))
		}
	}
}
