package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"api-memberships/internal/reservation"

	"github.com/gin-gonic/gin"
)

// fakeService knows only reservation 1 and records list arguments.
type fakeService struct {
	listErr              error
	listCalled           bool
	gotLimit, gotOffset  int
	updatedID, deletedID uint
	truncated            bool
	truncateErr          error
}

var existing = reservation.Reservation{ID: 1, Email: "ana@example.com", Name: "Ana"}

func (f *fakeService) CreateReservation(_ context.Context, in reservation.CreateReservationInput) (reservation.Reservation, error) {
	if err := in.Validate(); err != nil {
		return reservation.Reservation{}, err
	}
	return existing, nil
}

func (f *fakeService) GetReservation(_ context.Context, id uint) (reservation.Reservation, error) {
	if id != existing.ID {
		return reservation.Reservation{}, reservation.ErrNotFound
	}
	return existing, nil
}

func (f *fakeService) ListReservations(_ context.Context, limit, offset int) ([]reservation.Reservation, error) {
	f.listCalled, f.gotLimit, f.gotOffset = true, limit, offset
	if f.listErr != nil {
		return nil, f.listErr
	}
	return []reservation.Reservation{existing}, nil
}

func (f *fakeService) UpdateReservation(_ context.Context, id uint, in reservation.CreateReservationInput) (reservation.Reservation, error) {
	if err := in.Validate(); err != nil {
		return reservation.Reservation{}, err
	}
	if id != existing.ID {
		return reservation.Reservation{}, reservation.ErrNotFound
	}
	f.updatedID = id
	in = in.Normalize()
	return reservation.Reservation{ID: id, Email: in.Email, Name: in.Name}, nil
}

func (f *fakeService) DeleteReservation(_ context.Context, id uint) error {
	if id != existing.ID {
		return reservation.ErrNotFound
	}
	f.deletedID = id
	return nil
}

func (f *fakeService) TruncateReservations(_ context.Context) error {
	f.truncated = true
	return f.truncateErr
}

type fakePublisher struct {
	jobs []reservation.Job
	err  error
}

func (f *fakePublisher) Publish(_ context.Context, job reservation.Job) error {
	if f.err != nil {
		return f.err
	}
	f.jobs = append(f.jobs, job)
	return nil
}

const validBody = `{"email":"bob@example.com","name":"Bob"}`

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// routers builds both APIs over the same fakes; read routes behave identically.
func routers(svc *fakeService) map[string]http.Handler {
	return map[string]http.Handler{
		"sync":  NewSyncRouter(svc),
		"async": NewAsyncRouter(svc, &fakePublisher{}),
	}
}

func TestReadRoutes(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		listErr    error
		wantStatus int
		wantLimit  int
		wantOffset int
		wantNoList bool // the service must not be called
	}{
		{"list uses default paging", "/reservations", nil, http.StatusOK, reservation.DefaultListLimit, 0, false},
		{"list passes paging params", "/reservations?limit=5&offset=10", nil, http.StatusOK, 5, 10, false},
		{"list rejects non-numeric limit", "/reservations?limit=abc", nil, http.StatusBadRequest, 0, 0, true},
		{"list rejects non-numeric offset", "/reservations?offset=x", nil, http.StatusBadRequest, 0, 0, true},
		{"list maps service validation to 400", "/reservations?limit=500", reservation.ErrInvalidLimit, http.StatusBadRequest, 500, 0, false},
		{"list maps unexpected error to 500", "/reservations", errors.New("db down"), http.StatusInternalServerError, reservation.DefaultListLimit, 0, false},
		{"get existing", "/reservations/1", nil, http.StatusOK, 0, 0, true},
		{"get missing", "/reservations/9", nil, http.StatusNotFound, 0, 0, true},
		{"get non-numeric id", "/reservations/abc", nil, http.StatusBadRequest, 0, 0, true},
		{"get zero id", "/reservations/0", nil, http.StatusBadRequest, 0, 0, true},
		{"get negative id", "/reservations/-1", nil, http.StatusBadRequest, 0, 0, true},
	}

	for _, tt := range tests {
		for api := range routers(&fakeService{}) {
			t.Run(api+"/"+tt.name, func(t *testing.T) {
				svc := &fakeService{listErr: tt.listErr}
				rec := do(t, routers(svc)[api], http.MethodGet, tt.path, "")

				if rec.Code != tt.wantStatus {
					t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body)
				}
				if svc.listCalled == tt.wantNoList {
					t.Fatalf("list called = %v, want %v", svc.listCalled, !tt.wantNoList)
				}
				if svc.listCalled && (svc.gotLimit != tt.wantLimit || svc.gotOffset != tt.wantOffset) {
					t.Fatalf("service got limit=%d offset=%d, want %d/%d", svc.gotLimit, svc.gotOffset, tt.wantLimit, tt.wantOffset)
				}
			})
		}
	}
}

