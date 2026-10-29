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

	"github.com/ResidentAngel/go-pet-project/internal/auth"
	"github.com/ResidentAngel/go-pet-project/internal/repository"
)

const testSecret = "test-secret-test-secret-test-secret-12345"

// fakeHabits подменяет базу данных в тестах хендлеров.
// Нужное поведение задаётся полями-функциями. Если поле не задано,
// а хендлер всё же дошёл до хранилища, тест упадёт с паникой: так мы проверяем,
// что запрос без токена или с плохими данными до базы не доходит.
type fakeHabits struct {
	createFn   func(ctx context.Context, userID int64, title string, description *string) (repository.Habit, error)
	getFn      func(ctx context.Context, userID, id int64) (repository.Habit, error)
	listFn     func(ctx context.Context, userID int64) ([]repository.Habit, error)
	deleteFn   func(ctx context.Context, userID, id int64) error
	markDoneFn func(ctx context.Context, userID, habitID int64, date time.Time) error
	statsFn    func(ctx context.Context, userID, habitID int64) (repository.Stats, error)
}

func (f fakeHabits) Create(ctx context.Context, userID int64, title string, description *string) (repository.Habit, error) {
	return f.createFn(ctx, userID, title, description)
}
func (f fakeHabits) GetByID(ctx context.Context, userID, id int64) (repository.Habit, error) {
	return f.getFn(ctx, userID, id)
}
func (f fakeHabits) List(ctx context.Context, userID int64) ([]repository.Habit, error) {
	return f.listFn(ctx, userID)
}
func (f fakeHabits) Delete(ctx context.Context, userID, id int64) error {
	return f.deleteFn(ctx, userID, id)
}
func (f fakeHabits) MarkDone(ctx context.Context, userID, habitID int64, date time.Time) error {
	return f.markDoneFn(ctx, userID, habitID, date)
}
func (f fakeHabits) Stats(ctx context.Context, userID, habitID int64) (repository.Stats, error) {
	return f.statsFn(ctx, userID, habitID)
}

type fakeUsers struct {
	createFn     func(ctx context.Context, email, passwordHash string) (repository.User, error)
	getByEmailFn func(ctx context.Context, email string) (repository.User, error)
}

func (f fakeUsers) Create(ctx context.Context, email, passwordHash string) (repository.User, error) {
	return f.createFn(ctx, email, passwordHash)
}
func (f fakeUsers) GetByEmail(ctx context.Context, email string) (repository.User, error) {
	return f.getByEmailFn(ctx, email)
}

func newTestHandler(habits fakeHabits, users fakeUsers) (http.Handler, *auth.TokenManager) {
	tm := auth.NewTokenManager(testSecret, time.Hour)
	return New(habits, users, tm).Routes(), tm
}

// bearer выпускает токен для пользователя и возвращает значение заголовка Authorization.
func bearer(t *testing.T, tm *auth.TokenManager, userID int64) string {
	t.Helper()
	token, err := tm.Issue(userID)
	if err != nil {
		t.Fatalf("выпуск токена: %v", err)
	}
	return "Bearer " + token
}

func doRequest(h http.Handler, method, path, body, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func checkStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("статус %d, ожидали %d, тело: %s", rec.Code, want, rec.Body.String())
	}
}

func TestRequireAuth(t *testing.T) {
	habits := fakeHabits{
		listFn: func(context.Context, int64) ([]repository.Habit, error) { return []repository.Habit{}, nil },
	}
	h, tm := newTestHandler(habits, fakeUsers{})

	expiredToken, err := auth.NewTokenManager(testSecret, -time.Minute).Issue(1)
	if err != nil {
		t.Fatal(err)
	}
	foreignToken, err := auth.NewTokenManager("another-secret-another-secret-1234567", time.Hour).Issue(1)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"нет заголовка", "", http.StatusUnauthorized},
		{"не Bearer", "Token abc", http.StatusUnauthorized},
		{"мусор вместо токена", "Bearer abc.def.ghi", http.StatusUnauthorized},
		{"просроченный токен", "Bearer " + expiredToken, http.StatusUnauthorized},
		{"подпись другим секретом", "Bearer " + foreignToken, http.StatusUnauthorized},
		{"корректный токен", bearer(t, tm, 1), http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(h, http.MethodGet, "/habits", "", tt.header)
			checkStatus(t, rec, tt.want)
			if tt.want == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") == "" {
				t.Error("в ответе 401 нет заголовка WWW-Authenticate")
			}
		})
	}
}

