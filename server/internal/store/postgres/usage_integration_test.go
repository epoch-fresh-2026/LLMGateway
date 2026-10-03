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

	testOwner(t, st)
	testOwner(t, st)
	if _, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk", Status: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := acc.CreateKey(ctx, 1, domain.KeyInput{KeyName: "active"}); err != nil {
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
	all, err := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListUsageLogs: %v", err)
	}
	if all.Total != 2 {
		t.Fatalf("total = %d, want 2", all.Total)
	}
	row := all.List[0]
	if row.ChannelName != "OpenAI" || row.TotalCost == "" {
		t.Fatalf("unexpected row: %+v", row)
	}

	filtered, _ := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Status: "error", Model: "gpt-4o", Page: 1, PageSize: 20})
	if filtered.Total != 1 {
		t.Fatalf("filtered total = %d, want 1", filtered.Total)
	}
	byUser, _ := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{UserID: intp(1), Page: 1, PageSize: 20})
	if byUser.Total != 2 {
		t.Fatalf("user filter total = %d, want 2", byUser.Total)
	}
	byRange, _ := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{StartTime: "2026-09-16T00:00:00Z", EndTime: "2026-09-16T23:59:59Z", Page: 1, PageSize: 20})
	if byRange.Total != 2 {
		t.Fatalf("range filter total = %d, want 2", byRange.Total)
	}

	if _, err := st.GetUsageLog(context.Background(), 1, first); err != nil {
		t.Fatalf("GetUsageLog: %v", err)
	}
	if _, err := st.GetUsageLog(context.Background(), 1, 404); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing log err = %v, want ErrNotFound", err)
	}

	overview, err := st.StatsOverview(context.Background(), 1, "2026-09-16T00:00:00Z", "2026-09-16T23:59:59Z")
	if err != nil {
		t.Fatalf("StatsOverview: %v", err)
	}
	if overview.RequestCount != int64(2) || overview.SuccessCount != int64(1) || overview.ErrorCount != int64(1) {
		t.Fatalf("unexpected overview: %+v", overview)
	}
	if overview.TotalTokens != 100 || overview.ActualTokens != 100 || overview.EstimatedTokens != 0 || overview.TotalCost != "0.001000" || overview.ActiveKeyCount != int64(1) {
		t.Fatalf("unexpected overview aggregates: %+v", overview)
	}

	daily, err := st.StatsDaily(context.Background(), 1, "2026-09-16", "2026-09-17", 1, 100)
	if err != nil {
		t.Fatalf("StatsDaily: %v", err)
	}
	if daily.Total != 1 {
		t.Fatalf("daily total = %d, want 1", daily.Total)
	}
	day := daily.List[0]
	if day.StatDate != "2026-09-16" || day.RequestCount != int64(2) || day.TotalCost != "0.001000" {
		t.Fatalf("unexpected day: %+v", day)
	}

	channels, err := st.StatsChannels(context.Background(), 1, "2026-09-16T00:00:00Z", "2026-09-16T23:59:59Z")
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
	ttft, err := st.StatsTTFT(context.Background(), 1, domain.TTFTStatsFilter{UserID: intp(1), ChannelID: intp(1), Model: "gpt", StartTime: "1970-01-01T00:00:00Z", EndTime: "2100-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if ttft.SampleCount != 3 || ttft.AverageMs != 266 || ttft.P50Ms != 200 || ttft.P95Ms != 500 || ttft.P99Ms != 500 {
		t.Fatalf("unexpected TTFT stats: %+v", ttft)
	}
}

