package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrEmailExists = errors.New("email already exists")
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

type User struct {
	ID          string
	Email       *string
	DisplayName *string
	IsGuest     bool
}

type ProviderCredential struct {
	ID        string
	UserID    string
	Provider  string
	Label     string
	IsRevoked bool
	CreatedAt time.Time
}

func (s *Store) CreateGuestUser(ctx context.Context) (User, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)

	var u User
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (is_guest) VALUES (true)
		RETURNING id, email, display_name, is_guest
	`).Scan(&u.ID, &u.Email, &u.DisplayName, &u.IsGuest); err != nil {
		return User{}, err
	}

	if _, err := tx.Exec(ctx, `INSERT INTO entitlements (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, u.ID); err != nil {
		return User{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return u, nil
}

func (s *Store) CreatePasswordUser(ctx context.Context, email string, displayName *string, passwordHash string) (User, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)

	var u User
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (email, display_name, is_guest) VALUES ($1, $2, false)
		RETURNING id, email, display_name, is_guest
	`, email, displayName).Scan(&u.ID, &u.Email, &u.DisplayName, &u.IsGuest); err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrEmailExists
		}
		return User{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO password_credentials (user_id, password_hash) VALUES ($1, $2)
	`, u.ID, passwordHash); err != nil {
		return User{}, err
	}

	if _, err := tx.Exec(ctx, `INSERT INTO entitlements (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, u.ID); err != nil {
		return User{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return u, nil
}

func (s *Store) GetPasswordUserByEmail(ctx context.Context, email string) (user User, passwordHash string, err error) {
	row := s.pool.QueryRow(ctx, `
		SELECT u.id, u.email, u.display_name, u.is_guest, pc.password_hash
		FROM users u
		JOIN password_credentials pc ON pc.user_id = u.id
		WHERE u.email = $1
	`, email)

	if err := row.Scan(&user.ID, &user.Email, &user.DisplayName, &user.IsGuest, &passwordHash); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, "", ErrNotFound
		}
		return User{}, "", err
	}
	return user, passwordHash, nil
}

func (s *Store) GetOrCreateGoogleUser(ctx context.Context, subject string, email *string, displayName *string) (User, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)

	var u User
	row := tx.QueryRow(ctx, `
		SELECT u.id, u.email, u.display_name, u.is_guest
		FROM identities i
		JOIN users u ON u.id = i.user_id
		WHERE i.provider = 'google' AND i.subject = $1
	`, subject)

	if err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.IsGuest); err == nil {
		_ = tx.Commit(ctx)
		return u, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return User{}, err
	}

	// Try link by email (account upgrade path).
	if email != nil && *email != "" {
		err := tx.QueryRow(ctx, `
			SELECT id, email, display_name, is_guest
			FROM users
			WHERE email = $1
		`, *email).Scan(&u.ID, &u.Email, &u.DisplayName, &u.IsGuest)

		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return User{}, err
		}
	}

	if u.ID == "" {
		if err := tx.QueryRow(ctx, `
			INSERT INTO users (email, display_name, is_guest) VALUES ($1, $2, false)
			RETURNING id, email, display_name, is_guest
		`, email, displayName).Scan(&u.ID, &u.Email, &u.DisplayName, &u.IsGuest); err != nil {
			if isUniqueViolation(err) {
				return User{}, ErrEmailExists
			}
			return User{}, err
		}
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO identities (user_id, provider, subject, email)
		VALUES ($1, 'google', $2, $3)
		ON CONFLICT (provider, subject) DO NOTHING
	`, u.ID, subject, email); err != nil {
		return User{}, err
	}

	if _, err := tx.Exec(ctx, `INSERT INTO entitlements (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, u.ID); err != nil {
		return User{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return u, nil
}

func (s *Store) CreateRefreshSession(ctx context.Context, userID string, refreshTokenHash []byte, expiresAt time.Time) (string, error) {
	var sessionID string
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO refresh_sessions (user_id, refresh_token_hash, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id
	`, userID, refreshTokenHash, expiresAt).Scan(&sessionID); err != nil {
		return "", err
	}
	return sessionID, nil
}

type RefreshSession struct {
	ID        string
	User      User
	ExpiresAt time.Time
}

