package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatv1 "github.com/ryeng/agentai-backend-monorepo/backend/contracts/gen/go/agentai/chat/v1"
	commonv1 "github.com/ryeng/agentai-backend-monorepo/backend/contracts/gen/go/agentai/common/v1"
	"github.com/ryeng/agentai-backend-monorepo/backend/libs/pagination"

	"github.com/ryeng/agentai-backend-monorepo/backend/services/chat/internal/store"
)

type Server struct {
	chatv1.UnimplementedChatServiceServer

	log   *slog.Logger
	store *store.Store
}

func NewServer(log *slog.Logger, st *store.Store) *Server {
	return &Server{log: log, store: st}
}

func (s *Server) CreateConversation(ctx context.Context, req *chatv1.CreateConversationRequest) (*chatv1.Conversation, error) {
	userID := strings.TrimSpace(req.GetUserId())
	if userID == "" {
		return nil, status.Error(codes.InvalidArgument, "missing user_id")
	}

	title := strings.TrimSpace(req.GetTitle())
	if title == "" {
		title = "New conversation"
	}

	c, err := s.store.CreateConversation(ctx, userID, title)
	if err != nil {
		s.log.Error("create conversation failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}
	return toProtoConversation(c), nil
}

func (s *Server) ListConversations(ctx context.Context, req *chatv1.ListConversationsRequest) (*chatv1.ListConversationsResponse, error) {
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

	items, nextT, nextID, err := s.store.ListConversations(ctx, userID, pageSize, cursorT, cursorID)
	if err != nil {
		s.log.Error("list conversations failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	resp := &chatv1.ListConversationsResponse{
		Items: make([]*chatv1.Conversation, 0, len(items)),
		Page:  &commonv1.PageResponse{},
	}
	for _, it := range items {
		resp.Items = append(resp.Items, toProtoConversation(it))
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

func (s *Server) CreateMessage(ctx context.Context, req *chatv1.CreateMessageRequest) (*chatv1.Message, error) {
	userID := strings.TrimSpace(req.GetUserId())
	conversationID := strings.TrimSpace(req.GetConversationId())
	role := strings.TrimSpace(req.GetRole())
	content := req.GetContent()

	if userID == "" || conversationID == "" {
		return nil, status.Error(codes.InvalidArgument, "missing user_id or conversation_id")
	}
	if content == "" {
		return nil, status.Error(codes.InvalidArgument, "missing content")
	}
	switch role {
	case "user", "assistant", "system":
	default:
		return nil, status.Error(codes.InvalidArgument, "invalid role")
	}

	var audioRef *string
	if strings.TrimSpace(req.GetAudioRef()) != "" {
		v := strings.TrimSpace(req.GetAudioRef())
		audioRef = &v
	}

	m, err := s.store.CreateMessage(ctx, userID, conversationID, role, content, audioRef)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "conversation not found")
		}
		s.log.Error("create message failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	return toProtoMessage(m), nil
}

func (s *Server) ListMessages(ctx context.Context, req *chatv1.ListMessagesRequest) (*chatv1.ListMessagesResponse, error) {
	userID := strings.TrimSpace(req.GetUserId())
	conversationID := strings.TrimSpace(req.GetConversationId())
	if userID == "" || conversationID == "" {
		return nil, status.Error(codes.InvalidArgument, "missing user_id or conversation_id")
	}

	pageSize := int(req.GetPage().GetPageSize())
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 500 {
		pageSize = 500
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

	items, nextT, nextID, err := s.store.ListMessages(ctx, userID, conversationID, pageSize, cursorT, cursorID)
	if err != nil {
		s.log.Error("list messages failed", slog.Any("err", err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	resp := &chatv1.ListMessagesResponse{
		Items: make([]*chatv1.Message, 0, len(items)),
		Page:  &commonv1.PageResponse{},
	}
	for _, it := range items {
		resp.Items = append(resp.Items, toProtoMessage(it))
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

func toProtoConversation(c store.Conversation) *chatv1.Conversation {
	return &chatv1.Conversation{
		Id:        c.ID,
		UserId:    c.UserID,
		Title:     c.Title,
		Pinned:    c.Pinned,
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func toProtoMessage(m store.Message) *chatv1.Message {
	audioRef := ""
	if m.AudioRef != nil {
		audioRef = *m.AudioRef
	}
	runID := ""
	if m.RunID != nil {
		runID = *m.RunID
	}
	return &chatv1.Message{
		Id:             m.ID,
		ConversationId: m.ConversationID,
		UserId:         m.UserID,
		Role:           m.Role,
		Content:        m.Content,
		AudioRef:       audioRef,
		RunId:          runID,
		CreatedAt:      m.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}
