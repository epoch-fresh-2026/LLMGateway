package postgres

import (
	"context"

	domain "LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/db/sqlc"

	"github.com/jackc/pgx/v5/pgtype"
)

// --- Store reads ---

func (s *Store) GetUserCredentialsByUsername(ctx context.Context, username string) (domain.Credentials, error) {
	row, err := s.queries.GetUserCredentialsByUsername(ctx, pgtype.Text{String: username, Valid: true})
	if err != nil {
		return domain.Credentials{}, mapError(err)
	}
	return domain.Credentials{
		UserID:       int(row.ID),
		Username:     textOrEmpty(row.Username),
		Nickname:     row.Nickname,
		PasswordHash: textOrEmpty(row.PasswordHash),
	}, nil
}

func (s *Store) GetAccountByID(ctx context.Context, id int) (domain.Account, error) {
	row, err := s.queries.GetAccountByID(ctx, int64(id))
	if err != nil {
		return domain.Account{}, mapError(err)
	}
	return domain.Account{ID: int(row.ID), Username: textOrEmpty(row.Username), Nickname: row.Nickname}, nil
}

func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (domain.Session, error) {
	row, err := s.queries.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return domain.Session{}, mapError(err)
	}
	return domain.Session{ID: int(row.ID), UserID: int(row.UserID), ExpiresAt: row.ExpiresAt.Time}, nil
}

func (s *Store) DeleteSessionByTokenHash(ctx context.Context, tokenHash string) (bool, error) {
	affected, err := s.queries.DeleteSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return false, mapError(err)
	}
	return affected > 0, nil
}

func (s *Store) DeleteExpiredSessions(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	affected, err := s.queries.DeleteExpiredSessions(ctx, int32(limit))
	if err != nil {
		return 0, mapError(err)
	}
	return int(affected), nil
}

// --- Tx primitives ---

func (t *Tx) InsertUserWithCredentials(in domain.CredentialsInput) (domain.Account, error) {
	id, err := t.queries.CreateUserWithCredentials(t.ctx, sqlc.CreateUserWithCredentialsParams{
		Username:     pgtype.Text{String: in.Username, Valid: in.Username != ""},
		Nickname:     in.Nickname,
		PasswordHash: pgtype.Text{String: in.PasswordHash, Valid: in.PasswordHash != ""},
	})
	if err != nil {
		return domain.Account{}, mapError(err)
	}
	return domain.Account{ID: int(id), Username: in.Username, Nickname: in.Nickname}, nil
}

func (t *Tx) InsertSession(in domain.SessionInput) (int, error) {
	id, err := t.queries.CreateSession(t.ctx, sqlc.CreateSessionParams{
		TokenHash: in.TokenHash,
		UserID:    int64(in.UserID),
		ExpiresAt: pgtype.Timestamptz{Time: in.ExpiresAt, Valid: true},
	})
	if err != nil {
		return 0, mapError(err)
	}
	return int(id), nil
}
