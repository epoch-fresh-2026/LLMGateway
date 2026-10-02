package quota

import (
	"testing"
	"time"
)

func TestQuotaPeriodBoundsUseUTCNaturalPeriods(t *testing.T) {
	now := time.Date(2028, time.February, 29, 23, 30, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	dayStart, dayEnd, err := QuotaPeriodBounds(now, QuotaPeriodDay)
	if err != nil {
		t.Fatal(err)
	}
	if got := dayStart.Format(time.RFC3339); got != "2028-02-29T00:00:00Z" {
		t.Fatalf("day start = %s", got)
	}
	if got := dayEnd.Format(time.RFC3339); got != "2028-03-01T00:00:00Z" {
		t.Fatalf("day end = %s", got)
	}
	monthStart, monthEnd, err := QuotaPeriodBounds(now, QuotaPeriodMonth)
	if err != nil {
		t.Fatal(err)
	}
	if got := monthStart.Format(time.RFC3339); got != "2028-02-01T00:00:00Z" {
		t.Fatalf("month start = %s", got)
	}
	if got := monthEnd.Format(time.RFC3339); got != "2028-03-01T00:00:00Z" {
		t.Fatalf("month end = %s", got)
	}
}

func TestNormalizeQuotaPolicyRequiresLimitAndValidScope(t *testing.T) {
	name, scope, period, scopeID := "daily", "user", "day", 1
	if _, err := NormalizeQuotaPolicy(QuotaPolicyInput{PolicyName: &name, ScopeType: &scope, ScopeID: &scopeID, PeriodType: &period}); err == nil {
		t.Fatal("policy without limit was accepted")
	}
	limit := int64(100)
	policy, err := NormalizeQuotaPolicy(QuotaPolicyInput{PolicyName: &name, ScopeType: &scope, ScopeID: &scopeID, PeriodType: &period, TokenLimit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	if policy.ScopeType != QuotaScopeUser || policy.PeriodType != QuotaPeriodDay || policy.TokenLimit == nil || *policy.TokenLimit != 100 {
		t.Fatalf("policy = %+v", policy)
	}
}
