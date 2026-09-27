package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestChatCompletionSettlementStaysInProxy guards the boundary from #86: the
// success path delegates to proxy settlement and does not sequence balance or
// channel writes itself, and the settlement orchestration uses only the
// transaction primitives.
func TestChatCompletionSettlementStaysInProxy(t *testing.T) {
	orchestration, err := os.ReadFile(filepath.Join("..", "proxy", "orchestration.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(orchestration)
	if !strings.Contains(source, "a.Settle(ctx, ") {
		t.Fatal("chat completion success path must call proxy settlement")
	}
	if strings.Contains(source, "UpdateChannelBalance") {
		t.Fatal("chat completion success path must not sequence channel balance calls directly")
	}

	settleSource, err := os.ReadFile(filepath.Join("..", "proxy", "settlement.go"))
	if err != nil {
		t.Fatal(err)
	}
	settlement := string(settleSource)
	for _, primitive := range []string{"settlement.Tx", "SettleQuotaReservation", "UpdateChannelBalance", "InsertUsageLog"} {
		if !strings.Contains(settlement, primitive) {
			t.Fatalf("proxy settlement must use transaction primitive %q", primitive)
		}
	}
}
