// Package httpapi contains the Gin HTTP adapters for both APIs.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strconv"

	"api-memberships/internal/reservation"

	"github.com/gin-gonic/gin"
)

// Reader is the read side used by both APIs; reads are always synchronous.
type Reader interface {
	GetReservation(ctx context.Context, id uint) (reservation.Reservation, error)
	ListReservations(ctx context.Context, limit, offset int) ([]reservation.Reservation, error)
}

// SyncService is the full CRUD use case set consumed by the sync API.
type SyncService interface {
	Reader
	CreateReservation(ctx context.Context, in reservation.CreateReservationInput) (reservation.Reservation, error)
	UpdateReservation(ctx context.Context, id uint, in reservation.CreateReservationInput) (reservation.Reservation, error)
	DeleteReservation(ctx context.Context, id uint) error
}

func newRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return r
}

// NewSyncRouter performs every operation within the request.
func NewSyncRouter(svc SyncService) *gin.Engine {
	r := newRouter()
	registerDocs(r, syncSpec)
	registerReadRoutes(r, svc)
	r.POST("/reservations", func(c *gin.Context) {
		in, ok := bindInput(c)
		if !ok {
			return
		}
		res, err := svc.CreateReservation(c.Request.Context(), in)
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusCreated, res)
	})
	r.PUT("/reservations/:id", func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		in, ok := bindInput(c)
		if !ok {
			return
		}
		res, err := svc.UpdateReservation(c.Request.Context(), id, in)
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusOK, res)
	})
	r.DELETE("/reservations/:id", func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		if err := svc.DeleteReservation(c.Request.Context(), id); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
	return r
}

// NewAsyncRouter reads synchronously from the database and validates writes
// before enqueuing them for the worker.
func NewAsyncRouter(reader Reader, pub reservation.Publisher) *gin.Engine {
	r := newRouter()
	registerDocs(r, asyncSpec)
	registerReadRoutes(r, reader)
	r.POST("/reservations", func(c *gin.Context) {
		in, ok := bindInput(c)
		if !ok {
			return
		}
		if err := in.Validate(); err != nil {
			writeError(c, err)
			return
		}
		enqueue(c, pub, reservation.Job{Type: reservation.JobCreate, Input: in.Normalize()})
	})
	r.PUT("/reservations/:id", func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		in, ok := bindInput(c)
		if !ok {
			return
		}
		if err := in.Validate(); err != nil {
			writeError(c, err)
			return
		}
		enqueue(c, pub, reservation.Job{Type: reservation.JobUpdate, ID: id, Input: in.Normalize()})
	})
	r.DELETE("/reservations/:id", func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		enqueue(c, pub, reservation.Job{Type: reservation.JobDelete, ID: id})
	})
	return r
}

func registerReadRoutes(r *gin.Engine, reader Reader) {
	r.GET("/reservations", func(c *gin.Context) {
		limit, err := queryInt(c, "limit", reservation.DefaultListLimit, reservation.ErrInvalidLimit)
		if err != nil {
			writeError(c, err)
			return
		}
		offset, err := queryInt(c, "offset", 0, reservation.ErrInvalidOffset)
		if err != nil {
			writeError(c, err)
			return
		}
		items, err := reader.ListReservations(c.Request.Context(), limit, offset)
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "limit": limit, "offset": offset})
	})
	r.GET("/reservations/:id", func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		res, err := reader.GetReservation(c.Request.Context(), id)
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusOK, res)
	})
}

func enqueue(c *gin.Context, pub reservation.Publisher, job reservation.Job) {
	job.RequestID = newRequestID()
	if err := pub.Publish(c.Request.Context(), job); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "queued", "request_id": job.RequestID})
}

func bindInput(c *gin.Context) (reservation.CreateReservationInput, bool) {
	var in reservation.CreateReservationInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return in, false
	}
	return in, true
}

// parseID reads the :id path param and writes a 400 unless it is a positive integer.
func parseID(c *gin.Context) (uint, bool) {
	n, err := strconv.ParseUint(c.Param("id"), 10, 0)
	if err != nil || n == 0 {
		writeError(c, reservation.ErrInvalidID)
		return 0, false
	}
	return uint(n), true
}

// queryInt returns def when the param is absent and invalid when it is not an integer.
// Range checks belong to the use case.
func queryInt(c *gin.Context, key string, def int, invalid error) (int, error) {
	raw, ok := c.GetQuery(key)
	if !ok {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, invalid
	}
	return n, nil
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, reservation.ErrValidation):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, reservation.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		log.Printf("request failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
