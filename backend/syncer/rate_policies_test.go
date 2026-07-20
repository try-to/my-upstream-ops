package syncer

import (
	"testing"

	"github.com/bejix/upstream-ops/backend/storage"
)

func TestRatePolicyMatchingFallsBackToGroupName(t *testing.T) {
	policies := []storage.RateGroupPolicy{{ChannelID: 3, GroupName: "Default", MaxRatio: 1, CalculationRatio: 1}}
	account := &storage.UpstreamSyncAccount{SourceChannelID: 3, SourceGroupName: " default "}
	policy := matchRatePolicy(policies, account)
	if policy == nil || policy.GroupName != "Default" {
		t.Fatalf("matched policy = %#v", policy)
	}
	snapshot := matchRateSnapshot([]storage.RateSnapshot{{ChannelID: 3, ModelName: "DEFAULT", Ratio: 0.5}}, policy)
	if snapshot == nil || snapshot.Ratio != 0.5 {
		t.Fatalf("matched snapshot = %#v", snapshot)
	}
}

func TestValidRatePolicyValues(t *testing.T) {
	if !validRatePolicyValues(0.5, 1, 1.25) {
		t.Fatal("valid decimal values were rejected")
	}
	for _, values := range [][3]float64{{-1, 1, 1}, {1, -1, 1}, {1, 1, 0}} {
		if validRatePolicyValues(values[0], values[1], values[2]) {
			t.Fatalf("invalid values were accepted: %#v", values)
		}
	}
}
