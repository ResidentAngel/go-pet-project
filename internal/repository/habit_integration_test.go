package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ResidentAngel/go-pet-project/internal/db"
)

// newTestRepos подключается к тестовой базе, применяет миграции и очищает таблицы.
// Без TEST_DATABASE_URL тесты пропускаются, поэтому обычный `go test ./...` не падает.
func newTestRepos(t *testing.T) (*HabitRepository, *UserRepository) {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан, пропускаю интеграционные тесты")
	}
	// защита от очистки рабочей базы
	if !strings.Contains(dsn, "_test") {
		t.Fatal("TEST_DATABASE_URL должен указывать на тестовую базу (в имени есть _test): тесты очищают таблицы")
	}

	if err := db.Migrate(dsn); err != nil {
		t.Fatalf("миграции: %v", err)
	}

	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("подключение: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `TRUNCATE habit_logs, habits, users RESTART IDENTITY`); err != nil {
		t.Fatalf("очистка таблиц: %v", err)
	}
	return NewHabitRepository(pool), NewUserRepository(pool)
}

func newUser(t *testing.T, users *UserRepository, email string) int64 {
	t.Helper()
	u, err := users.Create(context.Background(), email, "hash-not-used-in-these-tests")
	if err != nil {
		t.Fatalf("создание пользователя %s: %v", email, err)
	}
	return u.ID
}

// daysAgo возвращает дату n дней назад от сегодняшней.
func daysAgo(n int) time.Time {
	return time.Now().AddDate(0, 0, -n)
}

func TestUsers(t *testing.T) {
	_, users := newTestRepos(t)
	ctx := context.Background()

	u, err := users.Create(ctx, "me@example.com", "hash")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.ID == 0 || u.Email != "me@example.com" || u.PasswordHash != "hash" || u.CreatedAt.IsZero() {
		t.Errorf("получили %+v", u)
	}

	// уникальность без учёта регистра: индекс по lower(email) -> код 23505
	if _, err := users.Create(ctx, "ME@example.com", "hash"); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("повторный email: ошибка %v, ожидали ErrEmailTaken", err)
	}

	got, err := users.GetByEmail(ctx, "Me@Example.com")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if got.ID != u.ID || got.PasswordHash != "hash" {
		t.Errorf("получили %+v, ожидали пользователя %d", got, u.ID)
	}

	if _, err := users.GetByEmail(ctx, "nobody@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("неизвестный email: ошибка %v, ожидали ErrNotFound", err)
	}
}

