package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"api-memberships/internal/reservation"

	"github.com/gin-gonic/gin"
)

type nopCreator struct{}

func (nopCreator) CreateReservation(context.Context, reservation.CreateReservationInput) (reservation.Reservation, error) {
	return reservation.Reservation{}, nil
}

type nopPublisher struct{}

func (nopPublisher) Publish(context.Context, reservation.Job) error { return nil }

func TestDocsRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		router   *gin.Engine
		path     string
		contains string
	}{
		{"sync spec", NewSyncRouter(nopCreator{}), "/openapi.yaml", "Reservations Sync API"},
		{"sync ui", NewSyncRouter(nopCreator{}), "/swagger", "swagger-ui"},
		{"async spec", NewAsyncRouter(nopPublisher{}), "/openapi.yaml", "Reservations Async API"},
		{"async ui", NewAsyncRouter(nopPublisher{}), "/swagger", "swagger-ui"},
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
