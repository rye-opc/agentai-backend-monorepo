package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"google.golang.org/grpc"

	authv1 "github.com/ryeng/agentai-backend-monorepo/backend/contracts/gen/go/agentai/auth/v1"
	"github.com/ryeng/agentai-backend-monorepo/backend/libs/config"
	"github.com/ryeng/agentai-backend-monorepo/backend/libs/crypto/envelope"
	dbmigrate "github.com/ryeng/agentai-backend-monorepo/backend/libs/db/migrate"
	"github.com/ryeng/agentai-backend-monorepo/backend/libs/db/postgres"
	"github.com/ryeng/agentai-backend-monorepo/backend/libs/logging"

	"github.com/ryeng/agentai-backend-monorepo/backend/services/auth/internal/jwks"
	"github.com/ryeng/agentai-backend-monorepo/backend/services/auth/internal/service"
	"github.com/ryeng/agentai-backend-monorepo/backend/services/auth/internal/store"
	"github.com/ryeng/agentai-backend-monorepo/backend/services/auth/internal/tokens"
	"github.com/ryeng/agentai-backend-monorepo/backend/services/auth/migrations"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := logging.New("auth")

	grpcAddr := config.String("AUTH_GRPC_ADDR", ":9091")
	httpAddr := config.String("AUTH_HTTP_ADDR", ":8081")
	publicBaseURL := config.String("AUTH_PUBLIC_BASE_URL", "http://localhost:8081")

	databaseURL := config.String("DATABASE_URL", "postgres://agentai:agentai@localhost:5432/agentai_auth?sslmode=disable")
	runMigrations := config.Bool("DB_RUN_MIGRATIONS", false)

	jwtKID := config.String("AUTH_JWT_KID", "dev-1")
	jwtPrivatePEM := os.Getenv("AUTH_JWT_PRIVATE_KEY_PEM")
	jwtIssuer := config.String("AUTH_JWT_ISSUER", "agentai-auth")
	jwtAudience := config.String("AUTH_JWT_AUDIENCE", "agentai")
	accessTTL := time.Duration(config.Int("AUTH_ACCESS_TTL_SECONDS", 900)) * time.Second

	kmsKeyID := config.String("AUTH_KMS_KEY_ID", "local-1")
	masterKeyB64 := strings.TrimSpace(os.Getenv("AUTH_KMS_MASTER_KEY_BASE64"))
	if masterKeyB64 == "" {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		masterKeyB64 = base64.StdEncoding.EncodeToString(b)
		log.Warn("AUTH_KMS_MASTER_KEY_BASE64 not set; generated ephemeral dev key (credentials won't decrypt after restart)")
	}
	kms, err := envelope.NewLocalKMS(kmsKeyID, masterKeyB64)
	if err != nil {
		log.Error("KMS init failed", slog.Any("err", err))
		os.Exit(1)
	}

	signer, err := jwks.NewSigner(jwtKID, jwtPrivatePEM)
	if err != nil {
		log.Error("jwt signer init failed", slog.Any("err", err))
		os.Exit(1)
	}

	issuer := &tokens.Issuer{
		KID:       jwtKID,
		Issuer:    jwtIssuer,
		Audience:  jwtAudience,
		AccessTTL: accessTTL,
		Private:   signer.PrivateKey(),
	}

	var googleVerifier *oidc.IDTokenVerifier
	if clientID := strings.TrimSpace(os.Getenv("GOOGLE_OAUTH_CLIENT_ID")); clientID != "" {
		provider, err := oidc.NewProvider(ctx, "https://accounts.google.com")
		if err != nil {
			log.Error("google oidc provider init failed", slog.Any("err", err))
			os.Exit(1)
		}
		googleVerifier = provider.Verifier(&oidc.Config{ClientID: clientID})
	}

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
	authv1.RegisterAuthServiceServer(grpcServer, service.NewServer(log, st, issuer, kms, googleVerifier))

	httpServer := &http.Server{
		Addr:              httpAddr,
		ReadHeaderTimeout: 5 * time.Second,
	}
	httpServer.Handler = routes(publicBaseURL, signer)

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

func routes(publicBaseURL string, signer *jwks.Signer) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, signer.JWKS())
	})

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"issuer":   publicBaseURL,
			"jwks_uri": fmt.Sprintf("%s/.well-known/jwks.json", publicBaseURL),
		})
	})

	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
