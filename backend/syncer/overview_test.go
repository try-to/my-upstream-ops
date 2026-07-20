package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bejix/upstream-ops/backend/storage"
)

type overviewAdminState struct {
	mu               sync.Mutex
	schedulable      bool
	lastGroupRouting map[string][]int64
}

func newOverviewAdminServer(t *testing.T) (*httptest.Server, *overviewAdminState) {
	t.Helper()
	state := &overviewAdminState{schedulable: true}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/accounts", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			t.Fatal("missing x-api-key")
		}
		state.mu.Lock()
		defer state.mu.Unlock()
		respondJSON(w, map[string]any{"code": 0, "data": map[string]any{
			"items": []map[string]any{
				{"id": 11, "name": "真实上游", "platform": "openai", "type": "apikey", "status": "active", "schedulable": state.schedulable, "concurrency": 8, "priority": 2, "rate_multiplier": 0.8, "proxy_id": 5, "credentials": map[string]any{"api_key": t.Name() + "-credential"}},
				{"id": 21, "name": "授权账号 Plus 1", "platform": "openai", "type": "oauth", "status": "active", "schedulable": true, "credentials": map[string]any{"plan_type": "chatgpt_plus"}},
				{"id": 22, "name": "授权账号 Plus 2", "platform": "openai", "type": "oauth", "status": "active", "schedulable": false, "credentials": map[string]any{"plan_type": "plus"}},
				{"id": 23, "name": "授权账号 Pro", "platform": "openai", "type": "oauth", "status": "active", "schedulable": true, "credentials": map[string]any{"plan_type": "chatgpt_pro"}},
				{"id": 24, "name": "异常授权账号", "platform": "openai", "type": "oauth", "status": "active", "schedulable": true, "credentials": map[string]any{"plan_type": "abnormal_plus"}},
			},
			"total": 5, "page": 1, "page_size": 1000, "pages": 1,
		}})
	})
	mux.HandleFunc("/api/v1/admin/groups/all", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, map[string]any{"code": 0, "data": []map[string]any{
			{"id": 101, "name": "智能主组", "platform": "openai", "rate_multiplier": 1.2, "status": "active", "sort_order": 1, "model_routing_enabled": true, "model_routing": map[string]any{
				"*": []int64{-10001, 11}, "__weights_primary__": []int64{80, 120}, "__fallback__": []int64{-10002}, "__weights_fallback__": []int64{60}, "gpt-special": []int64{11},
			}},
			{"id": 102, "name": "普通组", "platform": "anthropic", "rate_multiplier": 0.9, "status": "active", "sort_order": 2, "model_routing_enabled": false, "model_routing": map[string]any{}},
		}})
	})
	mux.HandleFunc("/api/v1/admin/groups/101", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			ModelRouting        map[string][]int64 `json:"model_routing"`
			ModelRoutingEnabled bool               `json:"model_routing_enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode group routing: %v", err)
		}
		state.mu.Lock()
		state.lastGroupRouting = body.ModelRouting
		state.mu.Unlock()
		respondJSON(w, map[string]any{"code": 0, "data": map[string]any{
			"id": 101, "name": "智能主组", "model_routing_enabled": body.ModelRoutingEnabled, "model_routing": body.ModelRouting,
		}})
	})
	mux.HandleFunc("/api/v1/admin/proxies/all", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, map[string]any{"code": 0, "data": []map[string]any{{"id": 5, "name": "代理 A", "status": "active"}}})
	})
	mux.HandleFunc("/api/v1/admin/accounts/11/schedulable", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Schedulable bool `json:"schedulable"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode schedulable: %v", err)
		}
		state.mu.Lock()
		state.schedulable = body.Schedulable
		state.mu.Unlock()
		respondJSON(w, map[string]any{"code": 0, "data": map[string]any{"id": 11, "schedulable": body.Schedulable, "status": "active"}})
	})
	return httptest.NewServer(mux), state
}

