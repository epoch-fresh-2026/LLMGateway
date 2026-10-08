package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/db/sqlc"
	settlement "LLMGateway/server/internal/proxy/settlement"
	"LLMGateway/server/internal/quota"
	"LLMGateway/server/internal/ratelimit"
	"LLMGateway/server/internal/store"
	"LLMGateway/server/internal/usage"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestStorageValueCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{{"null", nil, ""}, {"text", "USD", "USD"}, {"bytes", []byte("USD"), "USD"}, {"driver numeric", 42, "42"}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := textValue(tc.value); got != tc.want {
				t.Fatalf("value = %q, want %q", got, tc.want)
			}
		})
	}
	for _, tc := range []struct{ input, want string }{{"", ""}, {" null ", "null"}, {" {\"b\":2,\"a\":1} ", "{\"a\":1,\"b\":2}"}, {"invalid", "invalid"}} {
		t.Run("json "+tc.input, func(t *testing.T) {
			if got := string(canonicalJSON(json.RawMessage(tc.input))); got != tc.want {
				t.Fatalf("canonical = %q, want %q", got, tc.want)
			}
		})
	}
	if rawJSON(json.RawMessage(" null ")) != nil {
		t.Fatal("JSON null must map to SQL NULL")
	}
	if got := string(rawJSON(json.RawMessage(" {} "))); got != "{}" {
		t.Fatalf("raw JSON = %q", got)
	}
	if got := timestampValue("not-a-time"); got.Valid {
		t.Fatal("invalid timestamp accepted")
	}
	limit, offset := limitOffset(0, 0)
	if limit != 20 || offset != 0 {
		t.Fatalf("defaults = %d/%d", limit, offset)
	}
	if got := channelHealthFromRow(sqlc.ChannelHealth{}); got.UpdatedAt != "" || got.OpenedAt != nil {
		t.Fatalf("null timestamps = %+v", got)
	}
	when := time.Date(2026, 10, 8, 1, 2, 3, 0, time.FixedZone("offset", 3600))
	if got := optionalTimestamp(pgtype.Timestamptz{Time: when, Valid: true}); got == nil || *got != "2026-10-08T00:02:03Z" {
		t.Fatalf("UTC timestamp = %v", got)
	}
}

func TestPersistenceValidatesStatisticsBeforeQuery(t *testing.T) {
	s := &Store{queries: sqlc.New(&faultDB{})}
	ctx := context.Background()
	cases := []struct {
		name string
		call func() error
	}{
		{"request since", func() error { _, e := s.CountRequestsSince(ctx, usage.UsageCountFilter{Since: "invalid"}); return e }},
		{"token since", func() error { _, e := s.CountTokensSince(ctx, usage.TokenCountFilter{Since: "invalid"}); return e }},
		{"overview", func() error { _, e := s.StatsOverview(ctx, 1, "invalid", ""); return e }},
		{"channels", func() error { _, e := s.StatsChannels(ctx, 1, "invalid", ""); return e }},
		{"ttft", func() error { _, e := s.StatsTTFT(ctx, 1, usage.TTFTStatsFilter{StartTime: "invalid"}); return e }},
		{"daily", func() error { _, e := s.StatsDaily(ctx, 1, "invalid", "", 1, 20); return e }},
		{"aggregate", func() error {
			_, e := s.AggregateUsage(ctx, 1, usage.UsageAggregateFilter{GroupBy: "model", StartTime: "invalid"})
			return e
		}},
		{"unknown aggregate", func() error {
			_, e := s.AggregateUsage(ctx, 1, usage.UsageAggregateFilter{GroupBy: "unsupported"})
			return e
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("error = %v, want invalid", err)
			}
		})
	}
}

