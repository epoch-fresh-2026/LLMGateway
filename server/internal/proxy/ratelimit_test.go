package proxy

import (
	"encoding/json"
	"testing"
	"time"

	domain "LLMGateway/server/internal/testutil/testtypes"
)

func TestRateLimitOverrideParsesAllMetricsAndOnlyKeyScope(t *testing.T) {
	overrides := parseRateLimitOverrides(json.RawMessage(`{"rpm":2,"tpm":100,"rpd":3,"concurrency":1}`))
	if overrides.RPM != 2 || overrides.TPM != 100 || overrides.RPD != 3 || overrides.Concurrency != 1 {
		t.Fatalf("overrides = %+v", overrides)
	}
	if _, ok := applicableOverride(overrides, domain.RateLimitRuleDTO{TargetType: "user", Metric: "rpm"}); ok {
		t.Fatal("user rule incorrectly accepted key override")
	}
	if value, ok := applicableOverride(overrides, domain.RateLimitRuleDTO{TargetType: "api_key", Metric: "tpm"}); !ok || value != 100 {
		t.Fatalf("api key override = %d,%v", value, ok)
	}
}

func TestAPIKeyRPMOverrideUsesConfiguredWindow(t *testing.T) {
	overrides := parseRateLimitOverrides(json.RawMessage(`{"rpm":2,"rpm_window_seconds":300}`))
	if overrides.RPMWindowSeconds != 300 {
		t.Fatalf("window = %d, want 300", overrides.RPMWindowSeconds)
	}
}

func TestSlidingWindowCounterUsesIntegerWeightedPreviousBucket(t *testing.T) {
	now := time.Unix(125, 0).UTC()
	if got := slidingWindowCount(10, 20, 10, now, time.Unix(120, 0).UTC()); got != 25 {
		t.Fatalf("sliding count = %d, want 25", got)
	}
}
