package httpapi

import (
	"context"
	"net/http"
	"testing"

	"LLMGateway/server/internal/testutil/storefake"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func intPtr(value int) *int { return &value }

func newUsageTestHandler(t *testing.T) (*Server, *storefake.Store) {
	t.Helper()
	st := storefake.New()
	server := NewServer(st)
	server.enforceSession = false
	return server, st
}

func TestUsageInvalidTimeParamsReturnBadRequest(t *testing.T) {
	handler := newTestServer()

	for _, path := range []string{
		"/admin/stats/overview?start_time=abc",
		"/admin/stats/channels?end_time=abc",
		"/admin/stats/daily?date_from=abc",
		"/admin/stats/daily?date_to=abc",
		"/admin/usage-logs?start_time=abc",
		"/admin/usage-logs?end_time=abc",
	} {
		res := adminRaw(t, handler, http.MethodGet, path, nil)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400; body=%s", path, res.Code, res.Body.String())
		}
	}
}

func TestUsageLogsAndStats(t *testing.T) {
	handler, st := newUsageTestHandler(t)

	// Empty data must still return code=0 with zero values.
	overview := adminDo(t, handler, http.MethodGet, "/admin/stats/overview", nil)
	if overview["data"].(map[string]any)["request_count"].(float64) != 0 {
		t.Fatalf("empty overview = %+v", overview["data"])
	}
	daily := adminDo(t, handler, http.MethodGet, "/admin/stats/daily", nil)
	if daily["data"].(map[string]any)["total"].(float64) != 0 {
		t.Fatalf("empty daily = %+v", daily["data"])
	}
	channels := adminDo(t, handler, http.MethodGet, "/admin/stats/channels", nil)
	if len(channels["data"].(map[string]any)["list"].([]any)) != 0 {
		t.Fatalf("empty channels = %+v", channels["data"])
	}

	if _, err := st.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "req-1", UserID: intPtr(1), ChannelID: intPtr(1), Model: "gpt", Status: "success", TotalTokens: 100, TotalCost: "0.001000"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "req-2", UserID: intPtr(1), ChannelID: intPtr(1), Model: "gpt-4o", Status: "error", TotalTokens: 200, TotalCost: "0.002000"}); err != nil {
		t.Fatal(err)
	}
	st.SeedUser("usage-user", "hash")
	if _, err := handler.accounts.CreateKey(context.Background(), 1, domain.KeyInput{KeyName: "active"}); err != nil {
		t.Fatal(err)
	}

	logs := adminDo(t, handler, http.MethodGet, "/admin/usage-logs?page=1&page_size=20", nil)
	listData := logs["data"].(map[string]any)
	if listData["total"].(float64) != 2 {
		t.Fatalf("logs total = %v, want 2", listData["total"])
	}
	row := listData["list"].([]any)[0].(map[string]any)
	if row["request_id"] == "" || row["total_cost"] == "" {
		t.Fatalf("unexpected log row: %+v", row)
	}

	single := adminDo(t, handler, http.MethodGet, "/admin/usage-logs/1", nil)
	if single["data"].(map[string]any)["request_id"] != "req-1" {
		t.Fatalf("unexpected single log: %+v", single["data"])
	}
	missing := adminRaw(t, handler, http.MethodGet, "/admin/usage-logs/404", nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing log status = %d, want 404", missing.Code)
	}

	filtered := adminDo(t, handler, http.MethodGet, "/admin/usage-logs?status=error&model=gpt-4o", nil)
	if filtered["data"].(map[string]any)["total"].(float64) != 1 {
		t.Fatalf("filtered total = %+v", filtered["data"])
	}

	overview = adminDo(t, handler, http.MethodGet, "/admin/stats/overview", nil)
	ov := overview["data"].(map[string]any)
	if ov["request_count"].(float64) != 2 || ov["success_count"].(float64) != 1 || ov["error_count"].(float64) != 1 {
		t.Fatalf("unexpected overview: %+v", ov)
	}
	if ov["total_tokens"].(float64) != 100 || ov["actual_tokens"].(float64) != 100 || ov["estimated_tokens"].(float64) != 0 || ov["total_cost"] != "0.001000" || ov["active_key_count"].(float64) != 1 {
		t.Fatalf("unexpected overview aggregates: %+v", ov)
	}

	daily = adminDo(t, handler, http.MethodGet, "/admin/stats/daily", nil)
	dailyData := daily["data"].(map[string]any)
	if dailyData["total"].(float64) != 1 {
		t.Fatalf("daily total = %+v", dailyData)
	}
	day := dailyData["list"].([]any)[0].(map[string]any)
	if day["request_count"].(float64) != 2 || day["total_tokens"].(float64) != 100 || day["actual_tokens"].(float64) != 100 || day["estimated_tokens"].(float64) != 0 || day["total_cost"] != "0.001000" {
		t.Fatalf("unexpected day: %+v", day)
	}

	channels = adminDo(t, handler, http.MethodGet, "/admin/stats/channels", nil)
	channelList := channels["data"].(map[string]any)["list"].([]any)
	if len(channelList) != 1 {
		t.Fatalf("channels len = %d, want 1", len(channelList))
	}
	channel := channelList[0].(map[string]any)
	if channel["channel_id"].(float64) != 1 || channel["request_count"].(float64) != 2 || channel["total_tokens"].(float64) != 100 || channel["actual_tokens"].(float64) != 100 || channel["estimated_tokens"].(float64) != 0 || channel["total_cost"] != "0.001000" {
		t.Fatalf("unexpected channel stat: %+v", channel)
	}
}

