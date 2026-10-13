package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDocsRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		router   *gin.Engine
		path     string
		contains string
	}{
		{"sync spec", NewSyncRouter(&fakeService{}), "/openapi.yaml", "Reservations Sync API"},
		{"sync ui", NewSyncRouter(&fakeService{}), "/swagger", "swagger-ui"},
		{"async spec", NewAsyncRouter(&fakeService{}, &fakePublisher{}), "/openapi.yaml", "Reservations Async API"},
		{"async ui", NewAsyncRouter(&fakeService{}, &fakePublisher{}), "/swagger", "swagger-ui"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tt.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if !strings.Contains(rec.Body.String(), tt.contains) {
				t.Fatalf("body does not contain %q", tt.contains)
			}
		})
	}
}
