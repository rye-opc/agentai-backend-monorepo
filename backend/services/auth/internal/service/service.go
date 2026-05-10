package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authv1 "github.com/ryeng/agentai-backend-monorepo/backend/contracts/gen/go/agentai/auth/v1"
	commonv1 "github.com/ryeng/agentai-backend-monorepo/backend/contracts/gen/go/agentai/common/v1"
	"github.com/ryeng/agentai-backend-monorepo/backend/libs/crypto/envelope"
	"github.com/ryeng/agentai-backend-monorepo/backend/libs/pagination"

	"github.com/ryeng/agentai-backend-monorepo/backend/services/auth/internal/store"
	"github.com/ryeng/agentai-backend-monorepo/backend/services/auth/internal/tokens"
)

type Server struct {
	authv1.UnimplementedAuthServiceServer

	log         *slog.Logger
	store       *store.Store
	issuer      *tokens.Issuer
	kms         envelope.KMS
	googleLogin *googleLogin
}

type googleLogin struct {
	verifier *oidc.IDTokenVerifier
}

func NewServer(log *slog.Logger, st *store.Store, issuer *tokens.Issuer, kms envelope.KMS, googleVerifier *oidc.IDTokenVerifier) *Server {
	var gl *googleLogin
	if googleVerifier != nil {
		gl = &googleLogin{verifier: googleVerifier}
	}

	return &Server{
		log:         log,
		store:       st,
		issuer:      issuer,
		kms:         kms,
		googleLogin: gl,
	}
}

