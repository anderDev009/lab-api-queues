// Package reservation holds the reservation domain: entity, input validation,
// ports and the CreateReservation use case shared by every adapter.
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

var (
	ErrEmailRequired = fmt.Errorf("%w: email is required", ErrValidation)
	ErrInvalidEmail  = fmt.Errorf("%w: email is invalid", ErrValidation)
	ErrNameRequired  = fmt.Errorf("%w: name is required", ErrValidation)
)

type Reservation struct {
	ID        uint      `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

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

// Repository is the persistence port.
type Repository interface {
	Create(ctx context.Context, r *Reservation) error
}

// Publisher is the port used by the async API to enqueue reservation requests.
type Publisher interface {
	Publish(ctx context.Context, job Job) error
}

// Job is the message exchanged between api-async and the worker.
type Job struct {
	RequestID string                 `json:"request_id"`
	Input     CreateReservationInput `json:"input"`
}
