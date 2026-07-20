package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/gin-gonic/gin"
)

func TestChannelRateGroupPolicySaveListAndDisable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openTestDB(t)
	channels := storage.NewChannels(db)
	rates := storage.NewRates(db)
	policies := storage.NewRateGroupPolicies(db)
	channel := &storage.Channel{Name: "source", Type: storage.ChannelTypeNewAPI, SiteURL: "https://source.example", MonitorEnabled: true}
	if err := channels.Create(channel); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	remoteID := int64(9)
	if _, err := rates.Upsert(&storage.RateSnapshot{
		ChannelID: channel.ID, RemoteGroupID: &remoteID, ModelName: "plus", Ratio: 0.6, LastSeenAt: time.Now(),
	}); err != nil {
		t.Fatalf("upsert rate: %v", err)
	}
	router := gin.New()
	registerChannels(router.Group("/api"), &Deps{Channels: channels, Rates: rates, RatePolicies: policies})

	request := httptest.NewRequest(http.MethodPut, "/api/channels/"+formatUint(channel.ID)+"/rate-groups/policy", strings.NewReader(`{"remote_group_id":9,"group_name":"plus","max_ratio":1,"calculation_ratio":2}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("save status = %d, body = %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/channels/"+formatUint(channel.ID)+"/rates", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data []channelRateDTO `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode rates: %v", err)
	}
	if len(payload.Data) != 1 || payload.Data[0].MaxRatio == nil || *payload.Data[0].MaxRatio != 1 || payload.Data[0].CalculatedRatio == nil || *payload.Data[0].CalculatedRatio != 1.2 || payload.Data[0].AutoSchedulableState != "disabled" {
		t.Fatalf("rate dto = %#v", payload.Data)
	}

	request = httptest.NewRequest(http.MethodPut, "/api/channels/"+formatUint(channel.ID)+"/rate-groups/policy", strings.NewReader(`{"remote_group_id":9,"group_name":"plus","max_ratio":null,"calculation_ratio":1}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("disable status = %d, body = %s", response.Code, response.Body.String())
	}
	got, err := policies.Find(channel.ID, storage.RateGroupKey(&remoteID, "plus"))
	if err != nil || got != nil {
		t.Fatalf("disabled policy = %#v, %v", got, err)
	}
}

func TestChannelRateGroupPolicyRejectsInvalidInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openTestDB(t)
	channels := storage.NewChannels(db)
	rates := storage.NewRates(db)
	policies := storage.NewRateGroupPolicies(db)
	channel := &storage.Channel{Name: "source", Type: storage.ChannelTypeNewAPI, SiteURL: "https://source.example", MonitorEnabled: true}
	if err := channels.Create(channel); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	router := gin.New()
	registerChannels(router.Group("/api"), &Deps{Channels: channels, Rates: rates, RatePolicies: policies})

	for _, body := range []string{
		`{"group_name":"missing","max_ratio":1,"calculation_ratio":1}`,
		`{"group_name":"missing","max_ratio":-1,"calculation_ratio":1}`,
		`{"group_name":"missing","max_ratio":1,"calculation_ratio":0}`,
	} {
		request := httptest.NewRequest(http.MethodPut, "/api/channels/"+formatUint(channel.ID)+"/rate-groups/policy", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d, response = %s", body, response.Code, response.Body.String())
		}
	}
}

func formatUint(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
