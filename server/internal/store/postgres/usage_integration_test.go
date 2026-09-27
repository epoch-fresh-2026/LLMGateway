package postgres

import (
	"context"
	"errors"
	"testing"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/store"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func TestPGUsageLogsAndStats(t *testing.T) {
	st := testStore(t)
	cat := testCatalog(t, st)
	acc := accounts.New(st, st.AccountsTx())
	ctx := context.Background()

	if _, err := acc.CreateUser(ctx, domain.UserInput{Nickname: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := acc.CreateUser(ctx, domain.UserInput{Nickname: "B"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk", Status: 1}); err != nil {
		t.Fatal(err)
	}

	first, err := st.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "req-1", UserID: intp(1), ChannelID: intp(1), Model: "gpt", Status: "success", TotalTokens: 100, TotalCost: "0.001000", UnitPriceInputPer1M: "0.10000000", UnitPriceOutputPer1M: "0.20000000"})
	if err != nil {
		t.Fatalf("InsertUsageLog: %v", err)
	}
	second, err := st.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "req-2", UserID: intp(1), ChannelID: intp(1), Model: "gpt-4o", Status: "error", TotalTokens: 200, TotalCost: "0.002000"})
	if err != nil {
		t.Fatal(err)
	}
	third, err := st.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "req-3", UserID: intp(2), ChannelID: intp(1), Model: "gpt", Status: "success", TotalTokens: 300, TotalCost: "0.003000"})
	if err != nil {
		t.Fatal(err)
	}

	// Control timestamps for deterministic UTC day grouping.
	if _, err := st.pool.Exec(ctx, "UPDATE usage_logs SET created_at = '2026-09-16T10:00:00Z' WHERE id = $1", first); err != nil {
		t.Fatal(err)
	}
	if _, err := st.pool.Exec(ctx, "UPDATE usage_logs SET created_at = '2026-09-16T23:30:00Z' WHERE id = $1", second); err != nil {
		t.Fatal(err)
	}
	if _, err := st.pool.Exec(ctx, "UPDATE usage_logs SET created_at = '2026-09-17T10:00:00Z' WHERE id = $1", third); err != nil {
		t.Fatal(err)
	}

	// List + filter.
	all, err := st.ListUsageLogs(context.Background(), domain.UsageLogFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListUsageLogs: %v", err)
	}
	if all.Total != 3 {
		t.Fatalf("total = %d, want 3", all.Total)
	}
	row := all.List[0]
	if row.ChannelName != "OpenAI" || row.TotalCost == "" {
		t.Fatalf("unexpected row: %+v", row)
	}

	filtered, _ := st.ListUsageLogs(context.Background(), domain.UsageLogFilter{Status: "error", Model: "gpt-4o", Page: 1, PageSize: 20})
	if filtered.Total != 1 {
		t.Fatalf("filtered total = %d, want 1", filtered.Total)
	}
	byUser, _ := st.ListUsageLogs(context.Background(), domain.UsageLogFilter{UserID: intp(1), Page: 1, PageSize: 20})
	if byUser.Total != 2 {
		t.Fatalf("user filter total = %d, want 2", byUser.Total)
	}
	byRange, _ := st.ListUsageLogs(context.Background(), domain.UsageLogFilter{StartTime: "2026-09-16T00:00:00Z", EndTime: "2026-09-16T23:59:59Z", Page: 1, PageSize: 20})
	if byRange.Total != 2 {
		t.Fatalf("range filter total = %d, want 2", byRange.Total)
	}

	if _, err := st.GetUsageLog(context.Background(), first); err != nil {
		t.Fatalf("GetUsageLog: %v", err)
	}
	if _, err := st.GetUsageLog(context.Background(), 404); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing log err = %v, want ErrNotFound", err)
	}

	overview, err := st.StatsOverview(context.Background(), "2026-09-16T00:00:00Z", "2026-09-16T23:59:59Z")
	if err != nil {
		t.Fatalf("StatsOverview: %v", err)
	}
	if overview.RequestCount != int64(2) || overview.SuccessCount != int64(1) || overview.ErrorCount != int64(1) {
		t.Fatalf("unexpected overview: %+v", overview)
	}
	if overview.TotalCost != "0.003000" || overview.ActiveUserCount != int64(1) {
		t.Fatalf("unexpected overview aggregates: %+v", overview)
	}

	daily, err := st.StatsDaily(context.Background(), "2026-09-16", "2026-09-17", 1, 100)
	if err != nil {
		t.Fatalf("StatsDaily: %v", err)
	}
	if daily.Total != 2 {
		t.Fatalf("daily total = %d, want 2", daily.Total)
	}
	day := daily.List[0]
	if day.StatDate != "2026-09-16" || day.RequestCount != int64(2) || day.TotalCost != "0.003000" {
		t.Fatalf("unexpected day: %+v", day)
	}

	channels, err := st.StatsChannels(context.Background(), "2026-09-16T00:00:00Z", "2026-09-16T23:59:59Z")
	if err != nil {
		t.Fatalf("StatsChannels: %v", err)
	}
	if len(channels.List) != 1 {
		t.Fatalf("channels len = %d, want 1", len(channels.List))
	}
	channel := channels.List[0]
	if channel.ChannelName != "OpenAI" || channel.RequestCount != int64(2) {
		t.Fatalf("unexpected channel stat: %+v", channel)
	}

	for index, ttft := range []int{100, 200, 500} {
		if _, err := st.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "ttft-" + string(rune('a'+index)), UserID: intp(1), ChannelID: intp(1), Model: "gpt", Status: "success", TTFTMs: &ttft}); err != nil {
			t.Fatal(err)
		}
	}
	ttft, err := st.StatsTTFT(context.Background(), domain.TTFTStatsFilter{UserID: intp(1), ChannelID: intp(1), Model: "gpt", StartTime: "1970-01-01T00:00:00Z", EndTime: "2100-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if ttft.SampleCount != 3 || ttft.AverageMs != 266 || ttft.P50Ms != 200 || ttft.P95Ms != 500 || ttft.P99Ms != 500 {
		t.Fatalf("unexpected TTFT stats: %+v", ttft)
	}
}

