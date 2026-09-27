package accounts

import (
	"context"
	"strings"

	"LLMGateway/server/internal/crypto"
	apperrors "LLMGateway/server/internal/errors"
)

// Profile returns the self-view of an authenticated account.
func (a *Server) Profile(ctx context.Context, userID int) (Account, error) {
	return a.store.GetAccountByID(ctx, userID)
}

// UpdateProfile updates the nickname and/or password. A password change
// requires the current password to match and mints a fresh bcrypt hash at the
// configured cost.
func (a *Server) UpdateProfile(ctx context.Context, userID int, in ProfileUpdateInput) (Account, error) {
	var newHash string
	if in.NewPassword != "" {
		if err := validatePassword(in.NewPassword); err != nil {
			return Account{}, err
		}
		creds, err := a.store.GetUserCredentialsByID(ctx, userID)
		if err != nil {
			return Account{}, err
		}
		if !crypto.VerifyPassword(creds.PasswordHash, in.CurrentPassword) {
			return Account{}, ErrInvalidCredentials
		}

		hash, err := crypto.HashPassword(in.NewPassword, a.auth.BcryptCost)
		if err != nil {
			return Account{}, err
		}
		newHash = hash
	}

	err := a.tx.InTx(ctx, func(tx Tx) error {
		if in.Nickname != nil {
			ok, err := tx.UpdateNickname(userID, strings.TrimSpace(*in.Nickname))
			if err != nil {
				return err
			}
			if !ok {
				return apperrors.ErrNotFound
			}
		}
		if newHash != "" {
			ok, err := tx.UpdatePassword(userID, newHash)
			if err != nil {
				return err
			}
			if !ok {
				return apperrors.ErrNotFound
			}
		}
		return nil
	})
	if err != nil {
		return Account{}, err
	}
	return a.Profile(ctx, userID)
}
