package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"LLMGateway/server/internal/money"
)

func (a *Server) ListChannels(ctx context.Context, ownerUserID int) (ListResponse[ChannelDTO], error) {
	return a.store.ListChannels(ctx, ownerUserID)
}

// CreateChannel applies channel defaults, normalizes the balance, encrypts the
// upstream key and inserts the channel owned by ownerUserID.
func (a *Server) CreateChannel(ctx context.Context, ownerUserID int, in ChannelInput) (ChannelDTO, error) {
	if strings.TrimSpace(in.APIKey) == "" {
		return ChannelDTO{}, fmt.Errorf("%w: api_key is required", ErrInvalid)
	}
	if in.AuthType == "" {
		in.AuthType = "bearer"
	}
	if in.Weight == 0 {
		in.Weight = 100
	}

	balance, err := normalizeBalance(in.Balance)
	if err != nil {
		return ChannelDTO{}, err
	}
	ciphertext, err := a.encryptSecret(in.APIKey)
	if err != nil {
		return ChannelDTO{}, err
	}

	id, err := a.store.InsertChannel(ctx, ChannelInsert{
		OwnerUserID:      ownerUserID,
		Name:             in.Name,
		BaseURL:          in.BaseURL,
		APIKeyCiphertext: ciphertext,
		AuthType:         in.AuthType,
		Status:           in.Status,
		Weight:           in.Weight,
		Priority:         in.Priority,
		Balance:          balance,
	})
	if err != nil {
		return ChannelDTO{}, err
	}
	return a.store.GetChannelDTO(ctx, ownerUserID, id)
}

// UpdateChannel normalizes the balance and rotates the upstream key only when a
// new plaintext key is supplied.
func (a *Server) UpdateChannel(ctx context.Context, ownerUserID, id int, in ChannelInput) (ChannelDTO, error) {
	balance, err := normalizeBalance(in.Balance)
	if err != nil {
		return ChannelDTO{}, err
	}

	ciphertext := ""
	if strings.TrimSpace(in.APIKey) != "" {
		encrypted, err := a.encryptSecret(in.APIKey)
		if err != nil {
			return ChannelDTO{}, err
		}
		ciphertext = encrypted
	}

	ok, err := a.store.UpdateChannelRecord(ctx, ownerUserID, id, ChannelUpdate{
		Name:             in.Name,
		BaseURL:          in.BaseURL,
		AuthType:         in.AuthType,
		Status:           in.Status,
		Weight:           in.Weight,
		Priority:         in.Priority,
		Balance:          balance,
		APIKeyCiphertext: ciphertext,
	})
	if err != nil {
		return ChannelDTO{}, err
	}
	if !ok {
		return ChannelDTO{}, ErrNotFound
	}
	return a.store.GetChannelDTO(ctx, ownerUserID, id)
}

func (a *Server) UpdateChannelStatus(ctx context.Context, ownerUserID, id, status int) (ChannelDTO, error) {
	ok, err := a.store.UpdateChannelStatusRecord(ctx, ownerUserID, id, status)
	if err != nil {
		return ChannelDTO{}, err
	}
	if !ok {
		return ChannelDTO{}, ErrNotFound
	}
	return a.store.GetChannelDTO(ctx, ownerUserID, id)
}

// UpdateChannelBalance sets and/or adjusts the balance inside a transaction,
// holding the channel row lock so concurrent read-modify-write cannot be lost.
func (a *Server) UpdateChannelBalance(ctx context.Context, ownerUserID, id int, balance, delta string) (ChannelDTO, error) {
	if balance == "" && delta == "" {
		return ChannelDTO{}, fmt.Errorf("%w: balance or delta is required", ErrInvalid)
	}

	err := a.tx.InTx(ctx, func(tx Tx) error {
		if err := tx.LockChannel(ownerUserID, id); err != nil {
			return err
		}
		currentText, err := tx.GetChannelBalanceText(ownerUserID, id)
		if err != nil {
			return err
		}
		base := money.Amount(0)
		if currentText != "" {
			parsed, err := money.Parse6(currentText)
			if err != nil {
				return fmt.Errorf("%w: invalid balance", ErrInvalid)
			}
			base = parsed
		}
		if balance != "" {
			parsed, err := money.Parse6(balance)
			if err != nil {
				return fmt.Errorf("%w: invalid balance", ErrInvalid)
			}
			base = parsed
		}
		if delta != "" {
			parsed, err := money.Parse6(delta)
			if err != nil {
				return fmt.Errorf("%w: invalid delta", ErrInvalid)
			}
			base = base.Add(parsed)
		}
		ok, err := tx.UpdateChannelBalance(ownerUserID, id, money.Format6(base))
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return ChannelDTO{}, err
	}
	return a.store.GetChannelDTO(ctx, ownerUserID, id)
}

func (a *Server) DeleteChannel(ctx context.Context, ownerUserID, id int) error {
	ok, err := a.store.DeleteChannel(ctx, ownerUserID, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// GetChannelSecret returns the channel with the decrypted upstream key. The
// plaintext key never leaves the process.
func (a *Server) GetChannelSecret(ctx context.Context, ownerUserID, id int) (*Channel, error) {
	record, err := a.store.GetChannelRecord(ctx, ownerUserID, id)
	if err != nil {
		return nil, err
	}

	plaintext := ""
	if record.APIKeyCiphertext != "" {
		if a.cipher == nil {
			return nil, errors.New("channel encryption key is not configured")
		}
		decrypted, err := a.cipher.Decrypt(record.APIKeyCiphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt channel api_key: %w", err)
		}
		plaintext = decrypted
	}

	return &Channel{
		ID:       record.ID,
		Name:     record.Name,
		BaseURL:  record.BaseURL,
		APIKey:   plaintext,
		AuthType: record.AuthType,
		Status:   record.Status,
		Weight:   record.Weight,
		Priority: record.Priority,
		Balance:  record.Balance,
	}, nil
}

func (a *Server) encryptSecret(plaintext string) (string, error) {
	if a.cipher == nil {
		return "", errors.New("channel encryption key is not configured")
	}
	return a.cipher.Encrypt(plaintext)
}

func normalizeBalance(balance *string) (*string, error) {
	if balance == nil || *balance == "" {
		return nil, nil
	}
	amount, err := money.Parse6(*balance)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid balance", ErrInvalid)
	}
	formatted := money.Format6(amount)
	return &formatted, nil
}
