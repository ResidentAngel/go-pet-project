package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ResidentAngel/go-pet-project/internal/repository"
)

type fakeStore struct {
	getFn func(ctx context.Context, id int64) (repository.Habit, error)
}

func (f fakeStore) GetByID(ctx context.Context, id int64) (repository.Habit, error) {
	return f.getFn(ctx, id)
}
func (f fakeStore) Create(context.Context, string, *string) (repository.Habit, error) {
	return repository.Habit{}, nil
}
func (f fakeStore) List(context.Context) ([]repository.Habit, error) { return nil, nil }
func (f fakeStore) Delete(context.Context, int64) error              { return nil }
func (f fakeStore) MarkDone(context.Context, int64, time.Time) error { return nil }

func TestGetHabit(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		getFn      func(ctx context.Context, id int64) (repository.Habit, error)
		wantStatus int
	}{
		{
			name: "найдено",
			path: "/habits/1",
			getFn: func(ctx context.Context, id int64) (repository.Habit, error) {
				return repository.Habit{ID: 1, Title: "Run"}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "не найдено",
			path: "/habits/2",
			getFn: func(ctx context.Context, id int64) (repository.Habit, error) {
				return repository.Habit{}, repository.ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "плохой id",
			path:       "/habits/abc",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(fakeStore{getFn: tt.getFn})
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			h.Routes().ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("статус %d, ожидали %d", rec.Code, tt.wantStatus)
			}
		})
	}
}