func TestPGRateReservationLifecycleAndOwnedTargets(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	owner := testOwner(t, st)
	acc := accounts.New(st, st.AccountsTx())
	keyID, _ := createKeyAndHash(t, acc, owner)
	channel, err := testCatalog(t, st).CreateChannel(ctx, owner, catalog.ChannelInput{Name: "rate", BaseURL: "https://example.invalid", APIKey: "placeholder", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertChannelModel(ctx, channel.ID, catalog.ChannelModel{ModelName: "public", UpstreamModel: "upstream", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, kind, value string
		want              bool
	}{
		{"wildcard", "model", "*", true}, {"owner", "user", "1", true}, {"foreign", "user", "999", false},
		{"own key", "api_key", "1", true}, {"invalid key", "api_key", "x", false}, {"own channel", "channel", "1", true},
		{"invalid channel", "channel", "x", false}, {"own model", "model", "public", true}, {"foreign model", "model", "missing", false}, {"invalid kind", "other", "1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, e := st.TargetOwnedByUser(ctx, owner, tc.kind, tc.value)
			if e != nil || got != tc.want {
				t.Fatalf("owned = %v/%v, want %v", got, e, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name string
		end  func(int64) (bool, error)
	}{
		{"finalized", func(id int64) (bool, error) { return st.FinalizeRateLimitReservation(ctx, id) }},
		{"released", func(id int64) (bool, error) { return st.ReleaseRateLimitReservation(ctx, id) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, e := st.InsertRateLimitReservation(ctx, ratelimit.RateLimitReservationInput{RequestID: tc.name, UserID: owner, APIKeyID: keyID, Model: "public", ChannelID: &channel.ID, EstimatedTokens: 10, ExpiresAt: time.Now().Add(time.Minute)})
			if e != nil {
				t.Fatal(e)
			}
			count, e := st.CountActiveRateLimitReservations(ctx, owner, &keyID, "public", &channel.ID)
			if e != nil || count != 1 {
				t.Fatalf("active = %d/%v", count, e)
			}
			ok, e := tc.end(id)
			if e != nil || !ok {
				t.Fatalf("finish = %v/%v", ok, e)
			}
			ok, e = tc.end(id)
			if e != nil || ok {
				t.Fatalf("repeated finish = %v/%v", ok, e)
			}
		})
	}
	if _, err := st.InsertRateLimitReservation(ctx, ratelimit.RateLimitReservationInput{RequestID: "expired", UserID: owner, APIKeyID: keyID, Model: "public", ExpiresAt: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if count, e := st.ReapRateLimitReservations(ctx, 10); e != nil || count != 1 {
		t.Fatalf("reaped = %d/%v", count, e)
	}
	if count, e := st.CountActiveRateLimitReservations(ctx, owner, nil, "", nil); e != nil || count != 0 {
		t.Fatalf("remaining = %d/%v", count, e)
	}
	if list, e := st.ListChannelHealthRows(ctx, owner); e != nil || len(list) != 1 || list[0].ChannelID != channel.ID {
		t.Fatalf("health list = %+v/%v", list, e)
	}
	lease, ok, e := st.AcquireChannelProbe(ctx, channel.ID, time.Minute)
	if e != nil || !ok {
		t.Fatalf("probe = %v/%v", ok, e)
	}
	if ok, e := st.ReleaseChannelProbe(ctx, channel.ID, lease); e != nil || !ok {
		t.Fatalf("release probe = %v/%v", ok, e)
	}
	if _, found, e := st.GetChannelModelByID(ctx, channel.ID, 999); e != nil || found {
		t.Fatalf("missing mapping = %v/%v", found, e)
	}
	if exists, e := st.ChannelModelExists(ctx, owner, channel.ID, "missing"); e != nil || exists {
		t.Fatalf("missing alias = %v/%v", exists, e)
	}
	models, e := st.ListChannelModels(ctx, owner, channel.ID)
	if e != nil || models.Total != 1 || models.List[0].ModelName != "public" {
		t.Fatalf("owned mappings = %+v/%v", models, e)
	}
	mapping, found, e := st.GetChannelModelByID(ctx, channel.ID, models.List[0].ID)
	if e != nil || !found || mapping.UpstreamModel != "upstream" {
		t.Fatalf("mapping = %+v/%v/%v", mapping, found, e)
	}
	if e := st.CatalogTx().InTx(ctx, func(tx catalog.Tx) error {
		updated, found, e := tx.UpdateChannelModelRecord(channel.ID, mapping.ID, "renamed", false)
		if e != nil || !found || updated.ModelName != "renamed" || updated.Enabled {
			t.Fatalf("updated mapping = %+v/%v/%v", updated, found, e)
		}
		if _, found, e := tx.UpdateChannelModelRecord(channel.ID, 999, "missing", false); e != nil || found {
			t.Fatalf("missing mapping update = %v/%v", found, e)
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
	pricing, e := st.UpsertPricingRecord(ctx, catalog.PricingRecord{ChannelID: channel.ID, UpstreamModel: "upstream", InputPricePer1M: "1.000000", OutputPricePer1M: "2.000000", CachedInputPricePer1M: "0.500000", Currency: "USD"})
	if e != nil || pricing.CachedInputPricePer1M != "0.50000000" {
		t.Fatalf("cached price round trip = %+v/%v", pricing, e)
	}
	prices, e := st.ListPricing(ctx, owner)
	if e != nil || prices.Total != 1 || prices.List[0].ID != pricing.ID || prices.List[0].ChannelName != channel.Name {
		t.Fatalf("owned pricing list = %+v/%v", prices, e)
	}
}

func TestPGKeyQuotaPolicyFilteringAndNullableLimits(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	owner := testOwner(t, st)
	keyID, _ := createKeyAndHash(t, accounts.New(st, st.AccountsTx()), owner)
	tokens := int64(100)
	cost := "1.000000"
	id, e := st.InsertQuotaPolicy(ctx, quota.QuotaPolicy{PolicyName: "key cap", ScopeType: quota.QuotaScopeAPIKey, ScopeID: keyID, PeriodType: quota.QuotaPeriodDay, TokenLimit: &tokens, CostLimit: &cost, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	enabled := true
	filter := quota.QuotaPolicyFilter{OwnerUserID: owner, ScopeType: string(quota.QuotaScopeAPIKey), ScopeID: keyID, Enabled: &enabled, Page: 1, PageSize: 20}
	list, e := st.ListQuotaPolicies(ctx, filter)
	if e != nil || list.Total != 1 || list.List[0].ID != id || list.List[0].ScopeID != keyID || list.List[0].TokenLimit == nil || *list.List[0].TokenLimit != tokens {
		t.Fatalf("filtered key policy = %+v/%v", list, e)
	}
	enabled = false
	list, e = st.ListQuotaPolicies(ctx, filter)
	if e != nil || list.Total != 0 || len(list.List) != 0 {
		t.Fatalf("disabled policy filter = %+v/%v", list, e)
	}
	empty := ""
	if numericValue(&empty).Valid || numericValue(nil).Valid {
		t.Fatal("absent numeric limits must remain SQL NULL")
	}
	if deleted, e := st.DeleteQuotaPolicy(ctx, owner, id); e != nil || !deleted {
		t.Fatalf("policy deletion = %v/%v", deleted, e)
	}
	if _, e := st.GetQuotaPolicy(ctx, owner, id); !errors.Is(e, store.ErrNotFound) {
		t.Fatalf("deleted policy still available: %v", e)
	}
}

func TestPGTransactionManagersRejectCanceledAdmission(t *testing.T) {
	st := testStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		name string
		call func() error
	}{
		{"accounts", func() error {
			return st.AccountsTx().InTx(ctx, func(accounts.Tx) error { t.Fatal("canceled callback executed"); return nil })
		}},
		{"catalog", func() error {
			return st.CatalogTx().InTx(ctx, func(catalog.Tx) error { t.Fatal("canceled callback executed"); return nil })
		}},
		{"quota", func() error {
			return st.QuotaTx().InTx(ctx, func(quota.Tx) error { t.Fatal("canceled callback executed"); return nil })
		}},
		{"settlement", func() error {
			return st.SettlementTx().InTx(ctx, func(settlement.Tx) error { t.Fatal("canceled callback executed"); return nil })
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if e := tc.call(); !errors.Is(e, context.Canceled) {
				t.Fatalf("error = %v, want canceled", e)
			}
		})
	}
}

func TestKeyUpdateRejectsMissingOrUnreadablePostWriteState(t *testing.T) {
	for _, tc := range []struct {
		name string
		db   *faultDB
		want error
	}{
		{"missing key", &faultDB{failAt: 99, commandTag: "UPDATE 0"}, nil},
		{"read after update failed", &faultDB{failAt: 2}, errDatabaseFault},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &Tx{ctx: context.Background(), queries: sqlc.New(tc.db)}
			key, found, e := tx.UpdateKeyActive(1, 1, true)
			if !errors.Is(e, tc.want) || found || !reflect.DeepEqual(key, accounts.ClientKey{}) {
				t.Fatalf("result = %+v/%v/%v", key, found, e)
			}
		})
	}
}
