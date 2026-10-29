package config

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

type Config struct {
	DatabaseURL string
	HTTPAddr    string
	JWTSecret   string
}

func Load() (Config, error) {
	if err := loadEnvFile(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return Config{}, errors.New("DATABASE_URL не задан")
	}

	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 {
		return Config{}, errors.New("JWT_SECRET не задан или короче 32 символов")
	}

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	return Config{DatabaseURL: dsn, HTTPAddr: addr, JWTSecret: secret}, nil
}

// loadEnvFile кладёт переменные из файла в окружение.
// Уже заданные переменные не перезаписываются.
func loadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}
