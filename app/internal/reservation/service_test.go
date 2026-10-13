package reservation_test

import (
	"context"
	"errors"
	"testing"

	"api-memberships/internal/reservation"
)

type fakeRepo struct {
	saved []reservation.Reservation
	err   error
}

func (f *fakeRepo) Create(_ context.Context, r *reservation.Reservation) error {
	if f.err != nil {
		return f.err
	}
	r.ID = uint(len(f.saved) + 1)
	f.saved = append(f.saved, *r)
	return nil
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		input   reservation.CreateReservationInput
		wantErr error
	}{
		{"valid input", reservation.CreateReservationInput{Email: "ana@example.com", Name: "Ana"}, nil},
		{"trims whitespace", reservation.CreateReservationInput{Email: "  ana@example.com ", Name: " Ana "}, nil},
		{"missing email", reservation.CreateReservationInput{Name: "Ana"}, reservation.ErrEmailRequired},
		{"blank email", reservation.CreateReservationInput{Email: "   ", Name: "Ana"}, reservation.ErrEmailRequired},
		{"email without at", reservation.CreateReservationInput{Email: "ana.example.com", Name: "Ana"}, reservation.ErrInvalidEmail},
		{"email without domain", reservation.CreateReservationInput{Email: "ana@", Name: "Ana"}, reservation.ErrInvalidEmail},
		{"email with display name", reservation.CreateReservationInput{Email: "Ana <ana@example.com>", Name: "Ana"}, reservation.ErrInvalidEmail},
		{"missing name", reservation.CreateReservationInput{Email: "ana@example.com"}, reservation.ErrNameRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil && !errors.Is(err, reservation.ErrValidation) {
				t.Fatalf("Validate() error = %v, want it to wrap ErrValidation", err)
			}
		})
	}
}

func TestServiceCreateReservation(t *testing.T) {
	dbErr := errors.New("db down")
	tests := []struct {
		name      string
		input     reservation.CreateReservationInput
		repoErr   error
		wantErr   error
		wantSaved int
	}{
		{"persists valid reservation", reservation.CreateReservationInput{Email: " ana@example.com", Name: "Ana "}, nil, nil, 1},
		{"rejects invalid email without saving", reservation.CreateReservationInput{Email: "bad", Name: "Ana"}, nil, reservation.ErrInvalidEmail, 0},
		{"propagates repository error", reservation.CreateReservationInput{Email: "ana@example.com", Name: "Ana"}, dbErr, dbErr, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{err: tt.repoErr}
			svc := reservation.NewService(repo)

			got, err := svc.CreateReservation(context.Background(), tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CreateReservation() error = %v, want %v", err, tt.wantErr)
			}
			if len(repo.saved) != tt.wantSaved {
				t.Fatalf("saved %d reservations, want %d", len(repo.saved), tt.wantSaved)
			}
			if tt.wantErr == nil {
				if got.ID == 0 || got.Email != "ana@example.com" || got.Name != "Ana" {
					t.Fatalf("unexpected reservation: %+v", got)
				}
			}
		})
	}
}
