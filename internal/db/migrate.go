package db

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/ResidentAngel/go-pet-project/migrations"
)

// Migrate применяет все ещё не применённые миграции.
func Migrate(dsn string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("источник миграций: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, toMigrateURL(dsn))
	if err != nil {
		return fmt.Errorf("инициализация миграций: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("применение миграций: %w", err)
	}
	return nil
}

// Драйвер pgx v5 в golang-migrate регистрируется под схемой pgx5://,
// а наш DSN начинается с postgres://.
func toMigrateURL(dsn string) string {
	return strings.Replace(dsn, "postgres://", "pgx5://", 1)
}
