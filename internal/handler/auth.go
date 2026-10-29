package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/ResidentAngel/go-pet-project/internal/auth"
	"github.com/ResidentAngel/go-pet-project/internal/repository"
)

type ctxKey struct{}

// userID достаёт id пользователя, который положил в контекст requireAuth.
func userID(r *http.Request) int64 {
	id, _ := r.Context().Value(ctxKey{}).(int64)
	return id
}

// requireAuth пропускает запрос дальше только с действительным токеном.
func (h *Handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			unauthorized(w, "нужен заголовок Authorization: Bearer <токен>")
			return
		}
		id, err := h.tokens.Parse(token)
		if err != nil {
			unauthorized(w, "недействительный или просроченный токен")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, id)
		next(w, r.WithContext(ctx))
	}
}

func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeError(w, http.StatusUnauthorized, msg)
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req credentials
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный JSON")
		return
	}

	email := normalizeEmail(req.Email)
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
		writeError(w, http.StatusBadRequest, "некорректный email")
		return
	}
	if utf8.RuneCountInString(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "пароль должен быть не короче 8 символов")
		return
	}
	if len(req.Password) > 72 { // bcrypt учитывает только первые 72 байта
		writeError(w, http.StatusBadRequest, "пароль должен быть не длиннее 72 байт")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		h.fail(w, err)
		return
	}
	user, err := h.users.Create(r.Context(), email, hash)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.respondToken(w, http.StatusCreated, user.ID)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req credentials
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный JSON")
		return
	}

	user, err := h.users.GetByEmail(r.Context(), normalizeEmail(req.Email))
	if errors.Is(err, repository.ErrNotFound) {
		auth.BurnTime(req.Password)
		unauthorized(w, "неверный email или пароль")
		return
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	if !auth.CheckPassword(user.PasswordHash, req.Password) {
		unauthorized(w, "неверный email или пароль")
		return
	}
	h.respondToken(w, http.StatusOK, user.ID)
}

func (h *Handler) respondToken(w http.ResponseWriter, status int, id int64) {
	token, err := h.tokens.Issue(id)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, status, map[string]string{"token": token})
}