func (s *Server) CreateGuestSession(ctx context.Context, _ *authv1.CreateGuestSessionRequest) (*authv1.SessionResponse, error) {
	user, err := s.store.CreateGuestUser(ctx)
	if err != nil {
		s.log.Error("create guest user failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	return s.newSession(ctx, user, map[string]any{"is_guest": user.IsGuest})
}

func (s *Server) SignUpPassword(ctx context.Context, req *authv1.SignUpPasswordRequest) (*authv1.SessionResponse, error) {
	email := strings.TrimSpace(req.GetEmail())
	password := req.GetPassword()
	if email == "" || !strings.Contains(email, "@") {
		return nil, status.Error(codes.InvalidArgument, "invalid email")
	}
	if len(password) < 8 {
		return nil, status.Error(codes.InvalidArgument, "password must be at least 8 characters")
	}

	var displayName *string
	if strings.TrimSpace(req.GetDisplayName()) != "" {
		v := strings.TrimSpace(req.GetDisplayName())
		displayName = &v
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		s.log.Error("bcrypt failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	user, err := s.store.CreatePasswordUser(ctx, email, displayName, string(hash))
	if err != nil {
		if errors.Is(err, store.ErrEmailExists) {
			return nil, status.Error(codes.AlreadyExists, "email already exists")
		}
		s.log.Error("create password user failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	return s.newSession(ctx, user, map[string]any{"email": email})
}

func (s *Server) LoginPassword(ctx context.Context, req *authv1.LoginPasswordRequest) (*authv1.SessionResponse, error) {
	email := strings.TrimSpace(req.GetEmail())
	password := req.GetPassword()
	if email == "" || password == "" {
		return nil, status.Error(codes.InvalidArgument, "missing email or password")
	}

	user, hash, err := s.store.GetPasswordUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.Unauthenticated, "invalid credentials")
		}
		s.log.Error("get password user failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid credentials")
	}

	return s.newSession(ctx, user, map[string]any{"email": email})
}

func (s *Server) LoginGoogle(ctx context.Context, req *authv1.LoginGoogleRequest) (*authv1.SessionResponse, error) {
	if s.googleLogin == nil {
		return nil, status.Error(codes.FailedPrecondition, "google login not configured")
	}

	raw := strings.TrimSpace(req.GetIdToken())
	if raw == "" {
		return nil, status.Error(codes.InvalidArgument, "missing id_token")
	}

	idToken, err := s.googleLogin.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid id_token")
	}

	var claims struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid id_token")
	}
	if strings.TrimSpace(claims.Sub) == "" {
		return nil, status.Error(codes.Unauthenticated, "invalid id_token")
	}

	var email *string
	if strings.TrimSpace(claims.Email) != "" && claims.EmailVerified {
		v := strings.TrimSpace(claims.Email)
		email = &v
	}
	var displayName *string
	if strings.TrimSpace(claims.Name) != "" {
		v := strings.TrimSpace(claims.Name)
		displayName = &v
	}

	user, err := s.store.GetOrCreateGoogleUser(ctx, claims.Sub, email, displayName)
	if err != nil {
		if errors.Is(err, store.ErrEmailExists) {
			return nil, status.Error(codes.AlreadyExists, "email already exists")
		}
		s.log.Error("get or create google user failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	extra := map[string]any{"provider": "google"}
	if email != nil {
		extra["email"] = *email
	}
	return s.newSession(ctx, user, extra)
}

func (s *Server) RefreshSession(ctx context.Context, req *authv1.RefreshSessionRequest) (*authv1.SessionResponse, error) {
	raw := strings.TrimSpace(req.GetRefreshToken())
	if raw == "" {
		return nil, status.Error(codes.InvalidArgument, "missing refresh_token")
	}

	refreshHash, err := tokens.HashRefreshToken(raw)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid refresh_token")
	}

	rs, err := s.store.GetValidRefreshSession(ctx, refreshHash)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.Unauthenticated, "invalid refresh_token")
		}
		s.log.Error("get refresh session failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	newRaw, newHash, err := tokens.NewRefreshToken()
	if err != nil {
		s.log.Error("generate refresh token failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if _, err := s.store.RotateRefreshSession(ctx, rs.ID, rs.User.ID, newHash, expiresAt); err != nil {
		s.log.Error("rotate refresh session failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	access, expiresIn, err := s.issuer.NewAccessToken(rs.User.ID, map[string]any{"is_guest": rs.User.IsGuest})
	if err != nil {
		s.log.Error("issue access token failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &authv1.SessionResponse{
		Session: &authv1.Session{
			User:             toProtoUser(rs.User),
			AccessToken:      access,
			RefreshToken:     newRaw,
			ExpiresInSeconds: expiresIn,
		},
	}, nil
}

func (s *Server) UpsertProviderCredential(ctx context.Context, req *authv1.UpsertProviderCredentialRequest) (*authv1.ProviderCredential, error) {
	userID := strings.TrimSpace(req.GetUserId())
	provider := strings.TrimSpace(req.GetProvider())
	label := strings.TrimSpace(req.GetLabel())
	apiKey := req.GetApiKey()

	if userID == "" || provider == "" || apiKey == "" {
		return nil, status.Error(codes.InvalidArgument, "missing required fields")
	}

	switch provider {
	case "openai", "elevenlabs":
	default:
		return nil, status.Error(codes.InvalidArgument, "unsupported provider")
	}

	if label == "" {
		label = "default"
	}

	if s.kms == nil {
		return nil, status.Error(codes.FailedPrecondition, "KMS not configured")
	}

	kmsKeyID, wrappedDEK, ciphertext, err := envelope.Encrypt(s.kms, []byte(apiKey))
	if err != nil {
		s.log.Error("encrypt provider credential failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	pc, err := s.store.UpsertProviderCredential(ctx, userID, provider, label, kmsKeyID, wrappedDEK, ciphertext)
	if err != nil {
		s.log.Error("upsert provider credential failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &authv1.ProviderCredential{
		Id:        pc.ID,
		UserId:    pc.UserID,
		Provider:  pc.Provider,
		Label:     pc.Label,
		IsRevoked: pc.IsRevoked,
	}, nil
}

func (s *Server) ListProviderCredentials(ctx context.Context, req *authv1.ListProviderCredentialsRequest) (*authv1.ListProviderCredentialsResponse, error) {
	userID := strings.TrimSpace(req.GetUserId())
	if userID == "" {
		return nil, status.Error(codes.InvalidArgument, "missing user_id")
	}

	pageSize := int(req.GetPage().GetPageSize())
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}

	var cursorT *int64
	var cursorID *string
	if token := strings.TrimSpace(req.GetPage().GetPageToken()); token != "" {
		c, err := pagination.DecodeTimeIDCursor(token)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid page_token")
		}
		cursorT = &c.T
		cursorID = &c.ID
	}

	items, nextT, nextID, err := s.store.ListProviderCredentials(ctx, userID, pageSize, cursorT, cursorID)
	if err != nil {
		s.log.Error("list provider credentials failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	resp := &authv1.ListProviderCredentialsResponse{
		Items: make([]*authv1.ProviderCredential, 0, len(items)),
		Page:  &commonv1.PageResponse{},
	}
	for _, it := range items {
		resp.Items = append(resp.Items, &authv1.ProviderCredential{
			Id:        it.ID,
			UserId:    it.UserID,
			Provider:  it.Provider,
			Label:     it.Label,
			IsRevoked: it.IsRevoked,
		})
	}

	if nextT != nil && nextID != nil {
		token, err := pagination.EncodeTimeIDCursor(pagination.TimeIDCursor{T: *nextT, ID: *nextID})
		if err != nil {
			return nil, status.Error(codes.Internal, "internal error")
		}
		resp.Page.NextPageToken = token
	}

	return resp, nil
}

func (s *Server) RevokeProviderCredential(ctx context.Context, req *authv1.RevokeProviderCredentialRequest) (*authv1.RevokeProviderCredentialResponse, error) {
	userID := strings.TrimSpace(req.GetUserId())
	credentialID := strings.TrimSpace(req.GetCredentialId())
	if userID == "" || credentialID == "" {
		return nil, status.Error(codes.InvalidArgument, "missing required fields")
	}

	ok, err := s.store.RevokeProviderCredential(ctx, userID, credentialID)
	if err != nil {
		s.log.Error("revoke provider credential failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &authv1.RevokeProviderCredentialResponse{Ok: ok}, nil
}

func (s *Server) newSession(ctx context.Context, user store.User, extraClaims map[string]any) (*authv1.SessionResponse, error) {
	access, expiresIn, err := s.issuer.NewAccessToken(user.ID, extraClaims)
	if err != nil {
		s.log.Error("issue access token failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	refreshRaw, refreshHash, err := tokens.NewRefreshToken()
	if err != nil {
		s.log.Error("generate refresh token failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	_, err = s.store.CreateRefreshSession(ctx, user.ID, refreshHash, time.Now().Add(30*24*time.Hour))
	if err != nil {
		s.log.Error("create refresh session failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &authv1.SessionResponse{
		Session: &authv1.Session{
			User:             toProtoUser(user),
			AccessToken:      access,
			RefreshToken:     refreshRaw,
			ExpiresInSeconds: expiresIn,
		},
	}, nil
}

func toProtoUser(u store.User) *authv1.User {
	email := ""
	if u.Email != nil {
		email = *u.Email
	}
	displayName := ""
	if u.DisplayName != nil {
		displayName = *u.DisplayName
	}
	return &authv1.User{
		Id:          u.ID,
		Email:       email,
		DisplayName: displayName,
		IsGuest:     u.IsGuest,
	}
}