// Все маршруты привычек закрыты авторизацией. Хранилища пустые: если хендлер
// пропустит запрос без токена, тест упадёт с паникой.
func TestHabitRoutesRequireAuth(t *testing.T) {
	h, _ := newTestHandler(fakeHabits{}, fakeUsers{})

	routes := []struct{ method, path string }{
		{http.MethodPost, "/habits"},
		{http.MethodGet, "/habits"},
		{http.MethodGet, "/habits/1"},
		{http.MethodDelete, "/habits/1"},
		{http.MethodPost, "/habits/1/done"},
		{http.MethodGet, "/habits/1/stats"},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			checkStatus(t, doRequest(h, rt.method, rt.path, "", ""), http.StatusUnauthorized)
		})
	}
}

// id пользователя из токена должен дойти до хранилища.
func TestUserIDFromTokenReachesStore(t *testing.T) {
	var gotUser int64
	habits := fakeHabits{
		listFn: func(ctx context.Context, userID int64) ([]repository.Habit, error) {
			gotUser = userID
			return []repository.Habit{}, nil
		},
	}
	h, tm := newTestHandler(habits, fakeUsers{})

	checkStatus(t, doRequest(h, http.MethodGet, "/habits", "", bearer(t, tm, 42)), http.StatusOK)
	if gotUser != 42 {
		t.Errorf("в хранилище пришёл userID %d, ожидали 42", gotUser)
	}
}

