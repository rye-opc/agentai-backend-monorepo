package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) CreatePendingAudioObject(
	ctx context.Context,
	audioRef string,
	userID string,
	bucket string,
	objectKey string,
	contentType string,
) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audio_objects (audio_ref, user_id, bucket, object_key, content_type, status)
		VALUES ($1, $2, $3, $4, $5, 'pending_upload')
	`, audioRef, userID, bucket, objectKey, contentType)
	return err
}
