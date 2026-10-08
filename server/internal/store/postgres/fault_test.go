package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/db/sqlc"
	"LLMGateway/server/internal/quota"
	"LLMGateway/server/internal/ratelimit"
	"LLMGateway/server/internal/usage"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var errDatabaseFault = errors.New("database operation failed")

type faultRow struct{ err error }

func (r faultRow) Scan(...any) error { return r.err }

type emptyRows struct{ pgx.Rows }

func (emptyRows) Next() bool { return false }
func (emptyRows) Err() error { return nil }
func (emptyRows) Close()     {}

type faultDB struct {
	pgx.Tx
	calls      int
	failAt     int
	commandTag string
}

func (db *faultDB) fail() bool {
	db.calls++
	return db.failAt == 0 || db.calls == db.failAt
}

func (db *faultDB) QueryRow(context.Context, string, ...any) pgx.Row {
	if db.fail() {
		return faultRow{errDatabaseFault}
	}
	return faultRow{nil}
}

func (db *faultDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if db.fail() {
		return nil, errDatabaseFault
	}
	return emptyRows{}, nil
}

func (db *faultDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	if db.fail() {
		return pgconn.CommandTag{}, errDatabaseFault
	}
	if db.commandTag != "" {
		return pgconn.NewCommandTag(db.commandTag), nil
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

// Failed reads must not be represented as empty successful lists, and failed
// writes must not acknowledge mutations that the database did not apply.
func TestPersistencePropagatesDatabaseFaults(t *testing.T) {
	ctx := context.Background()
	validSince := "2026-10-08T00:00:00Z"
	cases := []struct {
		name string
		call func(*Store) error
	}{
		{"list keys", func(s *Store) error { _, e := s.ListKeys(ctx, 1, 1, 20); return e }},
		{"authenticate key", func(s *Store) error { _, e := s.AuthenticateKey(ctx, "hash"); return e }},
		{"last used", func(s *Store) error { return s.UpdateKeyLastUsed(ctx, 1) }},
		{"credentials username", func(s *Store) error { _, e := s.GetUserCredentialsByUsername(ctx, "alice"); return e }},
		{"credentials id", func(s *Store) error { _, e := s.GetUserCredentialsByID(ctx, 1); return e }},
		{"account", func(s *Store) error { _, e := s.GetAccountByID(ctx, 1); return e }},
		{"session", func(s *Store) error { _, e := s.GetSessionByTokenHash(ctx, "hash"); return e }},
		{"delete session", func(s *Store) error { _, e := s.DeleteSessionByTokenHash(ctx, "hash"); return e }},
		{"reap sessions", func(s *Store) error { _, e := s.DeleteExpiredSessions(ctx, 0); return e }},
		{"list channels", func(s *Store) error { _, e := s.ListChannels(ctx, 1); return e }},
		{"channel owner", func(s *Store) error { _, e := s.GetChannelOwner(ctx, 1); return e }},
		{"channel", func(s *Store) error { _, e := s.GetChannelDTO(ctx, 1, 1); return e }},
		{"channel secret", func(s *Store) error { _, e := s.GetChannelRecord(ctx, 1, 1); return e }},
		{"insert channel", func(s *Store) error { _, e := s.InsertChannel(ctx, catalog.ChannelInsert{}); return e }},
		{"update channel", func(s *Store) error { _, e := s.UpdateChannelRecord(ctx, 1, 1, catalog.ChannelUpdate{}); return e }},
		{"channel status", func(s *Store) error { _, e := s.UpdateChannelStatusRecord(ctx, 1, 1, 1); return e }},
		{"delete channel", func(s *Store) error { _, e := s.DeleteChannel(ctx, 1, 1); return e }},
		{"list mappings", func(s *Store) error { _, e := s.ListChannelModels(ctx, 1, 1); return e }},
		{"mapping by id", func(s *Store) error { _, _, e := s.GetChannelModelByID(ctx, 1, 1); return e }},
		{"insert mapping", func(s *Store) error { _, e := s.InsertChannelModel(ctx, 1, catalog.ChannelModel{}); return e }},
		{"delete mapping", func(s *Store) error { _, e := s.DeleteChannelModel(ctx, 1, 1); return e }},
		{"mapping exists", func(s *Store) error { _, e := s.ChannelModelExists(ctx, 1, 1, "model"); return e }},
		{"upstream exists", func(s *Store) error { _, e := s.ChannelUpstreamExists(ctx, 1, 1, "model"); return e }},
		{"models", func(s *Store) error { _, e := s.ListCatalogModels(ctx, 1, false); return e }},
		{"pricing", func(s *Store) error { _, e := s.ListPricing(ctx, 1); return e }},
		{"upsert pricing", func(s *Store) error { _, e := s.UpsertPricingRecord(ctx, catalog.PricingRecord{}); return e }},
		{"delete pricing", func(s *Store) error { return s.DeletePricing(ctx, catalog.DeletePricingInput{}) }},
		{"get pricing", func(s *Store) error { _, e := s.GetPricing(ctx, 1, "model"); return e }},
		{"route candidates", func(s *Store) error { _, e := s.RouteCandidates(ctx, 1, "model", 10); return e }},
		{"route candidate", func(s *Store) error { _, _, e := s.RouteCandidate(ctx, 1, "model", 1, 10); return e }},
		{"health", func(s *Store) error { _, _, e := s.GetChannelHealthRow(ctx, 1); return e }},
		{"health list", func(s *Store) error { _, e := s.ListChannelHealthRows(ctx, 1); return e }},
		{"acquire probe", func(s *Store) error { _, _, e := s.AcquireChannelProbe(ctx, 1, time.Minute); return e }},
		{"release probe", func(s *Store) error { _, e := s.ReleaseChannelProbe(ctx, 1, "lease"); return e }},
		{"breaker config", func(s *Store) error { _, _, e := s.GetUserBreakerConfigRow(ctx, 1); return e }},
		{"health retention", func(s *Store) error { _, e := s.DeleteStaleChannelHealthBuckets(ctx, time.Now()); return e }},
		{"list rates", func(s *Store) error { _, e := s.ListRateLimits(ctx, 1, nil, 1, 20); return e }},
		{"get rate", func(s *Store) error { _, e := s.GetRateLimit(ctx, 1, 1); return e }},
		{"insert rate", func(s *Store) error { _, e := s.InsertRateLimit(ctx, 1, ratelimit.RateLimitRule{}); return e }},
		{"toggle rate", func(s *Store) error { _, e := s.UpdateRateLimitEnabled(ctx, 1, 1, true); return e }},
		{"delete rate", func(s *Store) error { _, e := s.DeleteRateLimit(ctx, 1, 1); return e }},
		{"reservation", func(s *Store) error {
			_, e := s.InsertRateLimitReservation(ctx, ratelimit.RateLimitReservationInput{})
			return e
		}},
		{"finalize rate", func(s *Store) error { _, e := s.FinalizeRateLimitReservation(ctx, 1); return e }},
		{"release rate", func(s *Store) error { _, e := s.ReleaseRateLimitReservation(ctx, 1); return e }},
		{"reap rate", func(s *Store) error { _, e := s.ReapRateLimitReservations(ctx, 10); return e }},
		{"count rate", func(s *Store) error { _, e := s.CountActiveRateLimitReservations(ctx, 1, nil, "model", nil); return e }},
		{"quota policy", func(s *Store) error { _, e := s.GetQuotaPolicy(ctx, 1, 1); return e }},
		{"insert quota", func(s *Store) error { _, e := s.InsertQuotaPolicy(ctx, quota.QuotaPolicy{}); return e }},
		{"delete quota", func(s *Store) error { _, e := s.DeleteQuotaPolicy(ctx, 1, 1); return e }},
		{"key ownership", func(s *Store) error { _, e := s.KeyBelongsToUser(ctx, 1, 1); return e }},
		{"quota list", func(s *Store) error { _, e := s.ListQuotaPolicies(ctx, quota.QuotaPolicyFilter{}); return e }},
		{"quota usage", func(s *Store) error { _, e := s.ListQuotaUsage(ctx, quota.QuotaPolicyFilter{}); return e }},
		{"usage list", func(s *Store) error { _, e := s.ListUsageLogs(ctx, 1, usage.UsageLogFilter{}); return e }},
		{"usage detail", func(s *Store) error { _, e := s.GetUsageLog(ctx, 1, 1); return e }},
		{"usage insert", func(s *Store) error { _, e := s.InsertUsageLog(ctx, usage.UsageLogInput{}); return e }},
		{"count requests", func(s *Store) error {
			_, e := s.CountRequestsSince(ctx, usage.UsageCountFilter{Since: validSince})
			return e
		}},
		{"count tokens", func(s *Store) error {
			_, e := s.CountTokensSince(ctx, usage.TokenCountFilter{Since: validSince})
			return e
		}},
		{"overview", func(s *Store) error { _, e := s.StatsOverview(ctx, 1, "", ""); return e }},
		{"daily", func(s *Store) error { _, e := s.StatsDaily(ctx, 1, "", "", 1, 20); return e }},
		{"channel stats", func(s *Store) error { _, e := s.StatsChannels(ctx, 1, "", ""); return e }},
		{"ttft", func(s *Store) error { _, e := s.StatsTTFT(ctx, 1, usage.TTFTStatsFilter{}); return e }},
	}
	for _, group := range []string{"user", "api_key", "model", "channel"} {
		cases = append(cases, struct {
			name string
			call func(*Store) error
		}{"aggregate " + group, func(s *Store) error {
			_, e := s.AggregateUsage(ctx, 1, usage.UsageAggregateFilter{GroupBy: group})
			return e
		}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := &faultDB{}
			s := &Store{queries: sqlc.New(db)}
			if err := tc.call(s); !errors.Is(err, errDatabaseFault) {
				t.Fatalf("error = %v, want database fault", err)
			}
		})
	}
}

func TestPersistenceRejectsIncompletePagination(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		call func(*Store) error
	}{
		{"keys", func(s *Store) error { _, e := s.ListKeys(ctx, 1, 1, 20); return e }},
		{"rate rules", func(s *Store) error { _, e := s.ListRateLimits(ctx, 1, nil, 1, 20); return e }},
		{"quota policies", func(s *Store) error { _, e := s.ListQuotaPolicies(ctx, quota.QuotaPolicyFilter{}); return e }},
		{"quota usage", func(s *Store) error { _, e := s.ListQuotaUsage(ctx, quota.QuotaPolicyFilter{}); return e }},
		{"usage logs", func(s *Store) error { _, e := s.ListUsageLogs(ctx, 1, usage.UsageLogFilter{}); return e }},
		{"daily stats", func(s *Store) error { _, e := s.StatsDaily(ctx, 1, "", "", 1, 20); return e }},
		{"upsert then read pricing", func(s *Store) error { _, e := s.UpsertPricingRecord(ctx, catalog.PricingRecord{}); return e }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Store{queries: sqlc.New(&faultDB{failAt: 2})}
			if err := tc.call(s); !errors.Is(err, errDatabaseFault) {
				t.Fatalf("error = %v, want second-query failure", err)
			}
		})
	}
}