func TestRegister(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		createFn   func(ctx context.Context, email, passwordHash string) (repository.User, error)
		wantStatus int
	}{
		{
			name: "успех, email нормализуется, пароль хешируется",
			body: `{"email":"  Me@Example.com ","password":"secret123"}`,
			createFn: func(ctx context.Context, email, hash string) (repository.User, error) {
				if email != "me@example.com" {
					t.Errorf("email = %q, ожидали me@example.com", email)
				}
				if hash == "secret123" || !auth.CheckPassword(hash, "secret123") {
					t.Error("пароль должен храниться как bcrypt-хеш, а не открытым текстом")
				}
				return repository.User{ID: 7, Email: email}, nil
			},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "email без домена",
			body:       `{"email":"not-an-email","password":"secret123"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "email с именем в формате RFC",
			body:       `{"email":"Bob <bob@example.com>","password":"secret123"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "короткий пароль",
			body:       `{"email":"me@example.com","password":"short"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "слишком длинный пароль",
			body:       `{"email":"me@example.com","password":"` + strings.Repeat("a", 73) + `"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "битый JSON",
			body:       `{"email":`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "email уже занят",
			body: `{"email":"me@example.com","password":"secret123"}`,
			createFn: func(context.Context, string, string) (repository.User, error) {
				return repository.User{}, repository.ErrEmailTaken
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "ошибка хранилища",
			body: `{"email":"me@example.com","password":"secret123"}`,
			createFn: func(context.Context, string, string) (repository.User, error) {
				return repository.User{}, errors.New("boom")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, tm := newTestHandler(fakeHabits{}, fakeUsers{createFn: tt.createFn})
			rec := doRequest(h, http.MethodPost, "/auth/register", tt.body, "")
			checkStatus(t, rec, tt.wantStatus)

			if tt.wantStatus == http.StatusCreated {
				var resp struct {
					Token string `json:"token"`
				}
				if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
					t.Fatalf("ответ не JSON: %v", err)
				}
				id, err := tm.Parse(resp.Token)
				if err != nil || id != 7 {
					t.Errorf("токен содержит id %d (ошибка %v), ожидали 7", id, err)
				}
			}
		})
	}
}

func TestLogin(t *testing.T) {
	hash, err := auth.HashPassword("secret123")
	if err != nil {
		t.Fatal(err)
	}
	getByEmail := func(ctx context.Context, email string) (repository.User, error) {
		if email != "me@example.com" {
			return repository.User{}, repository.ErrNotFound
		}
		return repository.User{ID: 5, Email: email, PasswordHash: hash}, nil
	}

	tests := []struct {
		name         string
		body         string
		getByEmailFn func(ctx context.Context, email string) (repository.User, error)
		wantStatus   int
	}{
		{
			name:         "успех, email нормализуется",
			body:         `{"email":" ME@example.com ","password":"secret123"}`,
			getByEmailFn: getByEmail,
			wantStatus:   http.StatusOK,
		},
		{
			name:         "неверный пароль",
			body:         `{"email":"me@example.com","password":"wrongpass1"}`,
			getByEmailFn: getByEmail,
			wantStatus:   http.StatusUnauthorized,
		},
		{
			name:         "неизвестный email",
			body:         `{"email":"nobody@example.com","password":"secret123"}`,
			getByEmailFn: getByEmail,
			wantStatus:   http.StatusUnauthorized,
		},
		{
			name:         "битый JSON",
			body:         `{"email":`,
			getByEmailFn: getByEmail,
			wantStatus:   http.StatusBadRequest,
		},
		{
			name: "ошибка хранилища",
			body: `{"email":"me@example.com","password":"secret123"}`,
			getByEmailFn: func(context.Context, string) (repository.User, error) {
				return repository.User{}, errors.New("boom")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, tm := newTestHandler(fakeHabits{}, fakeUsers{getByEmailFn: tt.getByEmailFn})
			rec := doRequest(h, http.MethodPost, "/auth/login", tt.body, "")
			checkStatus(t, rec, tt.wantStatus)

			if tt.wantStatus == http.StatusOK {
				var resp struct {
					Token string `json:"token"`
				}
				if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
					t.Fatalf("ответ не JSON: %v", err)
				}
				id, err := tm.Parse(resp.Token)
				if err != nil || id != 5 {
					t.Errorf("токен содержит id %d (ошибка %v), ожидали 5", id, err)
				}
			}
			// ответ на неверный пароль и на неизвестный email должен быть одинаковым
			if tt.wantStatus == http.StatusUnauthorized && !strings.Contains(rec.Body.String(), "неверный email или пароль") {
				t.Errorf("тело ответа: %s", rec.Body.String())
			}
		})
	}
}

