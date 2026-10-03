package storefake

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"LLMGateway/server/internal/store"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func seedLog(st *Store, userID, channelID *int, model, status, cost string, tokens int, createdAt string) {
	id := st.nextUsageLogID
	st.usageLogs = append(st.usageLogs, domain.UsageLog{
		ID:                   id,
		RequestID:            fmt.Sprintf("req-%d", id),
		UserID:               userID,
		ChannelID:            channelID,
		Model:                model,
		Status:               status,
		TotalCost:            cost,
		TotalTokens:          tokens,
		CreatedAt:            createdAt,
		UnitPriceInputPer1M:  "0.10000000",
		UnitPriceOutputPer1M: "0.20000000",
	})
	st.nextUsageLogID++
}

func TestUsageLogsFilterAndPaging(t *testing.T) {
	st := New()
	seedLog(st, intp(1), intp(1), "gpt-4o-mini", "success", "0.001000", 100, "2026-09-16T10:00:00Z")
	seedLog(st, intp(1), intp(1), "gpt-4o", "error", "0.002000", 200, "2026-09-16T11:00:00Z")
	seedLog(st, intp(2), intp(2), "gpt-4o-mini", "success", "0.003000", 300, "2026-09-17T10:00:00Z")

	all, err := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListUsageLogs: %v", err)
	}
	if all.Total != 2 {
		t.Fatalf("total = %d, want 2", all.Total)
	}
	// Ordered by created_at DESC; owner 1's latest log is req-2.
	first := all.List[0]
	if first.Model != "gpt-4o" || first.Status != "error" {
		t.Fatalf("unexpected first row: %+v", first)
	}

	byUser, _ := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{UserID: intp(1), Page: 1, PageSize: 20})
	if byUser.Total != 2 {
		t.Fatalf("user filter total = %d, want 2", byUser.Total)
	}
	byModel, _ := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Model: "gpt-4o", Page: 1, PageSize: 20})
	if byModel.Total != 1 {
		t.Fatalf("model filter total = %d, want 1", byModel.Total)
	}
	byStatus, _ := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Status: "error", Page: 1, PageSize: 20})
	if byStatus.Total != 1 {
		t.Fatalf("status filter total = %d, want 1", byStatus.Total)
	}
	byRange, _ := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{StartTime: "2026-09-16T00:00:00Z", EndTime: "2026-09-16T23:59:59Z", Page: 1, PageSize: 20})
	if byRange.Total != 2 {
		t.Fatalf("range filter total = %d, want 2", byRange.Total)
	}

	paged, _ := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 2})
	if len(paged.List) != 2 || paged.Total != 2 {
		t.Fatalf("paging = %d rows, total %d; want 2 rows, total 2", len(paged.List), paged.Total)
	}

	if _, err := st.GetUsageLog(context.Background(), 1, 1); err != nil {
		t.Fatalf("GetUsageLog: %v", err)
	}
	if _, err := st.GetUsageLog(context.Background(), 1, 404); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing log err = %v, want ErrNotFound", err)
	}
}

func TestCountRequestsSinceFiltersByModelAndChannel(t *testing.T) {
	st := New()
	seedLog(st, intp(1), intp(1), "gpt", "success", "0.001000", 100, "2026-09-16T10:00:00Z")
	seedLog(st, intp(1), intp(2), "gpt", "error", "0.001000", 100, "2026-09-16T10:00:10Z")
	seedLog(st, intp(1), intp(1), "other", "success", "0.001000", 100, "2026-09-16T10:00:20Z")
	seedLog(st, intp(2), intp(1), "gpt", "success", "0.001000", 100, "2026-09-16T10:00:30Z")

	count, err := st.CountRequestsSince(context.Background(), domain.UsageCountFilter{UserID: 1, Since: "2026-09-16T09:59:00Z", Model: "gpt", ChannelID: intp(1)})
	if err != nil {
		t.Fatalf("CountRequestsSince: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

func TestUsageInvalidTimeParams(t *testing.T) {
	st := New()

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
	if _, err := st.StatsDaily(context.Background(), 1, "2026-01-01", "2026-13-99", 1, 100); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("invalid date_to err = %v, want ErrInvalid", err)
	}
}

func TestInsertUsageLogResolvesChannelName(t *testing.T) {
	st := New()
	cat := newCatalog(st)
	if _, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk", Status: 1}); err != nil {
		t.Fatal(err)
	}
	id, err := st.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "req-1", UserID: intp(1), ChannelID: intp(1), Model: "gpt", Status: "success", TotalTokens: 10, TotalCost: "0.000100"})
	if err != nil {
		t.Fatalf("InsertUsageLog: %v", err)
	}
	dto, err := st.GetUsageLog(context.Background(), 1, id)
	if err != nil {
		t.Fatal(err)
	}
	if dto.ChannelName != "OpenAI" {
		t.Fatalf("channel_name = %v, want OpenAI", dto.ChannelName)
	}
	if dto.CreatedAt == "" {
		t.Fatal("created_at should be set")
	}
}

