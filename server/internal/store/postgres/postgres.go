package postgres

import (
	"errors"
	"fmt"
	"time"

	domain "LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/db/sqlc"
	"LLMGateway/server/internal/store"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the PostgreSQL-backed implementation of the module-owned ports.
//
// SQL access uses sqlc-generated queries from db/queries. Channel key
// encryption/decryption and catalog rules live in the catalog module; this
// store only moves ciphertext.
type Store struct {
	pool    *pgxpool.Pool
	queries *sqlc.Queries
	now     func() time.Time
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{
		pool:    pool,
		queries: sqlc.New(pool),
		now:     time.Now,
	}
}

// mapError converts database errors into the store error vocabulary:
// missing rows become ErrNotFound and constraint/format violations become
// ErrInvalid.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505", // unique_violation
			"23503", // foreign_key_violation
			"23502", // not_null_violation
			"22P02", // invalid_text_representation
			"22003", // numeric_value_out_of_range
			"22007", // invalid_datetime_format
			"22008": // datetime_field_overflow
			return fmt.Errorf("%w: %s", store.ErrInvalid, pgErr.Message)
		}
	}
	return err
}

// textValue renders a nullable text value (sqlc may infer interface{} after a
// COALESCE) as a string. A nil/empty value means "unset".
func textValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return fmt.Sprint(typed)
	}
}

func textOrEmpty(value pgtype.Text) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optionalTimestamp(value pgtype.Timestamptz) *string {
	if !value.Valid {
		return nil
	}
	formatted := value.Time.UTC().Format(time.RFC3339)
	return &formatted
}

func timestampValue(value string) pgtype.Timestamptz {
	if value == "" {
		return pgtype.Timestamptz{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: parsed, Valid: true}
}

func optionalInt(value pgtype.Int8) *int {
	if !value.Valid {
		return nil
	}
	converted := int(value.Int64)
	return &converted
}

func int8Value(value *int) pgtype.Int8 {
	if value == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: int64(*value), Valid: true}
}

func channelDTO(id int64, name, baseURL, authType string, status, weight, priority int32, balance string, modelCount int32) domain.ChannelDTO {
	return domain.ChannelDTO{ID: int(id), Name: name, BaseURL: baseURL, AuthType: authType, Status: int(status), Weight: int(weight), Priority: int(priority), Balance: optionalString(balance), ModelCount: int(modelCount)}
}

func pricingDTO(id, channelID int64, channelName, upstreamModel, inputPrice, outputPrice, cachedPrice, currency string) domain.PricingDTO {
	return domain.PricingDTO{ID: int(id), ChannelID: int(channelID), ChannelName: channelName, UpstreamModel: upstreamModel, InputPricePer1M: inputPrice, OutputPricePer1M: outputPrice, CachedInputPricePer1M: cachedPrice, Currency: currency}
}
