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

// fakeStore подменяет базу данных в тестах хендлеров.
// Нужное поведение задаётся через поля-функции.
type fakeStore struct {
	getFn    func(ctx context.Context, id int64) (repository.Habit, error)
	createFn func(ctx context.Context, title string, description *string) (repository.Habit, error)
	statsFn  func(ctx context.Context, habitID int64) (repository.Stats, error)
}

func (f fakeStore) GetByID(ctx context.Context, id int64) (repository.Habit, error) {
	return f.getFn(ctx, id)
}

func (f fakeStore) Create(ctx context.Context, title string, description *string) (repository.Habit, error) {
	return f.createFn(ctx, title, description)
}

func (f fakeStore) Stats(ctx context.Context, habitID int64) (repository.Stats, error) {
	return f.statsFn(ctx, habitID)
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
		{
			name:       "нулевой id",
			path:       "/habits/0",
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

func TestStatsHabit(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		statsFn    func(ctx context.Context, habitID int64) (repository.Stats, error)
		wantStatus int
	}{
		{
			name: "успех",
			path: "/habits/1/stats",
			statsFn: func(ctx context.Context, habitID int64) (repository.Stats, error) {
				return repository.Stats{CurrentStreak: 3, BestStreak: 5, TotalDays: 10}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "не найдено",
			path: "/habits/2/stats",
			statsFn: func(ctx context.Context, habitID int64) (repository.Stats, error) {
				return repository.Stats{}, repository.ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "плохой id",
			path:       "/habits/abc/stats",
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "ошибка хранилища",
			path: "/habits/1/stats",
			statsFn: func(ctx context.Context, habitID int64) (repository.Stats, error) {
				return repository.Stats{}, errors.New("boom")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(fakeStore{statsFn: tt.statsFn})
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			h.Routes().ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("статус %d, ожидали %d", rec.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				var got repository.Stats
				if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
					t.Fatalf("ответ не JSON: %v", err)
				}
				if got.CurrentStreak != 3 || got.BestStreak != 5 || got.TotalDays != 10 {
					t.Errorf("получили %+v, ожидали {3 5 10}", got)
				}
			}
		})
	}
}