func TestListResponseShape(t *testing.T) {
	rec := do(t, NewSyncRouter(&fakeService{}), http.MethodGet, "/reservations?limit=5&offset=10", "")
	var got struct {
		Items  []reservation.Reservation `json:"items"`
		Limit  int                       `json:"limit"`
		Offset int                       `json:"offset"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != 1 || got.Limit != 5 || got.Offset != 10 {
		t.Fatalf("unexpected list response: %s", rec.Body)
	}
}

func TestSyncWriteRoutes(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		path        string
		body        string
		wantStatus  int
		wantUpdated uint
		wantDeleted uint
	}{
		{"put replaces reservation", http.MethodPut, "/reservations/1", validBody, http.StatusOK, 1, 0},
		{"put missing reservation", http.MethodPut, "/reservations/9", validBody, http.StatusNotFound, 0, 0},
		{"put invalid input", http.MethodPut, "/reservations/1", `{"email":"bad","name":"Bob"}`, http.StatusBadRequest, 0, 0},
		{"put malformed json", http.MethodPut, "/reservations/1", `{`, http.StatusBadRequest, 0, 0},
		{"put invalid id", http.MethodPut, "/reservations/abc", validBody, http.StatusBadRequest, 0, 0},
		{"delete existing", http.MethodDelete, "/reservations/1", "", http.StatusNoContent, 0, 1},
		{"delete missing", http.MethodDelete, "/reservations/9", "", http.StatusNotFound, 0, 0},
		{"delete invalid id", http.MethodDelete, "/reservations/0", "", http.StatusBadRequest, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{}
			rec := do(t, NewSyncRouter(svc), tt.method, tt.path, tt.body)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body)
			}
			if svc.updatedID != tt.wantUpdated || svc.deletedID != tt.wantDeleted {
				t.Fatalf("updated=%d deleted=%d, want %d/%d", svc.updatedID, svc.deletedID, tt.wantUpdated, tt.wantDeleted)
			}
			if tt.wantStatus == http.StatusOK && !strings.Contains(rec.Body.String(), `"name":"Bob"`) {
				t.Fatalf("body does not contain updated reservation: %s", rec.Body)
			}
		})
	}
}

func TestAsyncWriteRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		pubErr     error
		wantStatus int
		wantJob    *reservation.Job // expected published job, ignoring RequestID
	}{
		{"post enqueues create", http.MethodPost, "/reservations", validBody, nil, http.StatusAccepted,
			&reservation.Job{Type: reservation.JobCreate, Input: reservation.CreateReservationInput{Email: "bob@example.com", Name: "Bob"}}},
		{"put enqueues update", http.MethodPut, "/reservations/7", `{"email":" bob@example.com ","name":"Bob"}`, nil, http.StatusAccepted,
			&reservation.Job{Type: reservation.JobUpdate, ID: 7, Input: reservation.CreateReservationInput{Email: "bob@example.com", Name: "Bob"}}},
		{"put invalid input", http.MethodPut, "/reservations/7", `{"email":"bad","name":"Bob"}`, nil, http.StatusBadRequest, nil},
		{"put malformed json", http.MethodPut, "/reservations/7", `{`, nil, http.StatusBadRequest, nil},
		{"put invalid id", http.MethodPut, "/reservations/abc", validBody, nil, http.StatusBadRequest, nil},
		{"delete enqueues delete", http.MethodDelete, "/reservations/7", "", nil, http.StatusAccepted,
			&reservation.Job{Type: reservation.JobDelete, ID: 7}},
		{"delete invalid id", http.MethodDelete, "/reservations/0", "", nil, http.StatusBadRequest, nil},
		{"publish failure is 500", http.MethodDelete, "/reservations/7", "", errors.New("broker down"), http.StatusInternalServerError, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pub := &fakePublisher{err: tt.pubErr}
			rec := do(t, NewAsyncRouter(&fakeService{}, pub), tt.method, tt.path, tt.body)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantJob == nil {
				if len(pub.jobs) != 0 {
					t.Fatalf("published %d jobs, want none", len(pub.jobs))
				}
				return
			}
			if len(pub.jobs) != 1 {
				t.Fatalf("published %d jobs, want 1", len(pub.jobs))
			}
			got := pub.jobs[0]
			var resp struct {
				Status    string `json:"status"`
				RequestID string `json:"request_id"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if resp.Status != "queued" || resp.RequestID == "" || resp.RequestID != got.RequestID {
				t.Fatalf("unexpected response %s for job %+v", rec.Body, got)
			}
			got.RequestID = ""
			if got != *tt.wantJob {
				t.Fatalf("published job = %+v, want %+v", got, *tt.wantJob)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	for api := range routers(&fakeService{}) {
		t.Run(api+" success", func(t *testing.T) {
			svc := &fakeService{}
			rec := do(t, routers(svc)[api], http.MethodPost, "/admin/truncate", "")
			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
			}
			if !svc.truncated {
				t.Fatal("service was not asked to truncate")
			}
		})
		t.Run(api+" error", func(t *testing.T) {
			svc := &fakeService{truncateErr: errors.New("boom")}
			rec := do(t, routers(svc)[api], http.MethodPost, "/admin/truncate", "")
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
			}
		})
	}
}
