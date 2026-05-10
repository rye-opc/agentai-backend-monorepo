package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ryeng/agentai-backend-monorepo/backend/libs/config"
	dbmigrate "github.com/ryeng/agentai-backend-monorepo/backend/libs/db/migrate"
	"github.com/ryeng/agentai-backend-monorepo/backend/libs/db/postgres"
	"github.com/ryeng/agentai-backend-monorepo/backend/libs/logging"

	"github.com/ryeng/agentai-backend-monorepo/backend/services/orchestrator/migrations"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := logging.New("orchestrator")

	httpAddr := config.String("ORCHESTRATOR_HTTP_ADDR", ":8084")
	databaseURL := config.String("DATABASE_URL", "postgres://agentai:agentai@localhost:5432/agentai_orchestrator?sslmode=disable")
	runMigrations := config.Bool("DB_RUN_MIGRATIONS", false)

	pool, err := postgres.NewPool(ctx, databaseURL)
	if err != nil {
		log.Error("db connect failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer pool.Close()

	if runMigrations {
		if err := dbmigrate.Up(databaseURL, migrations.FS, ".", dbmigrate.Options{MigrationsTable: "schema_migrations"}); err != nil {
			log.Error("migrations failed", slog.Any("err", err))
			os.Exit(1)
		}
		log.Info("migrations complete")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	srv := &http.Server{
		Addr:              httpAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("http server listening", slog.String("addr", httpAddr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http serve failed", slog.Any("err", err))
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
