# go-pet-project

Учебный REST API для трекера привычек на Go и PostgreSQL. Можно заводить привычки, отмечать их выполнение по дням и смотреть статистику серий (сколько дней подряд привычка выполняется).

## Стек

- Go, стандартная библиотека `net/http` (маршруты с методами, Go 1.22+)
- PostgreSQL в Docker
- [pgx v5](https://github.com/jackc/pgx) и пул соединений `pgxpool`

## Запуск

Нужны Go, Docker и Docker Compose.

1. Склонировать репозиторий:
   ```bash
   git clone https://github.com/ResidentAngel/go-pet-project.git
   cd go-pet-project
   ```

2. Создать файл настроек из шаблона и при желании поменять пароль:
   ```bash
   cp .env.example .env
   ```
   В PowerShell: `Copy-Item .env.example .env`

3. Поднять базу данных:
   ```bash
   docker compose up -d
   ```

4. Применить миграцию:
   ```powershell
   # PowerShell
   Get-Content migrations/001_init.up.sql | docker exec -i go_pet_postgres psql -U postgres -d habit_tracker
   ```
   ```bash
   # Linux / macOS
   docker exec -i go_pet_postgres psql -U postgres -d habit_tracker < migrations/001_init.up.sql
   ```

5. Запустить сервер:
   ```bash
   go run ./cmd/app
   ```
   Сервер слушает `http://localhost:8080`. Остановить можно через `Ctrl+C`, он завершится корректно (graceful shutdown).

## Настройки

Читаются из файла `.env` или из переменных окружения. Уже заданные переменные окружения имеют приоритет над `.env`.

| Переменная | Описание | По умолчанию |
|---|---|---|
| `DATABASE_URL` | строка подключения к PostgreSQL | обязательная |
| `HTTP_ADDR` | адрес HTTP-сервера | `:8080` |
| `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD` | настройки контейнера с базой | см. `.env.example` |

База доступна с компьютера на порту **5433** (внутри контейнера 5432), поэтому в `DATABASE_URL` указан порт 5433.

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

## Структура проекта

```
cmd/app/             точка входа, сборка зависимостей, graceful shutdown
internal/config/     загрузка настроек из .env и окружения
internal/db/         создание пула соединений с PostgreSQL
internal/repository/ работа с БД, весь SQL
internal/handler/    HTTP-хендлеры и тесты
migrations/          SQL-миграции
```

Хендлеры работают с хранилищем через интерфейс `HabitStore`, поэтому тестируются без базы данных.

## Тесты

```bash
go test ./...
```