package reservation_test

import (
	"context"
	"errors"
	"testing"

	"api-memberships/internal/reservation"
)

type fakeRepo struct {
	saved     []reservation.Reservation
	deleted   map[uint]bool
	err       error
	gotLimit  int
	gotOffset int
}

func (f *fakeRepo) Create(_ context.Context, r *reservation.Reservation) error {
	if f.err != nil {
		return f.err
	}
	r.ID = uint(len(f.saved) + 1)
	f.saved = append(f.saved, *r)
	return nil
}

func (f *fakeRepo) find(id uint) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	if id == 0 || int(id) > len(f.saved) || f.deleted[id] {
		return 0, reservation.ErrNotFound
	}
	return int(id) - 1, nil
}

func (f *fakeRepo) GetByID(_ context.Context, id uint) (reservation.Reservation, error) {
	i, err := f.find(id)
	if err != nil {
		return reservation.Reservation{}, err
	}
	return f.saved[i], nil
}

func (f *fakeRepo) List(_ context.Context, limit, offset int) ([]reservation.Reservation, error) {
	f.gotLimit, f.gotOffset = limit, offset
	if f.err != nil {
		return nil, f.err
	}
	var out []reservation.Reservation
	for _, r := range f.saved {
		if !f.deleted[r.ID] {
			out = append(out, r)
		}
	}
	if offset >= len(out) {
		return nil, nil
	}
	return out[offset:min(offset+limit, len(out))], nil
}

func (f *fakeRepo) Update(_ context.Context, r *reservation.Reservation) error {
	i, err := f.find(r.ID)
	if err != nil {
		return err
	}
	f.saved[i].Email, f.saved[i].Name = r.Email, r.Name
	*r = f.saved[i]
	return nil
}

func (f *fakeRepo) Truncate(_ context.Context) error {
	if f.err != nil {
		return f.err
	}
	f.saved, f.deleted = nil, nil
	return nil
}

func (f *fakeRepo) Delete(_ context.Context, id uint) error {
	if _, err := f.find(id); err != nil {
		return err
	}
	if f.deleted == nil {
		f.deleted = map[uint]bool{}
	}
	f.deleted[id] = true
	return nil
}

// seededRepo returns a repo holding reservations 1..n.
func seededRepo(n int) *fakeRepo {
	repo := &fakeRepo{}
	for i := range n {
		repo.saved = append(repo.saved, reservation.Reservation{ID: uint(i + 1), Email: "ana@example.com", Name: "Ana"})
	}
	return repo
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

func TestServiceGetReservation(t *testing.T) {
	dbErr := errors.New("db down")
	tests := []struct {
		name    string
		id      uint
		repoErr error
		wantErr error
	}{
		{"returns existing reservation", 2, nil, nil},
		{"rejects zero id", 0, nil, reservation.ErrInvalidID},
		{"propagates not found", 9, nil, reservation.ErrNotFound},
		{"propagates repository error", 1, dbErr, dbErr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := seededRepo(3)
			repo.err = tt.repoErr
			got, err := reservation.NewService(repo).GetReservation(context.Background(), tt.id)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("GetReservation() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && got.ID != tt.id {
				t.Fatalf("GetReservation() id = %d, want %d", got.ID, tt.id)
			}
		})
	}
}

func TestServiceListReservations(t *testing.T) {
	tests := []struct {
		name      string
		limit     int
		offset    int
		wantErr   error
		wantItems int
	}{
		{"first page", 2, 0, nil, 2},
		{"last partial page", 2, 2, nil, 1},
		{"offset past end returns empty list", 20, 10, nil, 0},
		{"max limit accepted", reservation.MaxListLimit, 0, nil, 3},
		{"zero limit rejected", 0, 0, reservation.ErrInvalidLimit, 0},
		{"limit above max rejected", reservation.MaxListLimit + 1, 0, reservation.ErrInvalidLimit, 0},
		{"negative offset rejected", 10, -1, reservation.ErrInvalidOffset, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := seededRepo(3)
			got, err := reservation.NewService(repo).ListReservations(context.Background(), tt.limit, tt.offset)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ListReservations() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if !errors.Is(err, reservation.ErrValidation) {
					t.Fatalf("error %v does not wrap ErrValidation", err)
				}
				return
			}
			if got == nil {
				t.Fatal("ListReservations() returned nil slice, want empty non-nil")
			}
			if len(got) != tt.wantItems {
				t.Fatalf("ListReservations() returned %d items, want %d", len(got), tt.wantItems)
			}
			if repo.gotLimit != tt.limit || repo.gotOffset != tt.offset {
				t.Fatalf("repo got limit=%d offset=%d", repo.gotLimit, repo.gotOffset)
			}
		})
	}
}

func TestServiceUpdateReservation(t *testing.T) {
	valid := reservation.CreateReservationInput{Email: " bob@example.com ", Name: " Bob "}
	tests := []struct {
		name    string
		id      uint
		input   reservation.CreateReservationInput
		wantErr error
	}{
		{"replaces email and name", 1, valid, nil},
		{"rejects zero id", 0, valid, reservation.ErrInvalidID},
		{"rejects invalid input", 1, reservation.CreateReservationInput{Email: "bad", Name: "Bob"}, reservation.ErrInvalidEmail},
		{"rejects missing name", 1, reservation.CreateReservationInput{Email: "bob@example.com"}, reservation.ErrNameRequired},
		{"propagates not found", 9, valid, reservation.ErrNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := seededRepo(1)
			got, err := reservation.NewService(repo).UpdateReservation(context.Background(), tt.id, tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("UpdateReservation() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if repo.saved[0].Email != "ana@example.com" {
					t.Fatalf("reservation changed on error: %+v", repo.saved[0])
				}
				return
			}
			if got.ID != 1 || got.Email != "bob@example.com" || got.Name != "Bob" {
				t.Fatalf("unexpected updated reservation: %+v", got)
			}
			if repo.saved[0].Email != "bob@example.com" {
				t.Fatalf("repository not updated: %+v", repo.saved[0])
			}
		})
	}
}

func TestServiceDeleteReservation(t *testing.T) {
	tests := []struct {
		name    string
		id      uint
		wantErr error
	}{
		{"deletes existing reservation", 1, nil},
		{"rejects zero id", 0, reservation.ErrInvalidID},
		{"propagates not found", 9, reservation.ErrNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := seededRepo(1)
			svc := reservation.NewService(repo)
			err := svc.DeleteReservation(context.Background(), tt.id)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("DeleteReservation() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil {
				if _, err := svc.GetReservation(context.Background(), tt.id); !errors.Is(err, reservation.ErrNotFound) {
					t.Fatalf("GetReservation() after delete error = %v, want ErrNotFound", err)
				}
			}
		})
	}
}

func TestServiceTruncateReservations(t *testing.T) {
	repo := &fakeRepo{}
	svc := reservation.NewService(repo)
	if _, err := svc.CreateReservation(context.Background(), reservation.CreateReservationInput{Email: "ana@example.com", Name: "Ana"}); err != nil {
		t.Fatal(err)
	}

	if err := svc.TruncateReservations(context.Background()); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if len(repo.saved) != 0 {
		t.Fatalf("saved = %d, want 0", len(repo.saved))
	}

	repo.err = errors.New("db down")
	if err := svc.TruncateReservations(context.Background()); !errors.Is(err, repo.err) {
		t.Fatalf("err = %v, want %v", err, repo.err)
	}
}
