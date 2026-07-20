package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/gin-gonic/gin"
)

type pagedAPIKeyListStub struct {
	pages   map[int]*connector.APIKeyPage
	errPage int
	queries []connector.APIKeyQuery
}

func (s *pagedAPIKeyListStub) ListAPIKeys(_ context.Context, _ uint, query connector.APIKeyQuery) (*connector.APIKeyPage, error) {
	s.queries = append(s.queries, query)
	if query.Page == s.errPage {
		return nil, errors.New("list failed")
	}
	return s.pages[query.Page], nil
}

func TestFilterRatesByActiveAPIKeysReadsAllPages(t *testing.T) {
	usedID := int64(9)
	unusedID := int64(12)
	stub := &pagedAPIKeyListStub{pages: map[int]*connector.APIKeyPage{
		1: {
			Items: []connector.APIKey{
				{ID: 1, Status: "active", GroupID: &usedID, GroupName: "Plus"},
				{ID: 2, Status: "disabled", GroupID: &unusedID, GroupName: "Unused"},
			},
			Total: 101, Page: 1, PageSize: 100, Pages: 2,
		},
		2: {
			Items: []connector.APIKey{
				{ID: 3, Status: "active", GroupName: " DEFAULT "},
				{ID: 4, Status: "expired", GroupName: "Expired"},
			},
			Total: 101, Page: 2, PageSize: 100, Pages: 2,
		},
	}}
	rates := []storage.RateSnapshot{
		{ID: 1, RemoteGroupID: &usedID, ModelName: "Plus"},
		{ID: 2, ModelName: "default"},
		{ID: 3, RemoteGroupID: &unusedID, ModelName: "Unused"},
		{ID: 4, ModelName: "Expired"},
	}

	filtered, err := filterRatesByActiveAPIKeys(context.Background(), stub, 7, rates)
	if err != nil {
		t.Fatalf("filter rates: %v", err)
	}
	if len(filtered) != 2 || filtered[0].ID != 1 || filtered[1].ID != 2 {
		t.Fatalf("filtered rates = %#v", filtered)
	}
	if len(stub.queries) != 2 {
		t.Fatalf("queries = %#v", stub.queries)
	}
	for _, query := range stub.queries {
		if query.PageSize != activeAPIKeyPageSize || query.Status != "active" {
			t.Fatalf("query = %#v", query)
		}
	}
}

func TestFilterRatesByActiveAPIKeysReturnsEmptyWithoutActiveKeys(t *testing.T) {
	stub := &pagedAPIKeyListStub{pages: map[int]*connector.APIKeyPage{
		1: {
			Items: []connector.APIKey{{ID: 1, Status: "disabled", GroupName: "default"}},
			Total: 1, Page: 1, PageSize: 100, Pages: 1,
		},
	}}

	filtered, err := filterRatesByActiveAPIKeys(context.Background(), stub, 7, []storage.RateSnapshot{{ID: 1, ModelName: "default"}})
	if err != nil {
		t.Fatalf("filter rates: %v", err)
	}
	if len(filtered) != 0 {
		t.Fatalf("filtered rates = %#v", filtered)
	}
}

func TestFilterRatesByActiveAPIKeysReturnsErrorForIncompleteList(t *testing.T) {
	stub := &pagedAPIKeyListStub{
		pages: map[int]*connector.APIKeyPage{
			1: {Total: 101, Page: 1, PageSize: 100, Pages: 2},
		},
		errPage: 2,
	}

	if _, err := filterRatesByActiveAPIKeys(context.Background(), stub, 7, []storage.RateSnapshot{{ID: 1, ModelName: "default"}}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestChannelRatesKeepsRatesWhenAPIKeyListingFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openTestDB(t)
	rates := storage.NewRates(db)
	if _, err := rates.Upsert(&storage.RateSnapshot{
		ChannelID: 7, ModelName: "default", Ratio: 1, LastSeenAt: time.Now(),
	}); err != nil {
		t.Fatalf("upsert rate: %v", err)
	}
	router := gin.New()
	registerChannels(router.Group("/api"), &Deps{
		Rates:      rates,
		ChannelSvc: &apiKeyChannelServiceStub{listErr: errors.New("upstream unavailable")},
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/channels/7/rates", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data []storage.RateSnapshot `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode rates: %v", err)
	}
	if len(payload.Data) != 1 || payload.Data[0].ModelName != "default" {
		t.Fatalf("rates = %#v", payload.Data)
	}
}
