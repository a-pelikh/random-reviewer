# random-reviewer

Бот для VK Teams, который назначает ревьюеров для code review по алгоритму взвешенного случайного выбора.

## Архитектура

```
cmd/bot/
└── main.go                        # точка входа

internal/
├── app/                           # инициализация и обработка событий бота
├── config/                        # загрузка конфигурации (YAML + env)
├── core/                          # доменные модели, интерфейсы, ошибки
├── migrations/                    # запуск миграций (golang-migrate)
├── repository/
│   ├── fs/                        # реализация репозитория (JSON-файл)
│   └── postgres/                  # реализация репозитория (PostgreSQL)
└── service/random-reviewer/       # бизнес-логика

migrations/                        # SQL-миграции (up/down)
configs/                           # конфигурационные файлы
```

Слои общаются через интерфейсы `core.ReviewersService` и `core.ReviewersRepository`. Зависимости направлены внутрь — сервис не знает о PostgreSQL, app не знает о SQL.

## Алгоритм выбора ревьюера

Каждый ревьюер имеет вес (`weight`). Выбор производится взвешенным случайным образом с инверсией весов:

```
score[i] = maxWeight - weight[i] + 1
```

Ревьюер с меньшим весом имеет больший шанс быть выбранным, но все участники участвуют в выборе. После назначения вес ревьюера увеличивается на 1.

При реролле вес предыдущего ревьюера уменьшается на 1 (`GREATEST(weight - 1, 0)`), а новому ревьюеру вес увеличивается. Оба действия выполняются в одной транзакции.

Запросивший ревью пользователь никогда не попадает в пул выбора.

## Приватность

User ID всех ревьюеров хранятся в зашифрованном виде (AES-GCM с HMAC-based nonce). Шифрование детерминировано: один и тот же `user_id` всегда даёт один и тот же зашифротекст, что позволяет сравнивать ID без расшифровки. Секрет задаётся полем `bot.secret` в конфигурации.

## Цепочка реролла

При первом назначении бот сохраняет три объекта:

- **M0** — исходное сообщение (PR-текст или то, на которое ответил пользователь)
- **M1** — сообщение с упоминанием `@bot` (якорная запись без ревьюера)
- **M2** — ответ бота с назначенным ревьюером

Все три связаны общим `root_message_id = M0`. Последующий реплай на **любое** из них запускает реролл.

При реролле уже назначенные ревьюеры по всей цепочке исключаются из пула — повторов нет.

Если пользователь вызывает бота непосредственно в M0 (без отдельного M1), M0 сам становится корнем цепочки.

## Схема базы данных

```sql
CREATE TYPE reset_types AS ENUM ('day', 'week', 'month');

CREATE TABLE chats (
    chat_id VARCHAR(125) PRIMARY KEY,
    reset   reset_types NOT NULL DEFAULT 'week'
);

CREATE TABLE reviewers (
    user_id     VARCHAR(125) NOT NULL,
    chat_id     VARCHAR(125) NOT NULL,
    PRIMARY KEY (user_id, chat_id),
    FOREIGN KEY (chat_id) REFERENCES chats(chat_id),
    weight      INTEGER     NOT NULL DEFAULT 0,
    freeze_time TIMESTAMPTZ,
    is_deleted  BOOLEAN     NOT NULL DEFAULT FALSE
);

CREATE TABLE reviews (
    review_id      BIGSERIAL    PRIMARY KEY,
    reviewer_id    VARCHAR(125),          -- NULL для якорных записей (M0/M1)
    chat_id        VARCHAR(125) NOT NULL,
    message_id     VARCHAR(125) NOT NULL UNIQUE,
    prev_message_id VARCHAR(125),         -- предыдущее звено цепочки
    root_message_id VARCHAR(125)          -- корень цепочки (M0)
);
```

`reviewer_id = NULL` означает якорную запись (сообщение без назначенного ревьюера).  
`is_deleted = TRUE` используется вместо физического удаления, чтобы не нарушать историю назначений.

## Команды

| Команда | Описание |
|---|---|
| `@bot` | Назначить ревьюера |
| `@bot add @user` | Добавить ревьюера в чат |
| `@bot remove @user` | Удалить ревьюера из чата |
| `@bot help` | Список команд |

Реролл выполняется **без ключевого слова** — достаточно сделать реплай на любое сообщение из цепочки ревью (M0, M1 или M2).

## Конфигурация

`configs/random-reviewer.yaml`:
```yaml
bot:
  token: ${BOT_TOKEN}
  api_url: ${BOT_API_URL}
  secret: ${BOT_SECRET}

storage:
  type: postgres   # или "fs" для хранения в JSON-файле
  path: data.json  # используется только при type=fs

postgres:
  user: ${POSTGRES_USER}
  password: ${POSTGRES_PASSWORD}
  db: ${POSTGRES_DB}
  host: ${POSTGRES_HOST}
  port: ${POSTGRES_PORT}
```

Переменные окружения загружаются из `.env`. Пример — `.env.example`.

## Запуск

```bash
docker compose up --build
```

Миграции применяются автоматически при старте через отдельное соединение. Основное соединение остаётся открытым для работы бота.

---

## TODO

### Сброс весов

Поле `chats.reset` хранит тип периода (`day` / `week` / `month`), но логика сброса не реализована.

**Предлагаемый подход:** фоновая горутина в `app.go`, которая по тику проверяет каждый чат и сбрасывает веса всех ревьюеров в 0 при наступлении нового периода. Для отслеживания момента сброса необходимо добавить поле `last_reset TIMESTAMPTZ` в таблицу `chats`.

```sql
ALTER TABLE chats ADD COLUMN last_reset TIMESTAMPTZ;
```

```go
ResetWeights(ctx context.Context, chatID ChatID) error
GetChatsForReset(ctx context.Context) ([]Chat, error)
```

### Транзакционная отправка сообщений

**Текущая проблема:** запись в БД (`AssignReviewer`) и отправка сообщения в VK Teams — два независимых действия. При сбое между ними возможна рассинхронизация: сообщение отправлено, но вес не обновлён (или наоборот).

**Предлагаемый подход — Transactional Outbox:**

1. В рамках транзакции записывать назначение ревьюера и событие отправки сообщения в таблицу `outbox`.
2. Отдельный воркер читает `outbox` и отправляет сообщения в VK Teams, помечая их как доставленные.

```sql
CREATE TABLE outbox (
    id         BIGSERIAL    PRIMARY KEY,
    payload    JSONB        NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    sent_at    TIMESTAMPTZ
);
```