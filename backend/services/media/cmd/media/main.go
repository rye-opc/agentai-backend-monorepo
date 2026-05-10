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

	mediav1 "github.com/ryeng/agentai-backend-monorepo/backend/contracts/gen/go/agentai/media/v1"
	"github.com/ryeng/agentai-backend-monorepo/backend/libs/config"
	dbmigrate "github.com/ryeng/agentai-backend-monorepo/backend/libs/db/migrate"
	"github.com/ryeng/agentai-backend-monorepo/backend/libs/db/postgres"
	"github.com/ryeng/agentai-backend-monorepo/backend/libs/logging"

	"github.com/ryeng/agentai-backend-monorepo/backend/services/media/internal/s3presign"
	"github.com/ryeng/agentai-backend-monorepo/backend/services/media/internal/service"
	"github.com/ryeng/agentai-backend-monorepo/backend/services/media/internal/store"
	"github.com/ryeng/agentai-backend-monorepo/backend/services/media/migrations"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := logging.New("media")

	grpcAddr := config.String("MEDIA_GRPC_ADDR", ":9093")
	httpAddr := config.String("MEDIA_HTTP_ADDR", ":8083")

	databaseURL := config.String("DATABASE_URL", "postgres://agentai:agentai@localhost:5432/agentai_media?sslmode=disable")
	runMigrations := config.Bool("DB_RUN_MIGRATIONS", false)

	s3Bucket := config.String("S3_BUCKET", "agentai-audio")
	s3Endpoint := config.String("S3_ENDPOINT", "http://localhost:9000")
	s3Region := config.String("S3_REGION", "us-east-1")
	s3AccessKeyID := config.String("S3_ACCESS_KEY_ID", "minio")
	s3SecretAccessKey := config.String("S3_SECRET_ACCESS_KEY", "miniosecret")
	s3UsePathStyle := config.Bool("S3_USE_PATH_STYLE", true)
	uploadTTL := time.Duration(config.Int("S3_UPLOAD_TTL_SECONDS", 900)) * time.Second

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

	presigner, err := s3presign.New(ctx, s3Bucket, s3presign.Config{
		Endpoint:        s3Endpoint,
		Region:          s3Region,
		AccessKeyID:     s3AccessKeyID,
		SecretAccessKey: s3SecretAccessKey,
		UsePathStyle:    s3UsePathStyle,
	})
	if err != nil {
		log.Error("s3 presigner init failed", slog.Any("err", err))
		os.Exit(1)
	}

	st := store.New(pool)

	grpcLis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Error("grpc listen failed", slog.Any("err", err))
		os.Exit(1)
	}

	grpcServer := grpc.NewServer()
	mediav1.RegisterMediaServiceServer(grpcServer, service.NewServer(log, st, presigner, s3Bucket, uploadTTL))

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