func TestUsageSettledErrorCodeContract(t *testing.T) {
	handler, st := newUsageTestHandler(t)
	for i, code := range []string{"", "partial_actual_upstream_stream_interrupted", "partial_estimated_upstream_stream_interrupted", "partial_estimated_settlement_failed", "partial_actual_settlement_failed", "partial_estimated_pricing_error", "partial_actual_pricing_error", "pricing_error", "settlement_failed", "upstream_error"} {
		status := "error"
		if i == 0 {
			status = "success"
		}
		_, err := st.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "contract-" + code, UserID: intPtr(1), APIKeyID: intPtr(1), ChannelID: intPtr(1), Model: "gpt", Status: status, ErrorCode: code, TotalTokens: (i + 1) * 100, TotalCost: "0.001000"})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/admin/stats/overview", "/admin/stats/daily", "/admin/stats/channels", "/admin/stats/usage?group_by=user", "/admin/stats/usage?group_by=api_key", "/admin/stats/usage?group_by=model", "/admin/stats/usage?group_by=channel"} {
		data := adminDo(t, handler, http.MethodGet, path, nil)["data"].(map[string]any)
		if path != "/admin/stats/overview" {
			list := data["list"].([]any)
			if len(list) != 1 {
				t.Fatalf("%s: %+v", path, data)
			}
			data = list[0].(map[string]any)
		}
		for field, want := range map[string]float64{"request_count": 10, "success_count": 1, "error_count": 9, "total_tokens": 300, "actual_tokens": 300, "estimated_tokens": 300} {
			if data[field] != want {
				t.Fatalf("%s %s = %v, want %v", path, field, data[field], want)
			}
		}
		if data["total_cost"] != "0.002000" {
			t.Fatalf("%s: %+v", path, data)
		}
	}
	logs := adminDo(t, handler, http.MethodGet, "/admin/usage-logs?status=error&page_size=100", nil)["data"].(map[string]any)
	if logs["total"] != float64(9) {
		t.Fatal(logs)
	}
	found := false
	for _, item := range logs["list"].([]any) {
		log := item.(map[string]any)
		if log["error_code"] == "partial_estimated_settlement_failed" {
			found = log["status"] == "error" && log["total_tokens"] == float64(400)
		}
	}
	if !found {
		t.Fatal("missing unsettled audit tokens")
	}
}