func TestStatsEmptyReturnsZeroValues(t *testing.T) {
	st := New()

	overview, err := st.StatsOverview(context.Background(), 1, "", "")
	if err != nil {
		t.Fatalf("StatsOverview: %v", err)
	}
	if overview.RequestCount != 0 || overview.TotalCost != "0.000000" || overview.ActiveKeyCount != 0 {
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

func TestSettledTokenSeparation(t *testing.T) {
	st := New()
	ctx := context.Background()
	for i, code := range []string{"", "partial_actual_upstream_stream_interrupted", "partial_estimated_upstream_stream_interrupted", "partial_estimated_settlement_failed", "pricing_error", "settlement_failed", "partial_actual_settlement_failed", "upstream_error", "partial_estimated_pricing_error", "partial_actual_pricing_error"} {
		status := "error"
		if i == 0 {
			status = "success"
		}
		seedLog(st, intp(1), intp(1), "gpt", status, "0.001000", (i+1)*100, "2026-09-16T10:00:00Z")
		st.usageLogs[len(st.usageLogs)-1].ErrorCode = code
	}
	check := func(total, actual, estimated int64, cost string) {
		t.Helper()
		if total != 300 || actual != 300 || estimated != 300 || total != actual || cost != "0.002000" {
			t.Fatalf("consumption = %d/%d/%d %s", total, actual, estimated, cost)
		}
	}
	overview, err := st.StatsOverview(ctx, 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	check(overview.TotalTokens, overview.ActualTokens, overview.EstimatedTokens, overview.TotalCost)
	if overview.RequestCount != 10 {
		t.Fatal(overview)
	}
	daily, err := st.StatsDaily(ctx, 1, "2026-09-16", "2026-09-16", 1, 100)
	if err != nil || len(daily.List) != 1 {
		t.Fatalf("daily: %+v %v", daily, err)
	}
	d := daily.List[0]
	check(d.TotalTokens, d.ActualTokens, d.EstimatedTokens, d.TotalCost)
	channels, err := st.StatsChannels(ctx, 1, "", "")
	if err != nil || len(channels.List) != 1 {
		t.Fatalf("channels: %+v %v", channels, err)
	}
	c := channels.List[0]
	check(c.TotalTokens, c.ActualTokens, c.EstimatedTokens, c.TotalCost)
	for _, group := range []string{"user", "api_key", "model", "channel"} {
		rows, err := st.AggregateUsage(ctx, 1, domain.UsageAggregateFilter{GroupBy: group, Page: 1, PageSize: 100})
		if err != nil || len(rows.List) != 1 {
			t.Fatalf("%s: %+v %v", group, rows, err)
		}
		r := rows.List[0]
		check(r.TotalTokens, r.ActualTokens, r.EstimatedTokens, r.TotalCost)
	}
	log, err := st.GetUsageLog(ctx, 1, 4)
	if err != nil || log.Status != "error" || log.ErrorCode != "partial_estimated_settlement_failed" || log.TotalTokens != 400 {
		t.Fatalf("audit: %+v %v", log, err)
	}
	count, err := st.CountTokensSince(ctx, domain.TokenCountFilter{UserID: 1, Since: "2026-09-16T00:00:00Z"})
	if err != nil || count != 300 {
		t.Fatalf("rate tokens: %d %v", count, err)
	}
}

func TestEstimatedUsageIsAuditOnly(t *testing.T) {
	st := New()
	ctx := context.Background()
	_, err := st.InsertUsageLog(ctx, domain.UsageLogInput{RequestID: "estimated-only", UserID: intp(1), APIKeyID: intp(1), ChannelID: intp(1), Model: "gpt", Status: "error", ErrorCode: "partial_estimated_upstream_stream_interrupted", TotalTokens: 100, InputTokens: 80, OutputTokens: 20, CachedInputTokens: 10, TotalCost: "1.000000"})
	if err != nil {
		t.Fatal(err)
	}
	o, err := st.StatsOverview(ctx, 1, "", "")
	if err != nil || o.TotalTokens != 0 || o.ActualTokens != 0 || o.EstimatedTokens != 100 || o.TotalCost != "0.000000" {
		t.Fatalf("overview: %+v %v", o, err)
	}
	daily, err := st.StatsDaily(ctx, 1, "1970-01-01", "2100-01-01", 1, 100)
	if err != nil || len(daily.List) != 1 {
		t.Fatalf("daily: %+v %v", daily, err)
	}
	d := daily.List[0]
	if d.TotalTokens != 0 || d.ActualTokens != 0 || d.EstimatedTokens != 100 || d.InputTokens != 0 || d.OutputTokens != 0 || d.CachedInputTokens != 0 || d.TotalCost != "0.000000" {
		t.Fatal(d)
	}
	channels, err := st.StatsChannels(ctx, 1, "", "")
	if err != nil || len(channels.List) != 1 {
		t.Fatalf("channels: %+v %v", channels, err)
	}
	c := channels.List[0]
	if c.TotalTokens != 0 || c.ActualTokens != 0 || c.EstimatedTokens != 100 || c.TotalCost != "0.000000" {
		t.Fatal(c)
	}
	for _, group := range []string{"user", "api_key", "model", "channel"} {
		rows, err := st.AggregateUsage(ctx, 1, domain.UsageAggregateFilter{GroupBy: group, Page: 1, PageSize: 100})
		if err != nil || len(rows.List) != 1 {
			t.Fatalf("%s: %+v %v", group, rows, err)
		}
		r := rows.List[0]
		if r.TotalTokens != 0 || r.ActualTokens != 0 || r.EstimatedTokens != 100 || r.TotalCost != "0.000000" {
			t.Fatal(r)
		}
	}
	for _, filter := range []domain.TokenCountFilter{{Since: "1970-01-01T00:00:00Z"}, {UserID: 1, APIKeyID: intp(1), ChannelID: intp(1), Model: "gpt", Since: "1970-01-01T00:00:00Z"}} {
		count, err := st.CountTokensSince(ctx, filter)
		if err != nil || count != 0 {
			t.Fatalf("rate tokens: %d %v", count, err)
		}
	}
}

func TestStatsAggregation(t *testing.T) {
	st := New()
	cat := newCatalog(st)
	if _, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "OpenAI", BaseURL: "https://api.test", APIKey: "sk", Status: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "Azure", BaseURL: "https://api2.test", APIKey: "sk", Status: 1}); err != nil {
		t.Fatal(err)
	}
	seedLog(st, intp(1), intp(1), "gpt", "success", "0.001000", 100, "2026-09-16T10:00:00Z")
	seedLog(st, intp(1), intp(2), "gpt", "error", "0.002000", 200, "2026-09-16T11:00:00Z")
	seedLog(st, intp(2), intp(1), "gpt", "success", "0.003000", 300, "2026-09-17T10:00:00Z")

	overview, err := st.StatsOverview(context.Background(), 1, "2026-09-16T00:00:00Z", "2026-09-16T23:59:59Z")
	if err != nil {
		t.Fatal(err)
	}
	if overview.RequestCount != 2 || overview.SuccessCount != 1 || overview.ErrorCount != 1 {
		t.Fatalf("unexpected overview counts: %+v", overview)
	}
	if overview.TotalTokens != 100 || overview.ActualTokens != 100 || overview.EstimatedTokens != 0 || overview.TotalCost != "0.001000" || overview.ActiveKeyCount != 0 {
		t.Fatalf("unexpected overview aggregates: %+v", overview)
	}

	daily, err := st.StatsDaily(context.Background(), 1, "2026-09-16", "2026-09-17", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if daily.Total != 1 {
		t.Fatalf("daily total = %d, want 1", daily.Total)
	}
	firstDay := daily.List[0]
	if firstDay.StatDate != "2026-09-16" || firstDay.RequestCount != 2 || firstDay.TotalCost != "0.001000" {
		t.Fatalf("unexpected first day: %+v", firstDay)
	}

	channels, err := st.StatsChannels(context.Background(), 1, "2026-09-16T00:00:00Z", "2026-09-16T23:59:59Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(channels.List) != 2 {
		t.Fatalf("channels len = %d, want 2", len(channels.List))
	}
	top := channels.List[0]
	if top.ChannelName == "" || top.RequestCount != 1 {
		t.Fatalf("unexpected channel stat: %+v", top)
	}
}
