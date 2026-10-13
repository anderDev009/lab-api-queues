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