func TestUsageEstimatedOnlyContract(t *testing.T) {
	handler, st := newUsageTestHandler(t)
	_, err := st.InsertUsageLog(context.Background(), domain.UsageLogInput{RequestID: "estimated-only", UserID: intPtr(1), APIKeyID: intPtr(1), ChannelID: intPtr(1), Model: "gpt", Status: "error", ErrorCode: "partial_estimated_client_canceled", TotalTokens: 100, InputTokens: 80, OutputTokens: 20, CachedInputTokens: 10, TotalCost: "1.000000"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/admin/stats/overview", "/admin/stats/daily", "/admin/stats/channels", "/admin/stats/usage?group_by=user", "/admin/stats/usage?group_by=api_key", "/admin/stats/usage?group_by=model", "/admin/stats/usage?group_by=channel"} {
		data := adminDo(t, handler, http.MethodGet, path, nil)["data"].(map[string]any)
		if path != "/admin/stats/overview" {
			list := data["list"].([]any)
			if len(list) != 1 {
				t.Fatalf("%s: %+v", path, data)
			}
			data = list[0].(map[string]any)
		}
		for field, want := range map[string]float64{"request_count": 1, "success_count": 0, "error_count": 1, "total_tokens": 0, "actual_tokens": 0, "estimated_tokens": 100} {
			if data[field] != want {
				t.Fatalf("%s %s = %v, want %v", path, field, data[field], want)
			}
		}
		if data["total_cost"] != "0.000000" {
			t.Fatalf("%s: %+v", path, data)
		}
		if path == "/admin/stats/daily" {
			for _, field := range []string{"input_tokens", "output_tokens", "cached_input_tokens"} {
				if data[field] != float64(0) {
					t.Fatalf("%s: %+v", field, data)
				}
			}
		}
	}
	count, err := st.CountTokensSince(context.Background(), domain.TokenCountFilter{UserID: 1, Since: "1970-01-01T00:00:00Z"})
	if err != nil || count != 0 {
		t.Fatalf("rate tokens: %d %v", count, err)
	}
}

func TestTTFTStatsFiltersAndPercentiles(t *testing.T) {
	handler, st := newUsageTestHandler(t)
	userID, keyID, channelID := 1, 9, 11
	for _, input := range []domain.UsageLogInput{
		{RequestID: "ttft-1", UserID: &userID, APIKeyID: &keyID, ChannelID: &channelID, Model: "gpt", Status: "success", TTFTMs: intPtr(100)},
		{RequestID: "ttft-2", UserID: &userID, APIKeyID: &keyID, ChannelID: &channelID, Model: "gpt", Status: "success", TTFTMs: intPtr(200)},
		{RequestID: "ttft-3", UserID: &userID, APIKeyID: &keyID, ChannelID: &channelID, Model: "gpt", Status: "error", TTFTMs: intPtr(500)},
		{RequestID: "ttft-no-value", UserID: &userID, APIKeyID: &keyID, ChannelID: &channelID, Model: "gpt", Status: "success"},
		{RequestID: "ttft-other", Model: "other", Status: "success", TTFTMs: intPtr(1000)},
	} {
		if _, err := st.InsertUsageLog(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}

	stats := adminDo(t, handler, http.MethodGet, "/admin/stats/ttft?user_id=1&api_key_id=9&channel_id=11&model=gpt&start_time=2020-01-01T00:00:00Z&end_time=2100-01-01T00:00:00Z", nil)
	data := stats["data"].(map[string]any)
	for field, want := range map[string]float64{
		"sample_count": 3,
		"average_ms":   266,
		"p50_ms":       200,
		"p95_ms":       500,
		"p99_ms":       500,
	} {
		if got := data[field].(float64); got != want {
			t.Fatalf("%s = %v, want %v; data=%+v", field, got, want, data)
		}
	}

	empty := adminDo(t, handler, http.MethodGet, "/admin/stats/ttft?model=missing", nil)
	if empty["data"].(map[string]any)["sample_count"].(float64) != 0 {
		t.Fatalf("empty TTFT stats = %+v", empty["data"])
	}

	invalid := adminRaw(t, handler, http.MethodGet, "/admin/stats/ttft?api_key_id=bad", nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid api_key_id status = %d, want 400", invalid.Code)
	}
}

func TestUsageLogsFilterAndAggregateByAPIKey(t *testing.T) {
	handler, st := newUsageTestHandler(t)
	userID, keyA, keyB, channelA, channelB := 1, 10, 20, 100, 200
	for _, input := range []domain.UsageLogInput{
		{RequestID: "key-a-gpt-success", UserID: &userID, APIKeyID: &keyA, ChannelID: &channelA, Model: "gpt", Status: "success", TotalTokens: 100, TotalCost: "0.001000", DurationMs: 40},
		{RequestID: "key-a-gpt-error", UserID: &userID, APIKeyID: &keyA, ChannelID: &channelB, Model: "gpt", Status: "error", TotalTokens: 50, TotalCost: "0.002000", DurationMs: 60},
		{RequestID: "key-a-other", UserID: &userID, APIKeyID: &keyA, ChannelID: &channelA, Model: "other", Status: "success", TotalTokens: 30, TotalCost: "0.003000", DurationMs: 20},
		{RequestID: "key-b-gpt", UserID: &userID, APIKeyID: &keyB, ChannelID: &channelA, Model: "gpt", Status: "success", TotalTokens: 999, TotalCost: "9.000000", DurationMs: 99},
	} {
		if _, err := st.InsertUsageLog(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}

	logs := adminDo(t, handler, http.MethodGet, "/admin/usage-logs?api_key_id=10&page=1&page_size=20", nil)
	if logs["data"].(map[string]any)["total"].(float64) != 3 {
		t.Fatalf("key-filtered logs = %+v", logs["data"])
	}

	byModel := adminDo(t, handler, http.MethodGet, "/admin/stats/usage?group_by=model&api_key_id=10&start_time=2020-01-01T00:00:00Z&end_time=2100-01-01T00:00:00Z", nil)
	modelData := byModel["data"].(map[string]any)
	if modelData["total"].(float64) != 2 {
		t.Fatalf("model aggregation = %+v", modelData)
	}
	model := modelData["list"].([]any)[0].(map[string]any)
	if model["model"] != "gpt" || model["request_count"].(float64) != 2 || model["success_count"].(float64) != 1 || model["error_count"].(float64) != 1 || model["total_tokens"].(float64) != 100 || model["actual_tokens"].(float64) != 100 || model["estimated_tokens"].(float64) != 0 || model["total_cost"] != "0.001000" || model["duration_ms"].(float64) != 100 {
		t.Fatalf("gpt aggregation = %+v", model)
	}

	byChannel := adminDo(t, handler, http.MethodGet, "/admin/stats/usage?group_by=channel&api_key_id=10&date_from=2020-01-01&date_to=2100-01-01", nil)
	channelData := byChannel["data"].(map[string]any)
	if channelData["total"].(float64) != 2 {
		t.Fatalf("channel aggregation = %+v", channelData)
	}
	byUser := adminDo(t, handler, http.MethodGet, "/admin/stats/usage?group_by=user&api_key_id=10", nil)
	if row := byUser["data"].(map[string]any)["list"].([]any)[0].(map[string]any); row["user_id"].(float64) != 1 || row["request_count"].(float64) != 3 || row["total_tokens"].(float64) != 130 || row["actual_tokens"].(float64) != 130 || row["estimated_tokens"].(float64) != 0 || row["total_cost"] != "0.004000" {
		t.Fatalf("user aggregation = %+v", row)
	}
	byKey := adminDo(t, handler, http.MethodGet, "/admin/stats/usage?group_by=api_key&page=1&page_size=1", nil)
	keyData := byKey["data"].(map[string]any)
	if keyData["total"].(float64) != 2 || len(keyData["list"].([]any)) != 1 {
		t.Fatalf("key aggregation pagination = %+v", keyData)
	}
	empty := adminDo(t, handler, http.MethodGet, "/admin/stats/usage?group_by=model&api_key_id=404", nil)
	if data := empty["data"].(map[string]any); data["total"].(float64) != 0 || len(data["list"].([]any)) != 0 {
		t.Fatalf("empty aggregation = %+v", data)
	}

	for _, path := range []string{
		"/admin/usage-logs?api_key_id=sk-secret",
		"/admin/stats/usage?group_by=unknown",
		"/admin/stats/usage?group_by=model&api_key_id=sk-secret",
		"/admin/stats/usage?group_by=model&start_time=2026-01-01T00:00:00Z&date_from=2026-01-01",
	} {
		if res := adminRaw(t, handler, http.MethodGet, path, nil); res.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400; body=%s", path, res.Code, res.Body.String())
		}
	}
}
