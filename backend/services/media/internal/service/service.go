package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mediav1 "github.com/ryeng/agentai-backend-monorepo/backend/contracts/gen/go/agentai/media/v1"

	"github.com/ryeng/agentai-backend-monorepo/backend/services/media/internal/s3presign"
	"github.com/ryeng/agentai-backend-monorepo/backend/services/media/internal/store"
)

type Server struct {
	mediav1.UnimplementedMediaServiceServer

	log       *slog.Logger
	store     *store.Store
	presigner *s3presign.Presigner
	bucket    string
	ttl       time.Duration
}

func NewServer(log *slog.Logger, st *store.Store, presigner *s3presign.Presigner, bucket string, ttl time.Duration) *Server {
	return &Server{log: log, store: st, presigner: presigner, bucket: bucket, ttl: ttl}
}

func (s *Server) CreateAudioUpload(ctx context.Context, req *mediav1.CreateAudioUploadRequest) (*mediav1.CreateAudioUploadResponse, error) {
	userID := strings.TrimSpace(req.GetUserId())
	contentType := strings.TrimSpace(req.GetContentType())
	if userID == "" || contentType == "" {
		return nil, status.Error(codes.InvalidArgument, "missing user_id or content_type")
	}

	audioRef := uuid.NewString()
	objectKey := "audio/" + userID + "/" + audioRef

	uploadURL, err := s.presigner.PresignPutObject(ctx, objectKey, contentType, s.ttl)
	if err != nil {
		s.log.Error("presign upload failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	if err := s.store.CreatePendingAudioObject(ctx, audioRef, userID, s.bucket, objectKey, contentType); err != nil {
		s.log.Error("create pending audio object failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &mediav1.CreateAudioUploadResponse{
		AudioRef:         audioRef,
		UploadUrl:        uploadURL,
		ObjectKey:        objectKey,
		ExpiresInSeconds: int32(s.ttl.Seconds()),
	}, nil
}
