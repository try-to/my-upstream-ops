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
	mu          sync.Mutex
	schedulable bool
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
				{
					"id": 11, "name": "managed", "platform": "openai", "type": "apikey", "status": "active",
					"schedulable": state.schedulable, "concurrency": 8, "priority": 2, "rate_multiplier": 0.8,
					"proxy_id": 5, "group_ids": []int64{101}, "credentials": map[string]any{"api_key": t.Name() + "-credential"},
				},
				{
					"id": 12, "name": "unmanaged", "platform": "anthropic", "type": "oauth", "status": "inactive",
					"schedulable": false, "concurrency": 3, "priority": 1, "group_ids": []int64{},
				},
			},
			"total": 2, "page": 1, "page_size": 1000, "pages": 1,
		}})
	})
	mux.HandleFunc("/api/v1/admin/groups/all", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, map[string]any{"code": 0, "data": []map[string]any{
			{"id": 101, "name": "OpenAI 主组", "platform": "openai", "rate_multiplier": 1.2, "status": "active", "sort": 1},
		}})
	})
	mux.HandleFunc("/api/v1/admin/proxies/all", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, map[string]any{"code": 0, "data": []map[string]any{
			{"id": 5, "name": "代理 A", "protocol": "http", "host": "redacted-proxy-host", "port": 8080, "username": t.Name() + "-proxy-user", "status": "active"},
		}})
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
	srv := httptest.NewServer(mux)
	return srv, state
}

func TestGetOverviewAggregatesAndRedactsRemoteAccounts(t *testing.T) {
	db := openSyncerTestDB(t)
	svc := newTestService(t, db, &fakeChannelService{})
	server, _ := newOverviewAdminServer(t)
	defer server.Close()

	target, err := svc.CreateTarget(context.Background(), TargetInput{
		Name: "目标站", BaseURL: server.URL, AdminAPIKey: t.Name(), Enabled: true,
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	rule, err := svc.CreateSyncGroup(SyncGroupDTO{
		DisplayName: "低价池", NameTemplate: "overview-{同步分组ID}", TargetID: target.ID,
		Platform: "openai", ModelLimitsMode: "all",
	})
	if err != nil {
		t.Fatalf("create sync group: %v", err)
	}
	now := time.Now()
	if err := storage.NewUpstreamSyncManagedAccounts(db).Upsert(&storage.UpstreamSyncManagedAccount{
		SyncGroupID: rule.ID, SyncAccountID: 99, TargetAccountID: 11,
		TargetAccountName: "managed", TargetGroupIDsJSON: "[]", LastAppliedAt: &now,
	}); err != nil {
		t.Fatalf("upsert managed: %v", err)
	}

	overview, err := svc.GetOverview(context.Background())
	if err != nil {
		t.Fatalf("GetOverview: %v", err)
	}
	if overview.Summary.TotalAccounts != 2 || overview.Summary.ActiveAccounts != 1 || overview.Summary.SchedulableAccounts != 1 {
		t.Fatalf("summary = %#v", overview.Summary)
	}
	if overview.Summary.ManagedAccounts != 1 || overview.Summary.UnmanagedAccounts != 1 {
		t.Fatalf("managed summary = %#v", overview.Summary)
	}
	if len(overview.Groups) != 2 || overview.Groups[0].Name != "OpenAI 主组" || overview.Groups[0].AccountCount != 1 || overview.Groups[1].Name != "未分组" {
		t.Fatalf("groups = %#v", overview.Groups)
	}
	if len(overview.Accounts) != 2 || !overview.Accounts[0].Managed || overview.Accounts[0].ProxyName != "代理 A" || overview.Accounts[0].ManagedSyncGroupNames[0] != "低价池" {
		t.Fatalf("accounts = %#v", overview.Accounts)
	}
	raw, err := json.Marshal(overview)
	if err != nil {
		t.Fatalf("marshal overview: %v", err)
	}
	for _, forbidden := range []string{t.Name() + "-credential", t.Name() + "-proxy-user", "credentials", "redacted-proxy-host"} {
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
	if _, err := svc.CreateTarget(context.Background(), TargetInput{
		Name: "目标站", BaseURL: server.URL, AdminAPIKey: t.Name(), Enabled: true,
	}); err != nil {
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

func TestGetOverviewRequiresExactlyOneTarget(t *testing.T) {
	db := openSyncerTestDB(t)
	svc := newTestService(t, db, &fakeChannelService{})
	server, _ := newOverviewAdminServer(t)
	defer server.Close()
	if _, err := svc.GetOverview(context.Background()); !errors.Is(err, ErrOverviewTargetNotConfigured) {
		t.Fatalf("no target error = %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := svc.CreateTarget(context.Background(), TargetInput{
			Name: "target-" + strconv.Itoa(i), BaseURL: server.URL, AdminAPIKey: t.Name(), Enabled: true,
		}); err != nil {
			t.Fatalf("create target %d: %v", i, err)
		}
	}
	if _, err := svc.GetOverview(context.Background()); !errors.Is(err, ErrOverviewMultipleTargets) {
		t.Fatalf("multiple targets error = %v", err)
	}
}
