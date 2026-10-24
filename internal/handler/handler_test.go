package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ResidentAngel/go-pet-project/internal/repository"
)

type fakeStore struct {
	getFn    func(ctx context.Context, id int64) (repository.Habit, error)
	createFn func(ctx context.Context, title string, description *string) (repository.Habit, error)
}

func (fakeStore) Stats(context.Context, int64) (repository.Stats, error) {
	return repository.Stats{}, nil
}

func (f fakeStore) GetByID(ctx context.Context, id int64) (repository.Habit, error) {
	return f.getFn(ctx, id)
}
func (f fakeStore) Create(ctx context.Context, title string, description *string) (repository.Habit, error) {
	return f.createFn(ctx, title, description)
}
func (f fakeStore) List(context.Context) ([]repository.Habit, error) { return nil, nil }
func (f fakeStore) Delete(context.Context, int64) error              { return nil }
func (f fakeStore) MarkDone(context.Context, int64, time.Time) error { return nil }

func TestCreateHabit(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		createFn   func(ctx context.Context, title string, description *string) (repository.Habit, error)
		wantStatus int
	}{
		{
			name: "успех",
			body: `{"title":"Run","description":"morning"}`,
			createFn: func(ctx context.Context, title string, description *string) (repository.Habit, error) {
				return repository.Habit{ID: 1, Title: title, Description: description}, nil
			},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "пустой title",
			body:       `{"title":""}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "title из пробелов",
			body:       `{"title":"   "}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "битый JSON",
			body:       `{"title":`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "ошибка хранилища",
			body: `{"title":"Run"}`,
			createFn: func(ctx context.Context, title string, description *string) (repository.Habit, error) {
				return repository.Habit{}, errors.New("boom")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(fakeStore{createFn: tt.createFn})
			req := httptest.NewRequest(http.MethodPost, "/habits", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()

			h.Routes().ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("статус %d, ожидали %d", rec.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusCreated {
				var got repository.Habit
				if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
					t.Fatalf("ответ не JSON: %v", err)
				}
				if got.Title != "Run" {
					t.Errorf("title = %q, ожидали Run", got.Title)
				}
			}
		})
	}
}
