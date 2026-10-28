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

// newTestRepo подключается к тестовой базе, применяет миграции и очищает таблицы.
// Без TEST_DATABASE_URL тесты пропускаются, поэтому обычный `go test ./...` не падает.
func newTestRepo(t *testing.T) *HabitRepository {
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

	if _, err := pool.Exec(ctx, `TRUNCATE habit_logs, habits RESTART IDENTITY`); err != nil {
		t.Fatalf("очистка таблиц: %v", err)
	}
	return NewHabitRepository(pool)
}

// daysAgo возвращает дату n дней назад от сегодняшней.
func daysAgo(n int) time.Time {
	return time.Now().AddDate(0, 0, -n)
}

func TestCreateAndGet(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	desc := "morning"
	withDesc, err := r.Create(ctx, "Run", &desc)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	withoutDesc, err := r.Create(ctx, "Read", nil)
	if err != nil {
		t.Fatalf("Create без описания: %v", err)
	}

	got, err := r.GetByID(ctx, withDesc.ID)
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
	got, err = r.GetByID(ctx, withoutDesc.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Description != nil {
		t.Errorf("description = %q, ожидали nil", *got.Description)
	}

	if _, err := r.GetByID(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("для несуществующего id ошибка %v, ожидали ErrNotFound", err)
	}
}

func TestCreateEmptyTitleRejectedByDB(t *testing.T) {
	r := newTestRepo(t)

	// хендлер такое не пропустит, но последний рубеж это CHECK в базе
	if _, err := r.Create(context.Background(), "", nil); err == nil {
		t.Error("пустой title должен нарушать CHECK-ограничение")
	}
}

func TestList(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	got, err := r.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("пустой список: %#v, ожидали пустой непустой-nil срез", got)
	}

	for _, title := range []string{"A", "B", "C"} {
		if _, err := r.Create(ctx, title, nil); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	got, err = r.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 || got[0].Title != "A" || got[2].Title != "C" {
		t.Errorf("получили %+v, ожидали A, B, C по порядку id", got)
	}
}

func TestDelete(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	h, err := r.Create(ctx, "Run", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := r.MarkDone(ctx, h.ID, daysAgo(0)); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	if err := r.Delete(ctx, h.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := r.GetByID(ctx, h.ID); !errors.Is(err, ErrNotFound) {
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

	if err := r.Delete(ctx, h.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("повторное удаление: ошибка %v, ожидали ErrNotFound", err)
	}
}

func TestMarkDone(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	h, err := r.Create(ctx, "Run", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := r.MarkDone(ctx, h.ID, daysAgo(0)); err != nil {
		t.Fatalf("первая отметка: %v", err)
	}
	// тот же день ещё раз: UNIQUE (habit_id, done_on) -> код 23505
	if err := r.MarkDone(ctx, h.ID, daysAgo(0)); !errors.Is(err, ErrAlreadyDone) {
		t.Errorf("повторная отметка: ошибка %v, ожидали ErrAlreadyDone", err)
	}
	// другой день проходит
	if err := r.MarkDone(ctx, h.ID, daysAgo(1)); err != nil {
		t.Errorf("отметка за вчера: %v", err)
	}
	// несуществующая привычка: внешний ключ -> код 23503
	if err := r.MarkDone(ctx, 9999, daysAgo(0)); !errors.Is(err, ErrNotFound) {
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

	r := newTestRepo(t)
	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// у каждого случая своя привычка, поэтому отметки не пересекаются
			h, err := r.Create(ctx, tt.name, nil)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			for _, d := range tt.daysAgo {
				if err := r.MarkDone(ctx, h.ID, daysAgo(d)); err != nil {
					t.Fatalf("MarkDone(%d дней назад): %v", d, err)
				}
			}

			got, err := r.Stats(ctx, h.ID)
			if err != nil {
				t.Fatalf("Stats: %v", err)
			}
			if got.CurrentStreak != tt.wantCurrent || got.BestStreak != tt.wantBest || got.TotalDays != tt.wantTotal {
				t.Errorf("получили %+v, ожидали {current %d, best %d, total %d}",
					got, tt.wantCurrent, tt.wantBest, tt.wantTotal)
			}
		})
	}

	if _, err := r.Stats(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("статистика несуществующей привычки: ошибка %v, ожидали ErrNotFound", err)
	}
}