func TestPGSettledTokenSeparation(t *testing.T) {
	st := testStore(t)
	testOwner(t, st)
	ctx := context.Background()
	for i, code := range []string{"", "partial_actual_upstream_stream_interrupted", "partial_estimated_upstream_stream_interrupted", "partial_estimated_settlement_failed", "pricing_error", "settlement_failed", "partial_actual_settlement_failed", "upstream_error", "partial_estimated_pricing_error", "partial_actual_pricing_error"} {
		status := "error"
		if i == 0 {
			status = "success"
		}
		_, err := st.InsertUsageLog(ctx, domain.UsageLogInput{RequestID: "separation-" + code, UserID: intp(1), Model: "gpt", Status: status, ErrorCode: code, TotalTokens: (i + 1) * 100, InputTokens: (i + 1) * 80, OutputTokens: (i + 1) * 20, TotalCost: "0.001000"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.pool.Exec(ctx, "UPDATE usage_logs SET created_at = '2026-09-16T10:00:00Z'"); err != nil {
		t.Fatal(err)
	}
	check := func(total, actual, estimated int64, cost string) {
		t.Helper()
		if total != 300 || actual != 300 || estimated != 300 || total != actual || cost != "0.002000" {
			t.Fatalf("consumption = %d/%d/%d %s", total, actual, estimated, cost)
		}
	}
	start, end := "2026-09-16T00:00:00Z", "2026-09-17T00:00:00Z"
	o, err := st.StatsOverview(ctx, 1, start, end)
	if err != nil {
		t.Fatal(err)
	}
	check(o.TotalTokens, o.ActualTokens, o.EstimatedTokens, o.TotalCost)
	if o.RequestCount != 10 {
		t.Fatal(o)
	}
	daily, err := st.StatsDaily(ctx, 1, "2026-09-16", "2026-09-16", 1, 100)
	if err != nil || len(daily.List) != 1 {
		t.Fatalf("daily: %+v %v", daily, err)
	}
	d := daily.List[0]
	check(d.TotalTokens, d.ActualTokens, d.EstimatedTokens, d.TotalCost)
	if d.InputTokens != 240 || d.OutputTokens != 60 {
		t.Fatal(d)
	}
	channels, err := st.StatsChannels(ctx, 1, start, end)
	if err != nil || len(channels.List) != 1 {
		t.Fatalf("channels: %+v %v", channels, err)
	}
	c := channels.List[0]
	check(c.TotalTokens, c.ActualTokens, c.EstimatedTokens, c.TotalCost)
	for _, group := range []string{"user", "api_key", "model", "channel"} {
		rows, err := st.AggregateUsage(ctx, 1, domain.UsageAggregateFilter{GroupBy: group, StartTime: start, EndTime: end, Page: 1, PageSize: 100})
		if err != nil || len(rows.List) != 1 {
			t.Fatalf("%s: %+v %v", group, rows, err)
		}
		r := rows.List[0]
		check(r.TotalTokens, r.ActualTokens, r.EstimatedTokens, r.TotalCost)
	}
	logs, err := st.ListUsageLogs(ctx, 1, domain.UsageLogFilter{Status: "error", Page: 1, PageSize: 100})
	if err != nil || len(logs.List) != 9 {
		t.Fatalf("audit: %+v %v", logs, err)
	}
	found := false
	for _, log := range logs.List {
		if log.ErrorCode == "partial_estimated_settlement_failed" {
			found = log.TotalTokens == 400
		}
	}
	if !found {
		t.Fatal("missing unsettled audit tokens")
	}
	count, err := st.CountTokensSince(ctx, domain.TokenCountFilter{UserID: 1, Since: start})
	if err != nil || count != 300 {
		t.Fatalf("rate tokens: %d %v", count, err)
	}
}

func TestPGEstimatedUsageIsAuditOnly(t *testing.T) {
	st := testStore(t)
	testOwner(t, st)
	ctx := context.Background()
	_, err := st.InsertUsageLog(ctx, domain.UsageLogInput{RequestID: "estimated-only", UserID: intp(1), Model: "gpt", Status: "error", ErrorCode: "partial_estimated_upstream_stream_interrupted", TotalTokens: 100, InputTokens: 80, OutputTokens: 20, CachedInputTokens: 10, TotalCost: "1.000000"})
	if err != nil {
		t.Fatal(err)
	}
	start, end := "1970-01-01T00:00:00Z", "2100-01-01T00:00:00Z"
	check := func(total, actual, estimated int64, cost string) {
		t.Helper()
		if total != 0 || actual != 0 || estimated != 100 || cost != "0.000000" {
			t.Fatalf("consumption = %d/%d/%d %s", total, actual, estimated, cost)
		}
	}
	o, err := st.StatsOverview(ctx, 1, start, end)
	if err != nil {
		t.Fatal(err)
	}
	check(o.TotalTokens, o.ActualTokens, o.EstimatedTokens, o.TotalCost)
	daily, err := st.StatsDaily(ctx, 1, "1970-01-01", "2100-01-01", 1, 100)
	if err != nil || len(daily.List) != 1 {
		t.Fatalf("daily: %+v %v", daily, err)
	}
	d := daily.List[0]
	check(d.TotalTokens, d.ActualTokens, d.EstimatedTokens, d.TotalCost)
	if d.InputTokens != 0 || d.OutputTokens != 0 || d.CachedInputTokens != 0 {
		t.Fatal(d)
	}
	channels, err := st.StatsChannels(ctx, 1, start, end)
	if err != nil || len(channels.List) != 1 {
		t.Fatalf("channels: %+v %v", channels, err)
	}
	c := channels.List[0]
	check(c.TotalTokens, c.ActualTokens, c.EstimatedTokens, c.TotalCost)
	for _, group := range []string{"user", "api_key", "model", "channel"} {
		rows, err := st.AggregateUsage(ctx, 1, domain.UsageAggregateFilter{GroupBy: group, StartTime: start, EndTime: end, Page: 1, PageSize: 100})
		if err != nil || len(rows.List) != 1 {
			t.Fatalf("%s: %+v %v", group, rows, err)
		}
		r := rows.List[0]
		check(r.TotalTokens, r.ActualTokens, r.EstimatedTokens, r.TotalCost)
	}
	count, err := st.CountTokensSince(ctx, domain.TokenCountFilter{UserID: 1, Since: start})
	if err != nil || count != 0 {
		t.Fatalf("rate tokens: %d %v", count, err)
	}
}

func TestPGCountRequestsSinceFiltersByModelAndChannel(t *testing.T) {
	st := testStore(t)
	cat := testCatalog(t, st)
	testOwner(t, st)
	testOwner(t, st)
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
	testOwner(t, st)
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
		{RequestID: "aggregate-a-gpt-2", UserID: intp(1), APIKeyID: intp(keyA.ID), Model: "gpt", Status: "error", ErrorCode: "partial_actual_upstream_stream_interrupted", TotalTokens: 20, TotalCost: "0.002000", DurationMs: 30},
		{RequestID: "aggregate-a-other", UserID: intp(1), APIKeyID: intp(keyA.ID), Model: "other", Status: "success", TotalTokens: 5, TotalCost: "0.003000", DurationMs: 10},
		{RequestID: "aggregate-b-gpt", UserID: intp(1), APIKeyID: intp(keyB.ID), Model: "gpt", Status: "success", TotalTokens: 999, TotalCost: "9.000000", DurationMs: 99},
	} {
		if _, err := st.InsertUsageLog(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}

	logs, err := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{APIKeyID: intp(keyA.ID), Page: 1, PageSize: 20})
	if err != nil || logs.Total != 3 {
		t.Fatalf("key-filtered logs = %+v, %v", logs, err)
	}
	aggregates, err := st.AggregateUsage(context.Background(), 1, domain.UsageAggregateFilter{GroupBy: "model", APIKeyID: intp(keyA.ID), StartTime: "1970-01-01T00:00:00Z", EndTime: "2100-01-01T00:00:00Z", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if aggregates.Total != 2 || aggregates.List[0].Model != "gpt" || aggregates.List[0].RequestCount != 2 || aggregates.List[0].SuccessCount != 1 || aggregates.List[0].ErrorCount != 1 || aggregates.List[0].TotalTokens != 30 || aggregates.List[0].TotalCost != "0.003000" || aggregates.List[0].DurationMs != 50 {
		t.Fatalf("aggregates = %+v", aggregates)
	}
	for _, groupBy := range []string{"user", "api_key", "channel"} {
		result, err := st.AggregateUsage(context.Background(), 1, domain.UsageAggregateFilter{GroupBy: groupBy, APIKeyID: intp(keyA.ID), StartTime: "1970-01-01T00:00:00Z", EndTime: "2100-01-01T00:00:00Z", Page: 1, PageSize: 20})
		if err != nil || result.Total != 1 || result.List[0].RequestCount != 3 || result.List[0].TotalTokens != 35 || result.List[0].TotalCost != "0.006000" {
			t.Fatalf("%s aggregate = %+v, %v", groupBy, result, err)
		}
	}
}

func TestPGUsageOwnerIsolation(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	testOwner(t, st)
	testOwner(t, st)
	if _, err := st.InsertUsageLog(ctx, domain.UsageLogInput{RequestID: "one", UserID: intp(1), Model: "gpt", Status: "success", TotalTokens: 10}); err != nil {
		t.Fatal(err)
	}
	second, err := st.InsertUsageLog(ctx, domain.UsageLogInput{RequestID: "two", UserID: intp(2), Model: "gpt", Status: "success", TotalTokens: 20})
	if err != nil {
		t.Fatal(err)
	}

	logs, err := st.ListUsageLogs(ctx, 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Total != 1 {
		t.Fatalf("owner 1 logs = %d, want 1", logs.Total)
	}
	overview, err := st.StatsOverview(ctx, 1, "1970-01-01T00:00:00Z", "9999-12-31T23:59:59Z")
	if err != nil {
		t.Fatal(err)
	}
	if overview.RequestCount != 1 {
		t.Fatalf("owner 1 request_count = %d, want 1", overview.RequestCount)
	}
	if _, err := st.GetUsageLog(ctx, 1, second); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-owner GetUsageLog err = %v, want ErrNotFound", err)
	}
}

func TestPGUsageInvalidTimeParams(t *testing.T) {
	st := testStore(t)

	if _, err := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{StartTime: "abc", Page: 1, PageSize: 20}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid start_time err = %v, want ErrInvalid", err)
	}
	if _, err := st.StatsOverview(context.Background(), 1, "abc", ""); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid overview start err = %v, want ErrInvalid", err)
	}
	if _, err := st.StatsChannels(context.Background(), 1, "", "abc"); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid channels end err = %v, want ErrInvalid", err)
	}
	if _, err := st.StatsDaily(context.Background(), 1, "abc", "2026-01-01", 1, 100); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid date_from err = %v, want ErrInvalid", err)
	}
}

func TestPGStatsEmptyReturnsZeroValues(t *testing.T) {
	st := testStore(t)

	overview, err := st.StatsOverview(context.Background(), 1, "", "")
	if err != nil {
		t.Fatalf("StatsOverview: %v", err)
	}
	if overview.RequestCount != int64(0) || overview.TotalCost != "0.000000" {
		t.Fatalf("unexpected empty overview: %+v", overview)
	}

	daily, err := st.StatsDaily(context.Background(), 1, "1970-01-01", "9999-12-31", 1, 100)
	if err != nil {
		t.Fatalf("StatsDaily: %v", err)
	}
	if daily.Total != 0 || len(daily.List) != 0 {
		t.Fatalf("unexpected empty daily: %+v", daily)
	}

	channels, err := st.StatsChannels(context.Background(), 1, "", "")
	if err != nil {
		t.Fatalf("StatsChannels: %v", err)
	}
	if len(channels.List) != 0 {
		t.Fatalf("unexpected empty channels: %+v", channels)
	}
}
