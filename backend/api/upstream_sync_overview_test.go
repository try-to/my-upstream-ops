package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bejix/upstream-ops/backend/syncer"
	"github.com/gin-gonic/gin"
)

func TestUpdateUpstreamSyncAccountSchedulableRejectsInvalidInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerUpstreamSync(router.Group("/api"), &Deps{UpstreamSync: &syncer.Service{}})

	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "invalid account id", path: "/api/upstream-sync/accounts/invalid/schedulable", body: `{"schedulable":true}`},
		{name: "missing schedulable", path: "/api/upstream-sync/accounts/1/schedulable", body: `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
			}
		})
	}
}
