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

	"github.com/ResidentAngel/go-pet-project/internal/auth"
	"github.com/ResidentAngel/go-pet-project/internal/repository"
)

// HabitStore и UserStore описывают, что хендлерам нужно от хранилища.
// Интерфейсы объявлены здесь, у потребителя, а не в repository.
type HabitStore interface {
	Create(ctx context.Context, userID int64, title string, description *string) (repository.Habit, error)
	GetByID(ctx context.Context, userID, id int64) (repository.Habit, error)
	List(ctx context.Context, userID int64) ([]repository.Habit, error)
	Delete(ctx context.Context, userID, id int64) error
	MarkDone(ctx context.Context, userID, habitID int64, date time.Time) error
	Stats(ctx context.Context, userID, habitID int64) (repository.Stats, error)
}

type UserStore interface {
	Create(ctx context.Context, email, passwordHash string) (repository.User, error)
	GetByEmail(ctx context.Context, email string) (repository.User, error)
}

type Handler struct {
	habits HabitStore
	users  UserStore
	tokens *auth.TokenManager
}

func New(habits HabitStore, users UserStore, tokens *auth.TokenManager) *Handler {
	return &Handler{habits: habits, users: users, tokens: tokens}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /auth/register", h.register)
	mux.HandleFunc("POST /auth/login", h.login)

	mux.HandleFunc("POST /habits", h.requireAuth(h.create))
	mux.HandleFunc("GET /habits", h.requireAuth(h.list))
	mux.HandleFunc("GET /habits/{id}", h.requireAuth(h.get))
	mux.HandleFunc("DELETE /habits/{id}", h.requireAuth(h.delete))
	mux.HandleFunc("POST /habits/{id}/done", h.requireAuth(h.markDone))
	mux.HandleFunc("GET /habits/{id}/stats", h.requireAuth(h.stats))
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

	habit, err := h.habits.Create(r.Context(), userID(r), req.Title, req.Description)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, habit)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	habits, err := h.habits.List(r.Context(), userID(r))
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
	habit, err := h.habits.GetByID(r.Context(), userID(r), id)
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
	if err := h.habits.Delete(r.Context(), userID(r), id); err != nil {
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

	if err := h.habits.MarkDone(r.Context(), userID(r), id, date); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "некорректный id")
		return
	}
	s, err := h.habits.Stats(r.Context(), userID(r), id)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// fail переводит ошибки хранилища в HTTP-статусы.
func (h *Handler) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, http.StatusNotFound, "не найдено")
	case errors.Is(err, repository.ErrAlreadyDone):
		writeError(w, http.StatusConflict, "уже отмечено за этот день")
	case errors.Is(err, repository.ErrEmailTaken):
		writeError(w, http.StatusConflict, "email уже зарегистрирован")
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
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		log.Printf("запись ответа: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
