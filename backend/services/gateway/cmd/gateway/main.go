package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/MicahParks/keyfunc/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	authv1 "github.com/ryeng/agentai-backend-monorepo/backend/contracts/gen/go/agentai/auth/v1"
	chatv1 "github.com/ryeng/agentai-backend-monorepo/backend/contracts/gen/go/agentai/chat/v1"
	commonv1 "github.com/ryeng/agentai-backend-monorepo/backend/contracts/gen/go/agentai/common/v1"
	mediav1 "github.com/ryeng/agentai-backend-monorepo/backend/contracts/gen/go/agentai/media/v1"
	"github.com/ryeng/agentai-backend-monorepo/backend/libs/config"
	"github.com/ryeng/agentai-backend-monorepo/backend/libs/logging"
)

type ctxKey string

const ctxUserID ctxKey = "user_id"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := logging.New("gateway")

	httpAddr := config.String("GATEWAY_HTTP_ADDR", ":8080")
	requestTimeout := time.Duration(config.Int("REQUEST_TIMEOUT_MS", 5000)) * time.Millisecond

	authGRPC := config.String("AUTH_GRPC_ADDR", "localhost:9091")
	chatGRPC := config.String("CHAT_GRPC_ADDR", "localhost:9092")
	mediaGRPC := config.String("MEDIA_GRPC_ADDR", "localhost:9093")

	jwksURL := config.String("AUTH_JWKS_URL", "http://localhost:8081/.well-known/jwks.json")

	authConn, err := grpc.DialContext(ctx, authGRPC, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error("dial auth failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer authConn.Close()

	chatConn, err := grpc.DialContext(ctx, chatGRPC, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error("dial chat failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer chatConn.Close()

	mediaConn, err := grpc.DialContext(ctx, mediaGRPC, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error("dial media failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer mediaConn.Close()

	jwks, err := keyfunc.Get(jwksURL, keyfunc.Options{
		Ctx:               ctx,
		RefreshInterval:   30 * time.Minute,
		RefreshRateLimit:  1 * time.Minute,
		RefreshTimeout:    10 * time.Second,
		RefreshUnknownKID: true,
		RefreshErrorHandler: func(err error) {
			log.Warn("jwks refresh error", slog.Any("err", err))
		},
	})
	if err != nil {
		log.Error("jwks init failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer jwks.EndBackground()

	authClient := authv1.NewAuthServiceClient(authConn)
	chatClient := chatv1.NewChatServiceClient(chatConn)
	mediaClient := mediav1.NewMediaServiceClient(mediaConn)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	r.Route("/v1/auth", func(r chi.Router) {
		r.Post("/guest", func(w http.ResponseWriter, req *http.Request) {
			var body struct {
				DeviceID string `json:"deviceId"`
			}
			_ = json.NewDecoder(req.Body).Decode(&body)

			cctx, cancel := context.WithTimeout(req.Context(), requestTimeout)
			defer cancel()

			resp, err := authClient.CreateGuestSession(cctx, &authv1.CreateGuestSessionRequest{
				DeviceId: strings.TrimSpace(body.DeviceID),
			})
			if err != nil {
				writeGRPCError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, toSessionJSON(resp.GetSession()))
		})

		r.Post("/password/signup", func(w http.ResponseWriter, req *http.Request) {
			var body struct {
				Email       string `json:"email"`
				Password    string `json:"password"`
				DisplayName string `json:"displayName"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
				return
			}

			cctx, cancel := context.WithTimeout(req.Context(), requestTimeout)
			defer cancel()

			resp, err := authClient.SignUpPassword(cctx, &authv1.SignUpPasswordRequest{
				Email:       body.Email,
				Password:    body.Password,
				DisplayName: body.DisplayName,
			})
			if err != nil {
				writeGRPCError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, toSessionJSON(resp.GetSession()))
		})

		r.Post("/password/login", func(w http.ResponseWriter, req *http.Request) {
			var body struct {
				Email    string `json:"email"`
				Password string `json:"password"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
				return
			}

			cctx, cancel := context.WithTimeout(req.Context(), requestTimeout)
			defer cancel()

			resp, err := authClient.LoginPassword(cctx, &authv1.LoginPasswordRequest{
				Email:    body.Email,
				Password: body.Password,
			})
			if err != nil {
				writeGRPCError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, toSessionJSON(resp.GetSession()))
		})

		r.Post("/google", func(w http.ResponseWriter, req *http.Request) {
			var body struct {
				IDToken string `json:"idToken"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
				return
			}

			cctx, cancel := context.WithTimeout(req.Context(), requestTimeout)
			defer cancel()

			resp, err := authClient.LoginGoogle(cctx, &authv1.LoginGoogleRequest{IdToken: body.IDToken})
			if err != nil {
				writeGRPCError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, toSessionJSON(resp.GetSession()))
		})

		r.Post("/refresh", func(w http.ResponseWriter, req *http.Request) {
			var body struct {
				RefreshToken string `json:"refreshToken"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
				return
			}

			cctx, cancel := context.WithTimeout(req.Context(), requestTimeout)
			defer cancel()

			resp, err := authClient.RefreshSession(cctx, &authv1.RefreshSessionRequest{RefreshToken: body.RefreshToken})
			if err != nil {
				writeGRPCError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, toSessionJSON(resp.GetSession()))
		})
	})

	r.Group(func(r chi.Router) {
		r.Use(authMiddleware(jwks))

		r.Get("/v1/conversations", func(w http.ResponseWriter, req *http.Request) {
			userID := req.Context().Value(ctxUserID).(string)

			pageSize := clampInt(queryInt(req, "pageSize", 50), 1, 200)
			pageToken := strings.TrimSpace(req.URL.Query().Get("pageToken"))

			cctx, cancel := context.WithTimeout(req.Context(), requestTimeout)
			defer cancel()

			resp, err := chatClient.ListConversations(cctx, &chatv1.ListConversationsRequest{
				UserId: userID,
				Page: &commonv1.PageRequest{
					PageSize:  int32(pageSize),
					PageToken: pageToken,
				},
			})
			if err != nil {
				writeGRPCError(w, err)
				return
			}

			writeJSON(w, http.StatusOK, map[string]any{
				"items":         toConversationsJSON(resp.GetItems()),
				"nextPageToken": nilIfEmpty(resp.GetPage().GetNextPageToken()),
			})
		})

		r.Post("/v1/conversations", func(w http.ResponseWriter, req *http.Request) {
			userID := req.Context().Value(ctxUserID).(string)

			var body struct {
				Title string `json:"title"`
			}
			_ = json.NewDecoder(req.Body).Decode(&body)

			cctx, cancel := context.WithTimeout(req.Context(), requestTimeout)
			defer cancel()

			resp, err := chatClient.CreateConversation(cctx, &chatv1.CreateConversationRequest{
				UserId: userID,
				Title:  strings.TrimSpace(body.Title),
			})
			if err != nil {
				writeGRPCError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, toConversationJSON(resp))
		})

		r.Get("/v1/conversations/{conversationId}/messages", func(w http.ResponseWriter, req *http.Request) {
			userID := req.Context().Value(ctxUserID).(string)
			conversationID := chi.URLParam(req, "conversationId")

			pageSize := clampInt(queryInt(req, "pageSize", 50), 1, 500)
			pageToken := strings.TrimSpace(req.URL.Query().Get("pageToken"))

			cctx, cancel := context.WithTimeout(req.Context(), requestTimeout)
			defer cancel()

			resp, err := chatClient.ListMessages(cctx, &chatv1.ListMessagesRequest{
				UserId:         userID,
				ConversationId: conversationID,
				Page: &commonv1.PageRequest{
					PageSize:  int32(pageSize),
					PageToken: pageToken,
				},
			})
			if err != nil {
				writeGRPCError(w, err)
				return
			}

			writeJSON(w, http.StatusOK, map[string]any{
				"items":         toMessagesJSON(resp.GetItems()),
				"nextPageToken": nilIfEmpty(resp.GetPage().GetNextPageToken()),
			})
		})

		r.Post("/v1/conversations/{conversationId}/messages", func(w http.ResponseWriter, req *http.Request) {
			userID := req.Context().Value(ctxUserID).(string)
			conversationID := chi.URLParam(req, "conversationId")

			var body struct {
				Role     string `json:"role"`
				Content  string `json:"content"`
				AudioRef string `json:"audioRef"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
				return
			}

			cctx, cancel := context.WithTimeout(req.Context(), requestTimeout)
			defer cancel()

			resp, err := chatClient.CreateMessage(cctx, &chatv1.CreateMessageRequest{
				UserId:         userID,
				ConversationId: conversationID,
				Role:           body.Role,
				Content:        body.Content,
				AudioRef:       strings.TrimSpace(body.AudioRef),
			})
			if err != nil {
				writeGRPCError(w, err)
				return
			}

			writeJSON(w, http.StatusOK, toMessageJSON(resp))
		})

		r.Post("/v1/audio/uploads", func(w http.ResponseWriter, req *http.Request) {
			userID := req.Context().Value(ctxUserID).(string)

			var body struct {
				ContentType string `json:"contentType"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
				return
			}

			cctx, cancel := context.WithTimeout(req.Context(), requestTimeout)
			defer cancel()

			resp, err := mediaClient.CreateAudioUpload(cctx, &mediav1.CreateAudioUploadRequest{
				UserId:      userID,
				ContentType: strings.TrimSpace(body.ContentType),
			})
			if err != nil {
				writeGRPCError(w, err)
				return
			}

			writeJSON(w, http.StatusOK, map[string]any{
				"audioRef":         resp.GetAudioRef(),
				"uploadUrl":        resp.GetUploadUrl(),
				"objectKey":        resp.GetObjectKey(),
				"expiresInSeconds": resp.GetExpiresInSeconds(),
			})
		})
	})

	srv := &http.Server{
		Addr:              httpAddr,
		Handler:           r,
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

func authMiddleware(jwks *keyfunc.JWKS) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authz := r.Header.Get("Authorization")
			if !strings.HasPrefix(authz, "Bearer ") {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "missing bearer token"})
				return
			}
			raw := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
			if raw == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "missing bearer token"})
				return
			}

			token, err := jwt.Parse(raw, jwks.Keyfunc, jwt.WithValidMethods([]string{"RS256"}))
			if err != nil || token == nil || !token.Valid {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid token"})
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid token"})
				return
			}

			sub, _ := claims["sub"].(string)
			sub = strings.TrimSpace(sub)
			if sub == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid token"})
				return
			}

			ctx := context.WithValue(r.Context(), ctxUserID, sub)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeGRPCError(w http.ResponseWriter, err error) {
	st, ok := status.FromError(err)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal error"})
		return
	}

	code := st.Code()
	msg := st.Message()

	switch code {
	case codes.InvalidArgument:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": msg})
	case codes.Unauthenticated:
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": msg})
	case codes.PermissionDenied:
		writeJSON(w, http.StatusForbidden, map[string]any{"error": msg})
	case codes.NotFound:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": msg})
	case codes.AlreadyExists:
		writeJSON(w, http.StatusConflict, map[string]any{"error": msg})
	case codes.FailedPrecondition:
		writeJSON(w, http.StatusPreconditionFailed, map[string]any{"error": msg})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal error"})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func toSessionJSON(s *authv1.Session) map[string]any {
	if s == nil || s.User == nil {
		return map[string]any{}
	}

	email := nilIfEmpty(s.User.GetEmail())
	displayName := nilIfEmpty(s.User.GetDisplayName())

	return map[string]any{
		"user": map[string]any{
			"id":          s.User.GetId(),
			"email":       email,
			"displayName": displayName,
			"isGuest":     s.User.GetIsGuest(),
		},
		"accessToken":      s.GetAccessToken(),
		"refreshToken":     s.GetRefreshToken(),
		"expiresInSeconds": s.GetExpiresInSeconds(),
	}
}

func toConversationsJSON(items []*chatv1.Conversation) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		out = append(out, toConversationJSON(it))
	}
	return out
}

func toConversationJSON(c *chatv1.Conversation) map[string]any {
	if c == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":        c.GetId(),
		"title":     c.GetTitle(),
		"pinned":    c.GetPinned(),
		"createdAt": c.GetCreatedAt(),
		"updatedAt": c.GetUpdatedAt(),
	}
}

func toMessagesJSON(items []*chatv1.Message) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		out = append(out, toMessageJSON(it))
	}
	return out
}

func toMessageJSON(m *chatv1.Message) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":             m.GetId(),
		"conversationId": m.GetConversationId(),
		"role":           m.GetRole(),
		"content":        m.GetContent(),
		"audioRef":       nilIfEmpty(m.GetAudioRef()),
		"runId":          nilIfEmpty(m.GetRunId()),
		"createdAt":      m.GetCreatedAt(),
	}
}

func nilIfEmpty(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func queryInt(r *http.Request, key string, defaultValue int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return defaultValue
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	return v
}

func clampInt(v int, min int, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
