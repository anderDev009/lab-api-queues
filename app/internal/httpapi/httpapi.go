// Package httpapi contains the Gin HTTP adapters for both APIs.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"

	"api-memberships/internal/reservation"

	"github.com/gin-gonic/gin"
)

// Creator is the use case consumed by the sync handler.
type Creator interface {
	CreateReservation(ctx context.Context, in reservation.CreateReservationInput) (reservation.Reservation, error)
}

func newRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return r
}

// NewSyncRouter persists the reservation within the request.
func NewSyncRouter(svc Creator) *gin.Engine {
	r := newRouter()
	r.POST("/reservations", func(c *gin.Context) {
		var in reservation.CreateReservationInput
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}
		res, err := svc.CreateReservation(c.Request.Context(), in)
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusCreated, res)
	})
	return r
}

// NewAsyncRouter validates the request and enqueues it for the worker.
func NewAsyncRouter(pub reservation.Publisher) *gin.Engine {
	r := newRouter()
	r.POST("/reservations", func(c *gin.Context) {
		var in reservation.CreateReservationInput
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}
		if err := in.Validate(); err != nil {
			writeError(c, err)
			return
		}
		job := reservation.Job{RequestID: newRequestID(), Input: in.Normalize()}
		if err := pub.Publish(c.Request.Context(), job); err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"status": "queued", "request_id": job.RequestID})
	})
	return r
}

func writeError(c *gin.Context, err error) {
	if errors.Is(err, reservation.ErrValidation) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	log.Printf("request failed: %v", err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
}

func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