func TestGetOverviewUsesSmartDispatchPoolsAndRedactsAuthorizationAccounts(t *testing.T) {
	db := openSyncerTestDB(t)
	svc := newTestService(t, db, &fakeChannelService{})
	server, _ := newOverviewAdminServer(t)
	defer server.Close()
	target, err := svc.CreateTarget(context.Background(), TargetInput{Name: "目标站", BaseURL: server.URL, AdminAPIKey: t.Name(), Enabled: true})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	now := time.Now()
	rule, err := svc.CreateSyncGroup(SyncGroupDTO{DisplayName: "智能同步", NameTemplate: "overview-{同步分组ID}", TargetID: target.ID, Platform: "openai", ModelLimitsMode: "all"})
	if err != nil {
		t.Fatalf("create sync group: %v", err)
	}
	if err := storage.NewUpstreamSyncManagedAccounts(db).Upsert(&storage.UpstreamSyncManagedAccount{SyncGroupID: rule.ID, SyncAccountID: 99, TargetAccountID: 11, TargetAccountName: "真实上游", TargetGroupIDsJSON: "[]", LastAppliedAt: &now}); err != nil {
		t.Fatalf("upsert managed: %v", err)
	}

	overview, err := svc.GetOverview(context.Background())
	if err != nil {
		t.Fatalf("GetOverview: %v", err)
	}
	if overview.Summary.TotalGroups != 1 || overview.Summary.SmartDispatchGroups != 1 || overview.Summary.RealUpstreamAccounts != 1 || overview.Summary.VirtualPools != 2 || overview.Summary.VirtualPoolMembers != 2 {
		t.Fatalf("summary = %#v", overview.Summary)
	}
	if len(overview.Groups) != 1 || len(overview.Groups[0].PrimaryPool) != 2 || overview.Groups[0].PrimaryPool[0].Name != "OpenAI OAuth Plus" || overview.Groups[0].PrimaryPool[0].MemberCount != 1 || overview.Groups[0].PrimaryPool[1].Name != "真实上游" || overview.Groups[0].PrimaryPool[1].Managed != true {
		t.Fatalf("groups = %#v", overview.Groups)
	}
	if len(overview.Groups[0].FallbackPool) != 1 || overview.Groups[0].FallbackPool[0].Name != "OpenAI OAuth Pro" || overview.Groups[0].FallbackPool[0].MemberCount != 1 {
		t.Fatalf("fallback pool = %#v", overview.Groups[0].FallbackPool)
	}
	raw, err := json.Marshal(overview)
	if err != nil {
		t.Fatalf("marshal overview: %v", err)
	}
	for _, forbidden := range []string{"授权账号 Plus", "授权账号 Pro", t.Name() + "-credential", "credentials"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("overview leaked %q: %s", forbidden, raw)
		}
	}
}

