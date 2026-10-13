package reservation

import "context"

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// CreateReservation validates the input and persists a new reservation.
func (s *Service) CreateReservation(ctx context.Context, in CreateReservationInput) (Reservation, error) {
	if err := in.Validate(); err != nil {
		return Reservation{}, err
	}
	in = in.Normalize()
	r := Reservation{Email: in.Email, Name: in.Name}
	if err := s.repo.Create(ctx, &r); err != nil {
		return Reservation{}, err
	}
	return r, nil
}

// GetReservation returns the reservation with the given id or ErrNotFound.
func (s *Service) GetReservation(ctx context.Context, id uint) (Reservation, error) {
	if id == 0 {
		return Reservation{}, ErrInvalidID
	}
	return s.repo.GetByID(ctx, id)
}

// ListReservations returns one page of reservations. limit must be within
// 1..MaxListLimit and offset non-negative. The result is never nil.
func (s *Service) ListReservations(ctx context.Context, limit, offset int) ([]Reservation, error) {
	if limit < 1 || limit > MaxListLimit {
		return nil, ErrInvalidLimit
	}
	if offset < 0 {
		return nil, ErrInvalidOffset
	}
	items, err := s.repo.List(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []Reservation{}
	}
	return items, nil
}

// UpdateReservation fully replaces the email and name of an existing reservation.
func (s *Service) UpdateReservation(ctx context.Context, id uint, in CreateReservationInput) (Reservation, error) {
	if id == 0 {
		return Reservation{}, ErrInvalidID
	}
	if err := in.Validate(); err != nil {
		return Reservation{}, err
	}
	in = in.Normalize()
	r := Reservation{ID: id, Email: in.Email, Name: in.Name}
	if err := s.repo.Update(ctx, &r); err != nil {
		return Reservation{}, err
	}
	return r, nil
}

// DeleteReservation removes the reservation with the given id or returns ErrNotFound.
func (s *Service) DeleteReservation(ctx context.Context, id uint) error {
	if id == 0 {
		return ErrInvalidID
	}
	return s.repo.Delete(ctx, id)
}