func TestCreateAndGet(t *testing.T) {
	r, users := newTestRepos(t)
	ctx := context.Background()
	uid := newUser(t, users, "me@example.com")

	desc := "morning"
	withDesc, err := r.Create(ctx, uid, "Run", &desc)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	withoutDesc, err := r.Create(ctx, uid, "Read", nil)
	if err != nil {
		t.Fatalf("Create без описания: %v", err)
	}

	got, err := r.GetByID(ctx, uid, withDesc.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Title != "Run" || got.Description == nil || *got.Description != "morning" {
		t.Errorf("получили %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Error("created_at не заполнен")
	}

	// NULL в базе должен превратиться в nil, а не в пустую строку
	got, err = r.GetByID(ctx, uid, withoutDesc.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Description != nil {
		t.Errorf("description = %q, ожидали nil", *got.Description)
	}

	if _, err := r.GetByID(ctx, uid, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("для несуществующего id ошибка %v, ожидали ErrNotFound", err)
	}
}

func TestCreateEmptyTitleRejectedByDB(t *testing.T) {
	r, users := newTestRepos(t)
	uid := newUser(t, users, "me@example.com")

	// хендлер такое не пропустит, но последний рубеж это CHECK в базе
	if _, err := r.Create(context.Background(), uid, "", nil); err == nil {
		t.Error("пустой title должен нарушать CHECK-ограничение")
	}
}

func TestCreateForMissingUserRejectedByDB(t *testing.T) {
	r, _ := newTestRepos(t)

	// внешний ключ habits.user_id -> users.id
	if _, err := r.Create(context.Background(), 9999, "Run", nil); err == nil {
		t.Error("привычка несуществующего пользователя должна нарушать внешний ключ")
	}
}

func TestList(t *testing.T) {
	r, users := newTestRepos(t)
	ctx := context.Background()
	uid := newUser(t, users, "me@example.com")

	got, err := r.List(ctx, uid)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("пустой список: %#v, ожидали пустой не-nil срез", got)
	}

	for _, title := range []string{"A", "B", "C"} {
		if _, err := r.Create(ctx, uid, title, nil); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	got, err = r.List(ctx, uid)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 || got[0].Title != "A" || got[2].Title != "C" {
		t.Errorf("получили %+v, ожидали A, B, C по порядку id", got)
	}
}

func TestDelete(t *testing.T) {
	r, users := newTestRepos(t)
	ctx := context.Background()
	uid := newUser(t, users, "me@example.com")

	h, err := r.Create(ctx, uid, "Run", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := r.MarkDone(ctx, uid, h.ID, daysAgo(0)); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	if err := r.Delete(ctx, uid, h.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := r.GetByID(ctx, uid, h.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("после удаления ошибка %v, ожидали ErrNotFound", err)
	}

	// ON DELETE CASCADE: отметки должны исчезнуть вместе с привычкой
	var n int
	err = r.pool.QueryRow(ctx, `SELECT count(*) FROM habit_logs WHERE habit_id = $1`, h.ID).Scan(&n)
	if err != nil {
		t.Fatalf("подсчёт отметок: %v", err)
	}
	if n != 0 {
		t.Errorf("осталось %d отметок, ожидали 0", n)
	}

	if err := r.Delete(ctx, uid, h.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("повторное удаление: ошибка %v, ожидали ErrNotFound", err)
	}
}

func TestMarkDone(t *testing.T) {
	r, users := newTestRepos(t)
	ctx := context.Background()
	uid := newUser(t, users, "me@example.com")

	h, err := r.Create(ctx, uid, "Run", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := r.MarkDone(ctx, uid, h.ID, daysAgo(0)); err != nil {
		t.Fatalf("первая отметка: %v", err)
	}
	// тот же день ещё раз: UNIQUE (habit_id, done_on) -> код 23505
	if err := r.MarkDone(ctx, uid, h.ID, daysAgo(0)); !errors.Is(err, ErrAlreadyDone) {
		t.Errorf("повторная отметка: ошибка %v, ожидали ErrAlreadyDone", err)
	}
	// другой день проходит
	if err := r.MarkDone(ctx, uid, h.ID, daysAgo(1)); err != nil {
		t.Errorf("отметка за вчера: %v", err)
	}
	// несуществующая привычка: INSERT ... SELECT не вставляет ни одной строки
	if err := r.MarkDone(ctx, uid, 9999, daysAgo(0)); !errors.Is(err, ErrNotFound) {
		t.Errorf("отметка несуществующей привычки: ошибка %v, ожидали ErrNotFound", err)
	}
}

func TestStats(t *testing.T) {
	tests := []struct {
		name        string
		daysAgo     []int // за какие дни (0 = сегодня) есть отметки
		wantCurrent int
		wantBest    int
		wantTotal   int
	}{
		{name: "нет отметок", daysAgo: nil, wantCurrent: 0, wantBest: 0, wantTotal: 0},
		{name: "серия до сегодня и старая серия", daysAgo: []int{0, 1, 2, 12, 13}, wantCurrent: 3, wantBest: 3, wantTotal: 5},
		{name: "только вчера: серия жива", daysAgo: []int{1}, wantCurrent: 1, wantBest: 1, wantTotal: 1},
		{name: "последняя два дня назад: серия оборвана", daysAgo: []int{2, 3}, wantCurrent: 0, wantBest: 2, wantTotal: 2},
		{name: "старая длинная серия и сегодня", daysAgo: []int{0, 5, 6, 7, 8}, wantCurrent: 1, wantBest: 4, wantTotal: 5},
	}

	r, users := newTestRepos(t)
	ctx := context.Background()
	uid := newUser(t, users, "me@example.com")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// у каждого случая своя привычка, поэтому отметки не пересекаются
			h, err := r.Create(ctx, uid, tt.name, nil)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			for _, d := range tt.daysAgo {
				if err := r.MarkDone(ctx, uid, h.ID, daysAgo(d)); err != nil {
					t.Fatalf("MarkDone(%d дней назад): %v", d, err)
				}
			}

			got, err := r.Stats(ctx, uid, h.ID)
			if err != nil {
				t.Fatalf("Stats: %v", err)
			}
			if got.CurrentStreak != tt.wantCurrent || got.BestStreak != tt.wantBest || got.TotalDays != tt.wantTotal {
				t.Errorf("получили %+v, ожидали {current %d, best %d, total %d}",
					got, tt.wantCurrent, tt.wantBest, tt.wantTotal)
			}
		})
	}

	if _, err := r.Stats(ctx, uid, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("статистика несуществующей привычки: ошибка %v, ожидали ErrNotFound", err)
	}
}

// Главная проверка авторизации на уровне данных: один пользователь
// не может ни увидеть, ни изменить привычки другого.
func TestUserIsolation(t *testing.T) {
	r, users := newTestRepos(t)
	ctx := context.Background()
	alice := newUser(t, users, "alice@example.com")
	bob := newUser(t, users, "bob@example.com")

	habit, err := r.Create(ctx, alice, "Run", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := r.MarkDone(ctx, alice, habit.ID, daysAgo(0)); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	if _, err := r.GetByID(ctx, bob, habit.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByID чужой привычки: ошибка %v, ожидали ErrNotFound", err)
	}
	if _, err := r.Stats(ctx, bob, habit.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Stats чужой привычки: ошибка %v, ожидали ErrNotFound", err)
	}
	if err := r.MarkDone(ctx, bob, habit.ID, daysAgo(1)); !errors.Is(err, ErrNotFound) {
		t.Errorf("MarkDone чужой привычки: ошибка %v, ожидали ErrNotFound", err)
	}
	if err := r.Delete(ctx, bob, habit.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete чужой привычки: ошибка %v, ожидали ErrNotFound", err)
	}
	list, err := r.List(ctx, bob)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("Боб видит %d привычек, ожидали 0", len(list))
	}

	// после всех попыток Боба данные Алисы не изменились
	got, err := r.GetByID(ctx, alice, habit.ID)
	if err != nil || got.Title != "Run" {
		t.Errorf("привычка Алисы: %+v, ошибка %v", got, err)
	}
	var logs int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM habit_logs WHERE habit_id = $1`, habit.ID).Scan(&logs); err != nil {
		t.Fatalf("подсчёт отметок: %v", err)
	}
	if logs != 1 {
		t.Errorf("отметок %d, ожидали 1: Боб не должен был ничего добавить", logs)
	}
}

func TestDeleteUserCascades(t *testing.T) {
	r, users := newTestRepos(t)
	ctx := context.Background()
	uid := newUser(t, users, "me@example.com")

	if _, err := r.Create(ctx, uid, "Run", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := r.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, uid); err != nil {
		t.Fatalf("удаление пользователя: %v", err)
	}

	var n int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM habits`).Scan(&n); err != nil {
		t.Fatalf("подсчёт привычек: %v", err)
	}
	if n != 0 {
		t.Errorf("осталось %d привычек, ожидали 0 (ON DELETE CASCADE)", n)
	}
}
