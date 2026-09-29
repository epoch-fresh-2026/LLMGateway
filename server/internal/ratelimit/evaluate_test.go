package ratelimit

import (
	"encoding/json"
	"testing"
	"time"
)

func TestOverridesParseAllMetricsAndOnlyKeyScope(t *testing.T) {
	overrides := ParseOverrides(json.RawMessage(`{"rpm":2,"tpm":100,"concurrency":1}`))
	if overrides.RPM != 2 || overrides.TPM != 100 || overrides.Concurrency != 1 {
		t.Fatalf("overrides = %+v", overrides)
	}
	if _, ok := overrides.ForRule(RateLimitRuleDTO{TargetType: "user", Metric: "rpm"}); ok {
		t.Fatal("user rule incorrectly accepted key override")
	}
	if value, ok := overrides.ForRule(RateLimitRuleDTO{TargetType: "api_key", Metric: "tpm"}); !ok || value != 100 {
		t.Fatalf("api key override = %d,%v", value, ok)
	}
}

func TestOverridesParseRPMWindow(t *testing.T) {
	overrides := ParseOverrides(json.RawMessage(`{"rpm":2,"rpm_window_seconds":300}`))
	if overrides.RPMWindowSeconds != 300 {
		t.Fatalf("window = %d, want 300", overrides.RPMWindowSeconds)
	}
}

func TestSlidingWindowCountWeightsPreviousBucket(t *testing.T) {
	now := time.Unix(125, 0).UTC()
	if got := SlidingWindowCount(10, 20, 10, now, time.Unix(120, 0).UTC()); got != 25 {
		t.Fatalf("sliding count = %d, want 25", got)
	}
}

func TestMatchesTargetScopes(t *testing.T) {
	tests := []struct {
		name string
		rule RateLimitRuleDTO
		want bool
	}{
		{"user wildcard", RateLimitRuleDTO{TargetType: "user", TargetValue: "*"}, true},
		{"user match", RateLimitRuleDTO{TargetType: "user", TargetValue: "7"}, true},
		{"user other", RateLimitRuleDTO{TargetType: "user", TargetValue: "8"}, false},
		{"key match", RateLimitRuleDTO{TargetType: "api_key", TargetValue: "9"}, true},
		{"model match", RateLimitRuleDTO{TargetType: "model", TargetValue: "gpt"}, true},
		{"model other", RateLimitRuleDTO{TargetType: "model", TargetValue: "other"}, false},
		{"channel excluded", RateLimitRuleDTO{TargetType: "channel", TargetValue: "*"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchesTarget(tt.rule, 7, 9, "gpt"); got != tt.want {
				t.Fatalf("MatchesTarget(%+v) = %v, want %v", tt.rule, got, tt.want)
			}
		})
	}
}