func TestPGCountRequestsSinceFiltersByModelAndChannel(t *testing.T) {
	st := testStore(t)
	cat := testCatalog(t, st)
	acc := accounts.New(st, st.AccountsTx())
	if _, err := acc.CreateUser(context.Background(), domain.UserInput{Nickname: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := acc.CreateUser(context.Background(), domain.UserInput{Nickname: "B"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "A", BaseURL: "https://a.test", APIKey: "sk", Status: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "B", BaseURL: "https://b.test", APIKey: "sk", Status: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "count-1", UserID: intp(1), ChannelID: intp(1), Model: "gpt", Status: "success"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "count-2", UserID: intp(1), ChannelID: intp(2), Model: "gpt", Status: "error"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "count-3", UserID: intp(1), ChannelID: intp(1), Model: "other", Status: "success"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "count-4", UserID: intp(2), ChannelID: intp(1), Model: "gpt", Status: "success"}); err != nil {
		t.Fatal(err)
	}

	count, err := st.CountRequestsSince(context.Background(), domain.UsageCountFilter{UserID: 1, Since: "1970-01-01T00:00:00Z", Model: "gpt", ChannelID: intp(1)})
	if err != nil {
		t.Fatalf("CountRequestsSince: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

func TestPGAggregateUsageFiltersByAPIKey(t *testing.T) {
	st := testStore(t)
	acc := accounts.New(st, st.AccountsTx())
	if _, err := acc.CreateUser(context.Background(), domain.UserInput{Nickname: "A"}); err != nil {
		t.Fatal(err)
	}
	keyA, err := acc.CreateKey(context.Background(), 1, domain.KeyInput{KeyName: "A"})
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := acc.CreateKey(context.Background(), 1, domain.KeyInput{KeyName: "B"})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []domain.UsageLogInput{
		{RequestID: "aggregate-a-gpt-1", UserID: intp(1), APIKeyID: intp(keyA.ID), Model: "gpt", Status: "success", TotalTokens: 10, TotalCost: "0.001000", DurationMs: 20},
		{RequestID: "aggregate-a-gpt-2", UserID: intp(1), APIKeyID: intp(keyA.ID), Model: "gpt", Status: "error", TotalTokens: 20, TotalCost: "0.002000", DurationMs: 30},
		{RequestID: "aggregate-a-other", UserID: intp(1), APIKeyID: intp(keyA.ID), Model: "other", Status: "success", TotalTokens: 5, TotalCost: "0.003000", DurationMs: 10},
		{RequestID: "aggregate-b-gpt", UserID: intp(1), APIKeyID: intp(keyB.ID), Model: "gpt", Status: "success", TotalTokens: 999, TotalCost: "9.000000", DurationMs: 99},
	} {
		if _, err := st.InsertUsageLog(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}

	logs, err := st.ListUsageLogs(context.Background(), domain.UsageLogFilter{APIKeyID: intp(keyA.ID), Page: 1, PageSize: 20})
	if err != nil || logs.Total != 3 {
		t.Fatalf("key-filtered logs = %+v, %v", logs, err)
	}
	aggregates, err := st.AggregateUsage(context.Background(), domain.UsageAggregateFilter{GroupBy: "model", APIKeyID: intp(keyA.ID), StartTime: "1970-01-01T00:00:00Z", EndTime: "2100-01-01T00:00:00Z", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if aggregates.Total != 2 || aggregates.List[0].Model != "gpt" || aggregates.List[0].RequestCount != 2 || aggregates.List[0].SuccessCount != 1 || aggregates.List[0].ErrorCount != 1 || aggregates.List[0].TotalTokens != 30 || aggregates.List[0].TotalCost != "0.003000" || aggregates.List[0].DurationMs != 50 {
		t.Fatalf("aggregates = %+v", aggregates)
	}
	for _, groupBy := range []string{"user", "api_key", "channel"} {
		result, err := st.AggregateUsage(context.Background(), domain.UsageAggregateFilter{GroupBy: groupBy, APIKeyID: intp(keyA.ID), StartTime: "1970-01-01T00:00:00Z", EndTime: "2100-01-01T00:00:00Z", Page: 1, PageSize: 20})
		if err != nil || result.Total != 1 || result.List[0].RequestCount != 3 || result.List[0].TotalTokens != 35 || result.List[0].TotalCost != "0.006000" {
			t.Fatalf("%s aggregate = %+v, %v", groupBy, result, err)
		}
	}
}

func TestPGUsageInvalidTimeParams(t *testing.T) {
	st := testStore(t)

	if _, err := st.ListUsageLogs(context.Background(), domain.UsageLogFilter{StartTime: "abc", Page: 1, PageSize: 20}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid start_time err = %v, want ErrInvalid", err)
	}
	if _, err := st.StatsOverview(context.Background(), "abc", ""); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid overview start err = %v, want ErrInvalid", err)
	}
	if _, err := st.StatsChannels(context.Background(), "", "abc"); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid channels end err = %v, want ErrInvalid", err)
	}
	if _, err := st.StatsDaily(context.Background(), "abc", "2026-01-01", 1, 100); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid date_from err = %v, want ErrInvalid", err)
	}
}

func TestPGStatsEmptyReturnsZeroValues(t *testing.T) {
	st := testStore(t)

	overview, err := st.StatsOverview(context.Background(), "", "")
	if err != nil {
		t.Fatalf("StatsOverview: %v", err)
	}
	if overview.RequestCount != int64(0) || overview.TotalCost != "0.000000" {
		t.Fatalf("unexpected empty overview: %+v", overview)
	}

	daily, err := st.StatsDaily(context.Background(), "1970-01-01", "9999-12-31", 1, 100)
	if err != nil {
		t.Fatalf("StatsDaily: %v", err)
	}
	if daily.Total != 0 || len(daily.List) != 0 {
		t.Fatalf("unexpected empty daily: %+v", daily)
	}

	channels, err := st.StatsChannels(context.Background(), "", "")
	if err != nil {
		t.Fatalf("StatsChannels: %v", err)
	}
	if len(channels.List) != 0 {
		t.Fatalf("unexpected empty channels: %+v", channels)
	}
}
