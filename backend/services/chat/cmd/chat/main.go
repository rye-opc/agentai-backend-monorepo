package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	chatv1 "github.com/ryeng/agentai-backend-monorepo/backend/contracts/gen/go/agentai/chat/v1"
	"github.com/ryeng/agentai-backend-monorepo/backend/libs/config"
	dbmigrate "github.com/ryeng/agentai-backend-monorepo/backend/libs/db/migrate"
	"github.com/ryeng/agentai-backend-monorepo/backend/libs/db/postgres"
	"github.com/ryeng/agentai-backend-monorepo/backend/libs/logging"

	"github.com/ryeng/agentai-backend-monorepo/backend/services/chat/internal/service"
	"github.com/ryeng/agentai-backend-monorepo/backend/services/chat/internal/store"
	"github.com/ryeng/agentai-backend-monorepo/backend/services/chat/migrations"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := logging.New("chat")

	grpcAddr := config.String("CHAT_GRPC_ADDR", ":9092")
	httpAddr := config.String("CHAT_HTTP_ADDR", ":8082")

	databaseURL := config.String("DATABASE_URL", "postgres://agentai:agentai@localhost:5432/agentai_chat?sslmode=disable")
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

	st := store.New(pool)

	grpcLis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Error("grpc listen failed", slog.Any("err", err))
		os.Exit(1)
	}

	grpcServer := grpc.NewServer()
	chatv1.RegisterChatServiceServer(grpcServer, service.NewServer(log, st))

	httpServer := &http.Server{
		Addr:              httpAddr,
		Handler:           healthHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("grpc server listening", slog.String("addr", grpcAddr))
		if err := grpcServer.Serve(grpcLis); err != nil {
			log.Error("grpc serve failed", slog.Any("err", err))
		}
	}()

	go func() {
		log.Info("http server listening", slog.String("addr", httpAddr))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http serve failed", slog.Any("err", err))
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")

	grpcServer.GracefulStop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
}

func healthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	return mux
}
