// Package reservation holds the reservation domain: entity, input validation,
// ports and the CRUD use cases shared by every adapter.
package reservation

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// ErrValidation is wrapped by every input validation error so adapters can
// distinguish client errors from infrastructure failures.
var ErrValidation = errors.New("validation error")

// ErrNotFound is returned when the requested reservation does not exist.
var ErrNotFound = errors.New("reservation not found")

// Pagination bounds for listing reservations.
const (
	DefaultListLimit = 20
	MaxListLimit     = 100
)

var (
	ErrEmailRequired = fmt.Errorf("%w: email is required", ErrValidation)
	ErrInvalidEmail  = fmt.Errorf("%w: email is invalid", ErrValidation)
	ErrNameRequired  = fmt.Errorf("%w: name is required", ErrValidation)
	ErrInvalidID     = fmt.Errorf("%w: id must be a positive integer", ErrValidation)
	ErrInvalidLimit  = fmt.Errorf("%w: limit must be between 1 and %d", ErrValidation, MaxListLimit)
	ErrInvalidOffset = fmt.Errorf("%w: offset must be a non-negative integer", ErrValidation)
)

type Reservation struct {
	ID        uint      `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateReservationInput carries the client-editable fields. It is used both
// to create a reservation and to fully replace one on update.
type CreateReservationInput struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

// Normalize returns a copy with surrounding whitespace removed.
func (in CreateReservationInput) Normalize() CreateReservationInput {
	return CreateReservationInput{
		Email: strings.TrimSpace(in.Email),
		Name:  strings.TrimSpace(in.Name),
	}
}

func (in CreateReservationInput) Validate() error {
	in = in.Normalize()
	if in.Email == "" {
		return ErrEmailRequired
	}
	addr, err := mail.ParseAddress(in.Email)
	if err != nil || addr.Address != in.Email || !strings.Contains(addr.Address[strings.LastIndex(addr.Address, "@")+1:], ".") {
		return ErrInvalidEmail
	}
	if in.Name == "" {
		return ErrNameRequired
	}
	return nil
}

// Repository is the persistence port. GetByID, Update and Delete return
// ErrNotFound when the reservation does not exist.
type Repository interface {
	Create(ctx context.Context, r *Reservation) error
	GetByID(ctx context.Context, id uint) (Reservation, error)
	List(ctx context.Context, limit, offset int) ([]Reservation, error)
	// Update replaces Email and Name of the reservation identified by r.ID
	// and fills r with the stored state.
	Update(ctx context.Context, r *Reservation) error
	Delete(ctx context.Context, id uint) error
	// Truncate removes every reservation and resets the id sequence.
	Truncate(ctx context.Context) error
}

// Publisher is the port used by the async API to enqueue reservation requests.
type Publisher interface {
	Publish(ctx context.Context, job Job) error
}
