package rabbitmq

import (
	"errors"
	"fmt"
	"testing"

	"api-memberships/internal/reservation"
)

func TestShouldRequeue(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"validation error is dropped", reservation.ErrInvalidEmail, false},
		{"not found is dropped", reservation.ErrNotFound, false},
		{"unknown job type is dropped", fmt.Errorf("%w: %q", reservation.ErrUnknownJobType, "archive"), false},
		{"transient error is requeued", errors.New("connection refused"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRequeue(tt.err); got != tt.want {
				t.Fatalf("shouldRequeue(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
