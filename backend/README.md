# Студак

Backend на Go для знакомств по вузам. REST API и webhook MAX работают в одном
процессе, данные хранятся в PostgreSQL. Для локального и серверного запуска есть
Docker Compose.

## Быстрый запуск

Нужны Docker и Docker Compose.

```bash
[ -f .env ] || cp .env.example .env
openssl rand -hex 32  # вставьте результат в POSTGRES_PASSWORD
openssl rand -hex 32  # вставьте результат в API_TOKEN
docker compose up --build -d
docker compose ps --all
curl http://127.0.0.1:8080/health
```

Ожидаемый ответ healthcheck:

```json
{"status":"ok"}
```

PostgreSQL не публикуется на хост и доступен только backend-контейнеру. Сам API
по умолчанию слушает только `127.0.0.1:8080`; наружу его следует выпускать через
HTTPS reverse proxy или Ngrok.

Скрипт `./start.sh -d` при первом запуске автоматически создаёт файл
`.grafana-admin-password` с правами `0600`. Он исключён из Git.

Остановка без удаления данных:

```bash
docker compose down
```

Удаление контейнеров вместе с томом PostgreSQL:

```bash
docker compose down -v
```

## Настройки

Обязательные переменные в `.env`:

- `POSTGRES_PASSWORD` — длинный случайный пароль PostgreSQL.
- `API_TOKEN` — bearer-токен REST API, минимум 32 символа.

Опциональные интеграции:

- `TG_BOT_TOKEN` — токен Telegram формата `<числовой-id>:<секрет>`; обработчик
  Telegram пока является заглушкой.
- `TG_WEBHOOK_SECRET` — обязателен при заданном `TG_BOT_TOKEN`, 16–256 безопасных символов.
- `MAX_BOT_TOKEN` — токен MAX.
- `MAX_WEBHOOK_SECRET` — обязателен при заданном `MAX_BOT_TOKEN`, минимум 16 символов.
- `WEBHOOK_BASE_URL` — публичный HTTPS URL без завершающего `/`.
- `CORS_ORIGINS` — разрешённые origins через запятую. Пустое значение запрещает
  cross-origin запросы; `*` запрещён в production.
- `GRAFANA_ADMIN_USER` — логин Grafana, по умолчанию `admin`.
- `GRAFANA_PORT` — локальный порт Grafana, по умолчанию `3000`.
- `PROMETHEUS_PORT` — локальный порт Prometheus, по умолчанию `9090`.

Для внешней managed-базы можно передать `DATABASE_URL`. Если она не задана,
приложение собирает подключение из `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`,
`DB_PASSWORD` и `DB_SSLMODE`. Для внешней базы оставляйте `sslmode=require` или
строже; `disable` используется только во внутренней Docker-сети.

MAX использует TLS-цепочку `Russian Trusted Root CA`, отсутствующую в стандартном
Alpine CA bundle. Проверенный корневой сертификат встроен только в HTTP-клиент
фиксированного MAX API и не добавляется в системное хранилище или Telegram-клиент.
Его SHA-256 fingerprint фиксируется unit-тестом; источник сертификата — портал
Госуслуг `https://www.gosuslugi.ru/crt`.

## REST API

Все REST-маршруты, кроме `/health` и webhook, требуют заголовок:

```text
Authorization: Bearer <API_TOKEN>
```

Пример:

```bash
curl -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:8080/universities?q=МГУ
```

Маршруты:

- `POST /users` — создать анкету.
- `GET /users/:id` — получить анкету.
- `PATCH /users/:id` — обновить анкету.
- `GET /users/next/:userId` — следующая анкета.
- `POST /like` — лайк или пропуск.
- `GET /universities?q=` — поиск вузов.
- `POST /webhook/max` — webhook MAX; создаётся только при наличии токена.
- `POST /webhook/tg` — заглушка Telegram.

`telegram_id`, `max_id` и внутренний `web_uuid` никогда не сериализуются в API.
Глобальный `API_TOKEN` подходит для закрытого backend-to-backend API. Перед
подключением публичного браузерного клиента нужно добавить пользовательские
сессии/JWT и проверку владения каждой анкетой — класть глобальный токен во
frontend нельзя.

## Локальный запуск без Docker

Поднимите PostgreSQL и задайте подключение:

```bash
export DATABASE_URL='postgres://dating:password@127.0.0.1:5432/dating?sslmode=disable'
export API_TOKEN="$(openssl rand -hex 32)"
export GOOSE_DRIVER=postgres
export GOOSE_DBSTRING="$DATABASE_URL"
export GOOSE_MIGRATION_DIR=./migrations
goose up
go run ./cmd/server
```

## Миграции базы данных

Схема хранится в версионированных SQL-файлах каталога `migrations` и применяется
утилитой Goose. При `docker compose up` сервис `migrate` выполняет `goose up`, и
backend запускается только после успешного завершения миграций. Повторный запуск
безопасен: Goose применяет только новые версии.

Проверить состояние и применить миграции вручную:

```bash
docker compose run --rm migrate status
docker compose run --rm migrate up
```

Откатить последнюю миграцию (команда удаляет данные, если это указано в секции
`Down`, поэтому сначала сделайте резервную копию):

```bash
docker compose run --rm migrate down
```

Для создания нового файла установите зафиксированную версию Goose и создайте
миграцию в рабочем каталоге:

```bash
go install github.com/pressly/goose/v3/cmd/goose@v3.29.0
GOOSE_MIGRATION_DIR=./migrations goose create add_profile_fields sql
```

После создания заполните обе секции `-- +goose Up` и `-- +goose Down`, затем
проверьте миграцию на отдельной PostgreSQL-базе. Runtime-приложение намеренно не
использует `AutoMigrate`: изменение схемы без зафиксированной версии исключено.

Параметры пула:

- `DB_MAX_OPEN_CONNS` — 25 по умолчанию.
- `DB_MAX_IDLE_CONNS` — 10.
- `DB_CONN_MAX_LIFETIME` — 30 минут.
- `DB_CONN_MAX_IDLE_TIME` — 5 минут.

## Проверки

```bash
go test ./...
go test -race ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
go run github.com/securego/gosec/v2/cmd/gosec@latest -quiet ./...
docker compose config
```

Unit-тесты используют временную SQLite только как быстрый тестовый адаптер;
production-бинарник подключает исключительно PostgreSQL. SQL-миграции следует
дополнительно прогонять в CI на чистом экземпляре PostgreSQL.

## Эксплуатация

### Логи и метрики в Grafana

После запуска откройте `http://127.0.0.1:3000`. Логин по умолчанию — `admin`,
пароль можно вывести локально:

```bash
cat .grafana-admin-password
```

Готовый dashboard находится в разделе **Dashboards → Studak → Студак — логи и
метрики**. В нём доступны:

- логи всех контейнеров проекта с фильтром по сервису;
- частота и HTTP-статусы запросов;
- задержка p95, количество goroutine, память и доступность backend.

Prometheus доступен локально на `http://127.0.0.1:9090`. Точка `/metrics`
backend работает на отдельном порту `9091` только внутри Docker-сети и на хост
не публикуется. Loki также не публикуется и доступен только Grafana и Alloy.
Prometheus хранит метрики 15 дней, Loki — логи 7 дней. Docker JSON-логи
ротируются: не более трёх файлов по 10 МБ на контейнер.

Быстрая проверка стека:

```bash
docker compose ps
docker compose logs --tail=100 alloy loki prometheus grafana
curl http://127.0.0.1:9090/-/ready
```

Alloy читает Docker API через read-only mount `/var/run/docker.sock` и собирает
только контейнеры с label проекта `studak`. Сам Docker socket даёт контейнеру
широкий доступ к Docker Engine даже при read-only mount, поэтому не добавляйте в
образ Alloy сторонние плагины и используйте только зафиксированную официальную
версию образа.

Смотреть runtime-логи:

```bash
docker compose logs -f backend
```

Внутренние ошибки выводятся с префиксом `ERROR`, описанием операции и
`stacktrace`. Ожидаемые ошибки входных данных (`400`) логируются без stack trace,
чтобы внешние запросы не могли бесконтрольно раздувать логи. Токен Telegram
редактируется из текста сетевых ошибок.

- Делайте регулярный `pg_dump` и проверяйте восстановление резервной копии.
- Не коммитьте `.env`, дампы БД или логи.
- Завершайте TLS на reverse proxy и добавляйте там rate limiting.
- Используйте отдельные токены ботов и базы для staging и production.
- Перед миграциями production-базы делайте резервную копию и проверяйте откат.