func (s *Store) GetValidRefreshSession(ctx context.Context, refreshTokenHash []byte) (RefreshSession, error) {
	var rs RefreshSession
	row := s.pool.QueryRow(ctx, `
		SELECT rs.id, rs.expires_at, u.id, u.email, u.display_name, u.is_guest
		FROM refresh_sessions rs
		JOIN users u ON u.id = rs.user_id
		WHERE rs.refresh_token_hash = $1
		  AND rs.revoked_at IS NULL
		  AND rs.expires_at > now()
	`, refreshTokenHash)

	if err := row.Scan(&rs.ID, &rs.ExpiresAt, &rs.User.ID, &rs.User.Email, &rs.User.DisplayName, &rs.User.IsGuest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RefreshSession{}, ErrNotFound
		}
		return RefreshSession{}, err
	}
	return rs, nil
}

func (s *Store) RotateRefreshSession(ctx context.Context, oldSessionID string, userID string, newRefreshTokenHash []byte, newExpiresAt time.Time) (newSessionID string, err error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE refresh_sessions
		SET revoked_at = now()
		WHERE id = $1 AND revoked_at IS NULL
	`, oldSessionID); err != nil {
		return "", err
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO refresh_sessions (user_id, refresh_token_hash, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id
	`, userID, newRefreshTokenHash, newExpiresAt).Scan(&newSessionID); err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return newSessionID, nil
}

func (s *Store) UpsertProviderCredential(
	ctx context.Context,
	userID string,
	provider string,
	label string,
	kmsKeyID string,
	dekCiphertext []byte,
	secretCiphertext []byte,
) (ProviderCredential, error) {
	var pc ProviderCredential
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO provider_credentials (user_id, provider, label, secret_ciphertext, dek_ciphertext, kms_key_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, provider, label)
		DO UPDATE SET
		  secret_ciphertext = EXCLUDED.secret_ciphertext,
		  dek_ciphertext = EXCLUDED.dek_ciphertext,
		  kms_key_id = EXCLUDED.kms_key_id,
		  revoked_at = NULL
		RETURNING id, user_id, provider, label, (revoked_at IS NOT NULL) AS is_revoked, created_at
	`, userID, provider, label, secretCiphertext, dekCiphertext, kmsKeyID).Scan(
		&pc.ID,
		&pc.UserID,
		&pc.Provider,
		&pc.Label,
		&pc.IsRevoked,
		&pc.CreatedAt,
	); err != nil {
		return ProviderCredential{}, err
	}
	return pc, nil
}

func (s *Store) ListProviderCredentials(
	ctx context.Context,
	userID string,
	limit int,
	cursorCreatedAtUnixMs *int64,
	cursorID *string,
) (items []ProviderCredential, nextCreatedAtUnixMs *int64, nextID *string, err error) {
	limitPlusOne := limit + 1

	var rows pgx.Rows
	if cursorCreatedAtUnixMs != nil && cursorID != nil && *cursorID != "" {
		rows, err = s.pool.Query(ctx, `
			SELECT id, user_id, provider, label, (revoked_at IS NOT NULL) AS is_revoked, created_at
			FROM provider_credentials
			WHERE user_id = $1
			  AND (created_at, id) < (to_timestamp($2 / 1000.0), $3::uuid)
			ORDER BY created_at DESC, id DESC
			LIMIT $4
		`, userID, *cursorCreatedAtUnixMs, *cursorID, limitPlusOne)
	} else {
		rows, err = s.pool.Query(ctx, `
			SELECT id, user_id, provider, label, (revoked_at IS NOT NULL) AS is_revoked, created_at
			FROM provider_credentials
			WHERE user_id = $1
			ORDER BY created_at DESC, id DESC
			LIMIT $2
		`, userID, limitPlusOne)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var pc ProviderCredential
		if err := rows.Scan(&pc.ID, &pc.UserID, &pc.Provider, &pc.Label, &pc.IsRevoked, &pc.CreatedAt); err != nil {
			return nil, nil, nil, err
		}
		items = append(items, pc)
	}
	if rows.Err() != nil {
		return nil, nil, nil, rows.Err()
	}

	if len(items) <= limit {
		return items, nil, nil, nil
	}

	// Pop the extra item; compute next cursor from the last returned item.
	items = items[:limit]
	last := items[len(items)-1]
	ms := last.CreatedAt.UnixMilli()
	nextCreatedAtUnixMs = &ms
	nextID = &last.ID
	return items, nextCreatedAtUnixMs, nextID, nil
}

func (s *Store) RevokeProviderCredential(ctx context.Context, userID string, credentialID string) (bool, error) {
	ct, err := s.pool.Exec(ctx, `
		UPDATE provider_credentials
		SET revoked_at = now()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL
	`, credentialID, userID)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
