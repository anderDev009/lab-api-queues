package reservation

import (
	"context"
	"errors"
	"fmt"
)

// JobType selects the write operation a queued job performs.
type JobType string

const (
	JobCreate JobType = "create"
	JobUpdate JobType = "update"
	JobDelete JobType = "delete"
)

// ErrUnknownJobType is returned for jobs whose type the worker cannot handle.
var ErrUnknownJobType = errors.New("unknown job type")

// Job is the message exchanged between api-async and the worker. ID is set
// for update and delete; Input is set for create and update.
type Job struct {
	RequestID string                 `json:"request_id"`
	Type      JobType                `json:"type,omitempty"`
	ID        uint                   `json:"id,omitempty"`
	Input     CreateReservationInput `json:"input,omitzero"`
}

// Kind returns the job type, treating a missing type as create so messages
// queued before typed jobs existed are still processed.
func (j Job) Kind() JobType {
	if j.Type == "" {
		return JobCreate
	}
	return j.Type
}

// HandleJob dispatches a queued job to the matching use case.
func (s *Service) HandleJob(ctx context.Context, job Job) error {
	switch job.Kind() {
	case JobCreate:
		_, err := s.CreateReservation(ctx, job.Input)
		return err
	case JobUpdate:
		_, err := s.UpdateReservation(ctx, job.ID, job.Input)
		return err
	case JobDelete:
		return s.DeleteReservation(ctx, job.ID)
	default:
		return fmt.Errorf("%w: %q", ErrUnknownJobType, job.Type)
	}
}
