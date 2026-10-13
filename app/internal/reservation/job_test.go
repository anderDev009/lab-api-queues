package reservation_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"api-memberships/internal/reservation"
)

func TestServiceHandleJob(t *testing.T) {
	valid := reservation.CreateReservationInput{Email: "bob@example.com", Name: "Bob"}
	tests := []struct {
		name      string
		job       reservation.Job
		wantErr   error
		wantCount int    // reservations stored after the job
		wantName  string // name of reservation 1 after the job
		wantGone  bool   // reservation 1 deleted after the job
	}{
		{"create job stores reservation", reservation.Job{Type: reservation.JobCreate, Input: valid}, nil, 2, "Ana", false},
		{"missing type defaults to create", reservation.Job{Input: valid}, nil, 2, "Ana", false},
		{"invalid create is a validation error", reservation.Job{Type: reservation.JobCreate}, reservation.ErrValidation, 1, "Ana", false},
		{"update job replaces reservation", reservation.Job{Type: reservation.JobUpdate, ID: 1, Input: valid}, nil, 1, "Bob", false},
		{"update of missing id is not found", reservation.Job{Type: reservation.JobUpdate, ID: 9, Input: valid}, reservation.ErrNotFound, 1, "Ana", false},
		{"update without id is a validation error", reservation.Job{Type: reservation.JobUpdate, Input: valid}, reservation.ErrValidation, 1, "Ana", false},
		{"delete job removes reservation", reservation.Job{Type: reservation.JobDelete, ID: 1}, nil, 1, "Ana", true},
		{"delete of missing id is not found", reservation.Job{Type: reservation.JobDelete, ID: 9}, reservation.ErrNotFound, 1, "Ana", false},
		{"unknown type is rejected", reservation.Job{Type: "archive", ID: 1}, reservation.ErrUnknownJobType, 1, "Ana", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := seededRepo(1)
			err := reservation.NewService(repo).HandleJob(context.Background(), tt.job)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("HandleJob() error = %v, want %v", err, tt.wantErr)
			}
			if len(repo.saved) != tt.wantCount {
				t.Fatalf("stored %d reservations, want %d", len(repo.saved), tt.wantCount)
			}
			if repo.saved[0].Name != tt.wantName {
				t.Fatalf("reservation 1 name = %q, want %q", repo.saved[0].Name, tt.wantName)
			}
			if repo.deleted[1] != tt.wantGone {
				t.Fatalf("reservation 1 deleted = %v, want %v", repo.deleted[1], tt.wantGone)
			}
		})
	}
}

func TestJobDecodesLegacyMessage(t *testing.T) {
	// Messages queued before typed jobs existed carry no "type" field.
	var job reservation.Job
	if err := json.Unmarshal([]byte(`{"request_id":"r1","input":{"email":"a@b.co","name":"A"}}`), &job); err != nil {
		t.Fatal(err)
	}
	if job.Kind() != reservation.JobCreate {
		t.Fatalf("Kind() = %q, want %q", job.Kind(), reservation.JobCreate)
	}
}