func TestSetOverviewAccountSchedulableOnlyReturnsNewState(t *testing.T) {
	db := openSyncerTestDB(t)
	svc := newTestService(t, db, &fakeChannelService{})
	server, state := newOverviewAdminServer(t)
	defer server.Close()
	if _, err := svc.CreateTarget(context.Background(), TargetInput{Name: "目标站", BaseURL: server.URL, AdminAPIKey: t.Name(), Enabled: true}); err != nil {
		t.Fatalf("create target: %v", err)
	}
	result, err := svc.SetOverviewAccountSchedulable(context.Background(), 11, false)
	if err != nil {
		t.Fatalf("SetOverviewAccountSchedulable: %v", err)
	}
	if result.AccountID != 11 || result.Schedulable {
		t.Fatalf("result = %#v", result)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.schedulable {
		t.Fatal("remote schedulable was not updated")
	}
}

func TestUpdateOverviewGroupSmartRoutingPreservesModelSpecificRoutes(t *testing.T) {
	db := openSyncerTestDB(t)
	svc := newTestService(t, db, &fakeChannelService{})
	server, state := newOverviewAdminServer(t)
	defer server.Close()
	if _, err := svc.CreateTarget(context.Background(), TargetInput{Name: "目标站", BaseURL: server.URL, AdminAPIKey: t.Name(), Enabled: true}); err != nil {
		t.Fatalf("create target: %v", err)
	}
	result, err := svc.UpdateOverviewGroupSmartRouting(context.Background(), 101, SmartRoutingUpdateInput{
		PrimaryPool:  []SmartRoutingEntryInput{{ID: -10001, Weight: 95}, {ID: 11, Weight: 135}},
		FallbackPool: []SmartRoutingEntryInput{{ID: -10002, Weight: 75}},
	})
	if err != nil {
		t.Fatalf("UpdateOverviewGroupSmartRouting: %v", err)
	}
	if result.GroupID != 101 || len(result.PrimaryPool) != 2 || result.PrimaryPool[1].Weight != 135 {
		t.Fatalf("result = %#v", result)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if got := state.lastGroupRouting["gpt-special"]; len(got) != 1 || got[0] != 11 {
		t.Fatalf("model-specific route was not preserved: %#v", state.lastGroupRouting)
	}
	if got := state.lastGroupRouting[smartDispatchPrimaryWeightsKey]; len(got) != 2 || got[0] != 95 || got[1] != 135 {
		t.Fatalf("primary weights = %#v", got)
	}
	if got := state.lastGroupRouting[smartDispatchFallbackWeightsKey]; len(got) != 1 || got[0] != 75 {
		t.Fatalf("fallback weights = %#v", got)
	}
}

func TestUpdateOverviewGroupSmartRoutingRejectsInvalidEntries(t *testing.T) {
	svc := &Service{}
	tests := []SmartRoutingUpdateInput{
		{PrimaryPool: []SmartRoutingEntryInput{{ID: 11, Weight: 0}}},
		{PrimaryPool: []SmartRoutingEntryInput{{ID: 11, Weight: 1000}}},
		{PrimaryPool: []SmartRoutingEntryInput{{ID: 0, Weight: 100}}},
		{PrimaryPool: []SmartRoutingEntryInput{{ID: -99999, Weight: 100}}},
	}
	for _, input := range tests {
		if _, err := svc.UpdateOverviewGroupSmartRouting(context.Background(), 101, input); !errors.Is(err, ErrInvalidOverviewRouting) {
			t.Fatalf("input %#v error = %v", input, err)
		}
	}
}

func TestValidateSmartRoutingPoolIDsRejectsPoolChanges(t *testing.T) {
	tests := [][]SmartRoutingEntryInput{
		{{ID: 11, Weight: 100}},
		{{ID: -10001, Weight: 100}, {ID: 12, Weight: 100}},
		{{ID: 11, Weight: 100}, {ID: -10001, Weight: 100}},
	}
	for _, entries := range tests {
		if err := validateSmartRoutingPoolIDs(entries, []int64{-10001, 11}); err == nil {
			t.Fatalf("entries %#v should be rejected", entries)
		}
	}
}

func TestVirtualOAuthPoolPlanClassification(t *testing.T) {
	tests := []struct {
		plan string
		want int64
	}{
		{plan: "chatgpt_plus", want: virtualOAuthPlusID},
		{plan: "pro", want: virtualOAuthProID},
		{plan: "education", want: virtualOAuthK12ID},
		{plan: "business_workspace", want: virtualOAuthTeamID},
		{plan: "chatgptfree", want: virtualOAuthFreeID},
		{plan: "abnormal_plus", want: 0},
	}
	for _, tt := range tests {
		got, ok := classifyVirtualOAuthPool(tt.plan)
		if (tt.want != 0) != ok || got != tt.want {
			t.Fatalf("classify %q = (%d, %v), want %d", tt.plan, got, ok, tt.want)
		}
	}
}

func TestGetOverviewRequiresExactlyOneTarget(t *testing.T) {
	db := openSyncerTestDB(t)
	svc := newTestService(t, db, &fakeChannelService{})
	server, _ := newOverviewAdminServer(t)
	defer server.Close()
	if _, err := svc.GetOverview(context.Background()); !errors.Is(err, ErrOverviewTargetNotConfigured) {
		t.Fatalf("no target error = %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := svc.CreateTarget(context.Background(), TargetInput{Name: "target-" + strconv.Itoa(i), BaseURL: server.URL, AdminAPIKey: t.Name(), Enabled: true}); err != nil {
			t.Fatalf("create target %d: %v", i, err)
		}
	}
	if _, err := svc.GetOverview(context.Background()); !errors.Is(err, ErrOverviewMultipleTargets) {
		t.Fatalf("multiple targets error = %v", err)
	}
}
