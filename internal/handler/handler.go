package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ResidentAngel/go-pet-project/internal/repository"
)

// HabitStore описывает, что хендлерам нужно от хранилища.
// Интерфейс объявлен здесь, у потребителя, а не в repository.
type HabitStore interface {
	Create(ctx context.Context, title string, description *string) (repository.Habit, error)
	GetByID(ctx context.Context, id int64) (repository.Habit, error)
	List(ctx context.Context) ([]repository.Habit, error)
	Delete(ctx context.Context, id int64) error
	MarkDone(ctx context.Context, habitID int64, date time.Time) error
}

type Handler struct {
	store HabitStore
}

func New(store HabitStore) *Handler {
	return &Handler{store: store}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /habits", h.create)
	mux.HandleFunc("GET /habits", h.list)
	mux.HandleFunc("GET /habits/{id}", h.get)
	mux.HandleFunc("DELETE /habits/{id}", h.delete)
	mux.HandleFunc("POST /habits/{id}/done", h.markDone)
	return mux
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // не больше 1 МБ

	var req struct {
		Title       string  `json:"title"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title обязателен")
		return
	}

	habit, err := h.store.Create(r.Context(), req.Title, req.Description)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, habit)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	habits, err := h.store.List(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, habits)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "некорректный id")
		return
	}
	habit, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, habit)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "некорректный id")
		return
	}
	if err := h.store.Delete(r.Context(), id); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) markDone(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "некорректный id")
		return
	}

	// Тело необязательное: {"date":"2026-10-24"}. Без него берём сегодня.
	var req struct {
		Date string `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	date := time.Now()
	if req.Date != "" {
		d, err := time.Parse(time.DateOnly, req.Date)
		if err != nil {
			writeError(w, http.StatusBadRequest, "дата должна быть в формате ГГГГ-ММ-ДД")
			return
		}
		date = d
	}

	if err := h.store.MarkDone(r.Context(), id, date); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// fail переводит ошибки хранилища в HTTP-статусы.
func (h *Handler) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, http.StatusNotFound, "не найдено")
	case errors.Is(err, repository.ErrAlreadyDone):
		writeError(w, http.StatusConflict, "уже отмечено за этот день")
	default:
		log.Printf("внутренняя ошибка: %v", err)
		writeError(w, http.StatusInternalServerError, "внутренняя ошибка")
	}
}

func parseID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("запись ответа: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
