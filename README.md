# go-pet-project

Учебный REST API для трекера привычек на Go и PostgreSQL. Можно заводить привычки, отмечать их выполнение по дням и смотреть статистику серий (сколько дней подряд привычка выполняется).

## Стек

- Go, стандартная библиотека `net/http` (маршруты с методами, Go 1.22+)
- PostgreSQL 18 в Docker
- [pgx v5](https://github.com/jackc/pgx) и пул соединений `pgxpool`
- [golang-migrate](https://github.com/golang-migrate/migrate) для миграций
- Docker Compose

## Быстрый старт

Нужны Docker и Docker Compose.

```bash
git clone https://github.com/ResidentAngel/go-pet-project.git
cd go-pet-project
cp .env.example .env     # в PowerShell: Copy-Item .env.example .env
docker compose up -d --build
```

Приложение доступно на `http://localhost:8080`. Миграции применяются автоматически при старте, ничего накатывать вручную не нужно.

Остановить: `docker compose down` (данные сохранятся). Остановить и удалить данные: `docker compose down -v`.

### Запуск приложения без Docker

Базу можно поднять отдельно, а приложение запустить локально (нужен Go):

```bash
docker compose up -d postgres
go run ./cmd/app
```

## Настройки

Читаются из файла `.env` или из переменных окружения. Уже заданные переменные окружения имеют приоритет над `.env`.

| Переменная | Описание | По умолчанию |
|---|---|---|
| `DATABASE_URL` | строка подключения к PostgreSQL (для запуска без Docker) | обязательная |
| `HTTP_ADDR` | адрес HTTP-сервера | `:8080` |
| `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD` | настройки контейнера с базой | см. `.env.example` |

База доступна с компьютера на порту **5433** (внутри контейнера 5432). Внутри Docker-сети приложение подключается к хосту `postgres` на порт 5432, строка подключения собирается в `docker-compose.yml`.

## API

| Метод | Путь | Описание | Успех |
|---|---|---|---|
| `POST` | `/habits` | создать привычку | 201 |
| `GET` | `/habits` | список привычек | 200 |
| `GET` | `/habits/{id}` | одна привычка | 200 |
| `DELETE` | `/habits/{id}` | удалить привычку вместе с отметками | 204 |
| `POST` | `/habits/{id}/done` | отметить выполнение | 201 |
| `GET` | `/habits/{id}/stats` | статистика серий | 200 |

Ошибки приходят в виде `{"error": "текст"}`:
`400` неверный запрос, `404` не найдено, `409` уже отмечено за этот день, `500` внутренняя ошибка.

### Примеры (PowerShell)

```powershell
$u = "http://localhost:8080"

# создать привычку
Invoke-RestMethod -Method Post -Uri "$u/habits" -ContentType "application/json; charset=utf-8" `
  -Body '{"title":"Run","description":"morning"}'

# список
Invoke-RestMethod "$u/habits"

# отметить выполнение сегодня
Invoke-RestMethod -Method Post -Uri "$u/habits/1/done"

# отметить за конкретную дату (формат ГГГГ-ММ-ДД)
Invoke-RestMethod -Method Post -Uri "$u/habits/1/done" -ContentType "application/json" `
  -Body '{"date":"2026-10-24"}'

# статистика
Invoke-RestMethod "$u/habits/1/stats"
```

### Примеры (curl)

```bash
curl -X POST localhost:8080/habits -H "Content-Type: application/json" \
  -d '{"title":"Run","description":"morning"}'
curl localhost:8080/habits/1/stats
```

### Ответ статистики

```json
{
  "current_streak": 3,
  "best_streak": 3,
  "total_days": 5
}
```

- `current_streak` текущая серия подряд идущих дней. Серия не прерывается, если последняя отметка была сегодня или вчера.
- `best_streak` самая длинная серия за всё время.
- `total_days` сколько всего дней отмечено.

Серии считаются одним SQL-запросом с оконной функцией `ROW_NUMBER()` (приём «gaps and islands»).

## Структура проекта

```
cmd/app/             точка входа, сборка зависимостей, graceful shutdown
internal/config/     загрузка настроек из .env и окружения
internal/db/         пул соединений и применение миграций
internal/repository/ работа с БД, весь SQL
internal/handler/    HTTP-хендлеры и тесты
migrations/          SQL-миграции, вшиваются в бинарник через go:embed
Dockerfile           сборка приложения (multi-stage)
docker-compose.yml   PostgreSQL и приложение
```

Хендлеры работают с хранилищем через интерфейс `HabitStore`, поэтому тестируются без базы данных.

## Миграции

Файлы лежат в `migrations/` в формате `NNN_имя.up.sql` и `NNN_имя.down.sql`. Приложение применяет все ещё не применённые миграции при старте, версия хранится в таблице `schema_migrations`.

Чтобы изменить схему, не правьте старые файлы, а добавьте новую пару, например `002_add_archived.up.sql` и `002_add_archived.down.sql`.

## Тесты

```bash
go test ./...
```
### Интеграционные тесты

Нужна отдельная тестовая база (тесты очищают таблицы):

```powershell
docker compose up -d postgres
docker exec -it go_pet_postgres psql -U postgres -c "CREATE DATABASE habit_tracker_test;"
$env:TEST_DATABASE_URL = "postgres://postgres:ПАРОЛЬ@127.0.0.1:5433/habit_tracker_test?sslmode=disable"
go test -count=1 ./internal/repository
```

Без `TEST_DATABASE_URL` интеграционные тесты пропускаются.