func TestCreateHabit(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		createFn   func(ctx context.Context, userID int64, title string, description *string) (repository.Habit, error)
		wantStatus int
	}{
		{
			name: "успех",
			body: `{"title":"Run","description":"morning"}`,
			createFn: func(ctx context.Context, userID int64, title string, description *string) (repository.Habit, error) {
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
			createFn: func(context.Context, int64, string, *string) (repository.Habit, error) {
				return repository.Habit{}, errors.New("boom")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, tm := newTestHandler(fakeHabits{createFn: tt.createFn}, fakeUsers{})
			rec := doRequest(h, http.MethodPost, "/habits", tt.body, bearer(t, tm, 42))
			checkStatus(t, rec, tt.wantStatus)

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
		getFn      func(ctx context.Context, userID, id int64) (repository.Habit, error)
		wantStatus int
	}{
		{
			name: "найдено",
			path: "/habits/1",
			getFn: func(ctx context.Context, userID, id int64) (repository.Habit, error) {
				return repository.Habit{ID: id, Title: "Run"}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "не найдено или чужая",
			path: "/habits/2",
			getFn: func(context.Context, int64, int64) (repository.Habit, error) {
				return repository.Habit{}, repository.ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
		{name: "плохой id", path: "/habits/abc", wantStatus: http.StatusBadRequest},
		{name: "нулевой id", path: "/habits/0", wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, tm := newTestHandler(fakeHabits{getFn: tt.getFn}, fakeUsers{})
			checkStatus(t, doRequest(h, http.MethodGet, tt.path, "", bearer(t, tm, 42)), tt.wantStatus)
		})
	}
}

func TestDeleteHabit(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		deleteFn   func(ctx context.Context, userID, id int64) error
		wantStatus int
	}{
		{
			name:       "удалено",
			path:       "/habits/1",
			deleteFn:   func(context.Context, int64, int64) error { return nil },
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "не найдено или чужая",
			path:       "/habits/2",
			deleteFn:   func(context.Context, int64, int64) error { return repository.ErrNotFound },
			wantStatus: http.StatusNotFound,
		},
		{name: "плохой id", path: "/habits/abc", wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, tm := newTestHandler(fakeHabits{deleteFn: tt.deleteFn}, fakeUsers{})
			checkStatus(t, doRequest(h, http.MethodDelete, tt.path, "", bearer(t, tm, 42)), tt.wantStatus)
		})
	}
}

func TestMarkDone(t *testing.T) {
	ok := func(context.Context, int64, int64, time.Time) error { return nil }

	tests := []struct {
		name       string
		path       string
		body       string
		markDoneFn func(ctx context.Context, userID, habitID int64, date time.Time) error
		wantStatus int
	}{
		{name: "без тела: сегодня", path: "/habits/1/done", markDoneFn: ok, wantStatus: http.StatusCreated},
		{name: "с датой", path: "/habits/1/done", body: `{"date":"2026-10-24"}`, markDoneFn: ok, wantStatus: http.StatusCreated},
		{name: "неверный формат даты", path: "/habits/1/done", body: `{"date":"24.10.2026"}`, wantStatus: http.StatusBadRequest},
		{name: "битый JSON", path: "/habits/1/done", body: `{"date":`, wantStatus: http.StatusBadRequest},
		{name: "плохой id", path: "/habits/abc/done", wantStatus: http.StatusBadRequest},
		{
			name: "уже отмечено",
			path: "/habits/1/done",
			markDoneFn: func(context.Context, int64, int64, time.Time) error {
				return repository.ErrAlreadyDone
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "привычка не найдена или чужая",
			path: "/habits/1/done",
			markDoneFn: func(context.Context, int64, int64, time.Time) error {
				return repository.ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, tm := newTestHandler(fakeHabits{markDoneFn: tt.markDoneFn}, fakeUsers{})
			checkStatus(t, doRequest(h, http.MethodPost, tt.path, tt.body, bearer(t, tm, 42)), tt.wantStatus)
		})
	}
}

func TestStatsHabit(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		statsFn    func(ctx context.Context, userID, habitID int64) (repository.Stats, error)
		wantStatus int
	}{
		{
			name: "успех",
			path: "/habits/1/stats",
			statsFn: func(context.Context, int64, int64) (repository.Stats, error) {
				return repository.Stats{CurrentStreak: 3, BestStreak: 5, TotalDays: 10}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "не найдено или чужая",
			path: "/habits/2/stats",
			statsFn: func(context.Context, int64, int64) (repository.Stats, error) {
				return repository.Stats{}, repository.ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
		{name: "плохой id", path: "/habits/abc/stats", wantStatus: http.StatusBadRequest},
		{
			name: "ошибка хранилища",
			path: "/habits/1/stats",
			statsFn: func(context.Context, int64, int64) (repository.Stats, error) {
				return repository.Stats{}, errors.New("boom")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, tm := newTestHandler(fakeHabits{statsFn: tt.statsFn}, fakeUsers{})
			rec := doRequest(h, http.MethodGet, tt.path, "", bearer(t, tm, 42))
			checkStatus(t, rec, tt.wantStatus)

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
