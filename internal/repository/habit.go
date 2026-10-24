package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound    = errors.New("не найдено")
	ErrAlreadyDone = errors.New("уже отмечено за этот день")
)

type Habit struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Description *string   `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type HabitRepository struct {
	pool *pgxpool.Pool
}

func NewHabitRepository(pool *pgxpool.Pool) *HabitRepository {
	return &HabitRepository{pool: pool}
}

func (r *HabitRepository) Create(ctx context.Context, title string, description *string) (Habit, error) {
	var h Habit
	err := r.pool.QueryRow(ctx,
		`INSERT INTO habits (title, description)
		 VALUES ($1, $2)
		 RETURNING id, title, description, created_at`,
		title, description,
	).Scan(&h.ID, &h.Title, &h.Description, &h.CreatedAt)
	if err != nil {
		return Habit{}, fmt.Errorf("создание привычки: %w", err)
	}
	return h, nil
}

func (r *HabitRepository) GetByID(ctx context.Context, id int64) (Habit, error) {
	var h Habit
	err := r.pool.QueryRow(ctx,
		`SELECT id, title, description, created_at FROM habits WHERE id = $1`,
		id,
	).Scan(&h.ID, &h.Title, &h.Description, &h.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Habit{}, ErrNotFound
		}
		return Habit{}, fmt.Errorf("получение привычки: %w", err)
	}
	return h, nil
}

func (r *HabitRepository) List(ctx context.Context) ([]Habit, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, title, description, created_at FROM habits ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("список привычек: %w", err)
	}
	defer rows.Close()

	habits := []Habit{}
	for rows.Next() {
		var h Habit
		if err := rows.Scan(&h.ID, &h.Title, &h.Description, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("чтение строки: %w", err)
		}
		habits = append(habits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("итерация по строкам: %w", err)
	}
	return habits, nil
}

func (r *HabitRepository) Delete(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM habits WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("удаление привычки: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *HabitRepository) MarkDone(ctx context.Context, habitID int64, date time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO habit_logs (habit_id, done_on) VALUES ($1, $2)`,
		habitID, date,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23505": // unique_violation
				return ErrAlreadyDone
			case "23503": // foreign_key_violation
				return ErrNotFound
			}
		}
		return fmt.Errorf("отметка привычки: %w", err)
	}
	return nil
}