func TestTransactionPrimitivesPropagateDatabaseFaults(t *testing.T) {
	cases := []struct {
		name string
		call func(*Tx) error
	}{
		{"get user", func(tx *Tx) error { _, e := tx.GetUser(1); return e }},
		{"nickname", func(tx *Tx) error { _, e := tx.UpdateNickname(1, "Alice"); return e }},
		{"password", func(tx *Tx) error { _, e := tx.UpdatePassword(1, "hash"); return e }},
		{"get key", func(tx *Tx) error { _, e := tx.GetKey(1, 1); return e }},
		{"insert key", func(tx *Tx) error { _, e := tx.InsertKey(accounts.KeyInsert{}); return e }},
		{"toggle key", func(tx *Tx) error { _, _, e := tx.UpdateKeyActive(1, 1, true); return e }},
		{"delete key", func(tx *Tx) error { _, e := tx.DeleteKey(1, 1); return e }},
		{"insert user", func(tx *Tx) error { _, e := tx.InsertUserWithCredentials(accounts.CredentialsInput{}); return e }},
		{"insert session", func(tx *Tx) error { _, e := tx.InsertSession(accounts.SessionInput{}); return e }},
		{"mapping", func(tx *Tx) error { _, _, e := tx.UpdateChannelModelRecord(1, 1, "model", true); return e }},
		{"lock channel", func(tx *Tx) error { return tx.LockChannel(1, 1) }},
		{"read balance", func(tx *Tx) error { _, e := tx.GetChannelBalanceText(1, 1); return e }},
		{"write balance", func(tx *Tx) error { _, e := tx.UpdateChannelBalance(1, 1, "1.000000"); return e }},
		{"health window", func(tx *Tx) error { _, e := tx.GetChannelHealthWindow(1, time.Now()); return e }},
		{"health update", func(tx *Tx) error { _, e := tx.UpdateChannelHealth(catalog.ChannelHealth{}); return e }},
		{"health get", func(tx *Tx) error { _, e := tx.GetChannelHealthForUpdate(1); return e }},
		{"ensure health", func(tx *Tx) error { return tx.EnsureChannelHealth(1) }},
		{"health bucket", func(tx *Tx) error { return tx.UpsertChannelHealthBucket(1, time.Now(), 1, 0, 0) }},
		{"delete probe", func(tx *Tx) error { return tx.DeleteChannelProbe(1) }},
		{"delete health buckets", func(tx *Tx) error { return tx.DeleteChannelHealthBuckets(1) }},
		{"reset health", func(tx *Tx) error { return tx.ResetChannelHealthState(1) }},
		{"upsert breaker", func(tx *Tx) error { return tx.UpsertUserBreakerConfig(1, catalog.ChannelBreakerConfig{}) }},
		{"delete breaker", func(tx *Tx) error { return tx.DeleteUserBreakerConfig(1) }},
		{"insert usage", func(tx *Tx) error { _, e := tx.InsertUsageLog(usage.UsageLogInput{}); return e }},
		{"applicable policies", func(tx *Tx) error { _, e := tx.ApplicablePolicies(1, 1); return e }},
		{"quota reserve", func(tx *Tx) error { _, e := tx.ReserveBucket(1, time.Now(), 1, "1.000000"); return e }},
		{"quota insert", func(tx *Tx) error { _, e := tx.InsertReservation(quota.QuotaReservationInsert{}); return e }},
		{"quota upsert", func(tx *Tx) error { return tx.UpsertBucket(1, time.Now(), time.Now()) }},
		{"quota lock", func(tx *Tx) error { return tx.LockBucket(1, time.Now()) }},
		{"quota item", func(tx *Tx) error { return tx.InsertReservationItem(1, 1, time.Now(), 1, "1.000000") }},
		{"quota release", func(tx *Tx) error { return tx.ReleaseReservation(1, "released", time.Now()) }},
		{"quota reap", func(tx *Tx) error { _, e := tx.ReapExpired(time.Now(), 1, 1, 1); return e }},
		{"quota settle", func(tx *Tx) error { return tx.SettleQuotaReservation(1, "request", 1, 1, 1, "1.000000") }},
		{"quota cleanup user", func(tx *Tx) error { return tx.DeleteQuotaReservationsForUser(1) }},
		{"quota cleanup key", func(tx *Tx) error { return tx.DeleteQuotaReservationsForKey(1, 1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := &faultDB{}
			tx := &Tx{ctx: context.Background(), tx: db, queries: sqlc.New(db), now: time.Now}
			if err := tc.call(tx); !errors.Is(err, errDatabaseFault) {
				t.Fatalf("error = %v, want transaction database fault", err)
			}
		})
	}
}
