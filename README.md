# random-reviewer

Бот для VK Teams, который назначает ревьюеров для code review по алгоритму взвешенного случайного выбора.

## Архитектура

```
cmd/bot/
└── main.go                        # точка входа

internal/
├── app/                            # инициализация бота и обработка команд
├── config/                         # загрузка конфигурации (YAML + env)
├── core/                           # доменные модели, интерфейсы, ошибки
├── migrations/                     # запуск миграций (golang-migrate)
├── repository/
│   ├── fs/                         # реализация репозитория (JSON-файл)
│   └── postgres/                   # реализация репозитория (PostgreSQL)
└── service/random-reviewer/        # бизнес-логика

migrations/                         # SQL-миграции (up/down)
configs/                            # конфигурационные файлы
```

Слои общаются через интерфейсы `core.ReviewersService` и `core.ReviewersRepository`. Зависимости направлены внутрь — сервис не знает о PostgreSQL, `app` не знает о SQL. Есть две взаимозаменяемые реализации репозитория: `postgres` (продовая) и `fs` (JSON-файл, без внешних зависимостей).

## Алгоритм выбора ревьюера

Каждый ревьюер имеет вес (`weight`). Выбор производится взвешенным случайным образом с инверсией весов:

```
score[i] = maxWeight - weight[i] + 1
```

Ревьюер с меньшим весом имеет больший шанс быть выбранным, но все участники участвуют в выборе. После назначения вес ревьюера увеличивается на 1.

При реролле вес предыдущего ревьюера уменьшается на 1 (`GREATEST(weight - 1, 0)`), а вес нового увеличивается. Оба действия выполняются в одной транзакции. Запросивший ревью пользователь и уже назначенные по этому ревью ревьюеры никогда не попадают в пул повторного выбора.

## Заморозка ревьюеров

Ревьюера можно заморозить до конкретной даты (`@bot freeze @user <date>`) — на это время он не участвует в выборе. Дата задаётся включительно.

- При заморозке `weight` не трогается.
- При разморозке (досрочной, `@bot unfreeze @user`, или автоматической по истечении даты заморозки) вес ревьюера выставляется в среднее (`AVG`, округлённое вверх) по весам активных незамороженных ревьюеров чата — чтобы вернувшийся не выпадал из пула на долгое время и не перетягивал его на себя.
- Фоновая задача (`ResetWeights`, тикер раз в час) сама размораживает вес тех, у кого `freeze_time` равен сегодняшнему дню — но не убирает саму отметку `freeze_time`, так что заморозка формально остаётся до конца дня, а вес уже возвращается к среднему.

Такая же логика (среднее по активным незамороженным) применяется при первом добавлении ревьюера — стартовый вес не должен быть меньше нуля и обгонять всех остальных.

## Сброс весов чата

У каждого чата есть `reset_days` (по умолчанию 14, настраивается через `@bot reset <days>`). Фоновая задача (`Reset`, тикер раз в 12 часов) проверяет чаты, у которых с последнего сброса прошло больше `reset_days`, обнуляет вес всех ревьюеров чата и обновляет `last_reset`.

## Очистка старых ревью

Фоновая задача (`Clean`, тикер раз в 24 часа) удаляет ревью (и связанные с ними сообщения) старше 14 дней — история не хранится вечно.

## Реролл и удаление старого сообщения

При каждом назначении/реролле бот сохраняет `message_id` своего последнего сообщения по ревью. Если реролл заменяет уже отправленное сообщение, старое сообщение бота удаляется в VK Teams — в чате остаётся только актуальное упоминание ревьюера.

## Схема базы данных

```sql
CREATE TABLE chats
(
    chat_id    VARCHAR(255) PRIMARY KEY,
    reset_days INT NOT NULL DEFAULT 14 CHECK ( reset_days > 0 ),
    last_reset TIMESTAMPTZ
);

CREATE TABLE reviewers
(
    reviewer_id BIGSERIAL PRIMARY KEY,
    user_id     VARCHAR(255) NOT NULL,
    chat_id     VARCHAR(255) NOT NULL REFERENCES chats (chat_id),
    UNIQUE (user_id, chat_id),
    weight      INT          NOT NULL DEFAULT 0 CHECK ( weight >= 0 ),
    freeze_time TIMESTAMPTZ,
    is_deleted  BOOLEAN      NOT NULL DEFAULT FALSE
);

CREATE TABLE reviews
(
    review_id   BIGSERIAL PRIMARY KEY,
    reviewer_id BIGINT       NOT NULL REFERENCES reviewers (reviewer_id),
    owner_id    VARCHAR(255) NOT NULL,
    message_id  VARCHAR(255),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE reviews_messages
(
    review_id   BIGINT       NOT NULL REFERENCES reviews (review_id),
    reviewer_id BIGINT       NOT NULL REFERENCES reviewers (reviewer_id),
    message_id  VARCHAR(255) NOT NULL,
    PRIMARY KEY (review_id, reviewer_id, message_id)
);
```

`is_deleted = TRUE` используется вместо физического удаления ревьюера, чтобы не нарушать историю назначений (FK на `reviews`).

## Команды

| Команда | Описание |
|---|---|
| `@bot` | Назначить ревьюера (или реролл, если это реплай на сообщение бота) |
| `@bot add @user` | Добавить ревьюера в чат |
| `@bot remove @user` | Удалить ревьюера из чата |
| `@bot list` | Показать список ревьюеров чата с весами и заморозками |
| `@bot reset <days>` | Установить период сброса весов (по умолчанию 14 дней) |
| `@bot freeze @user <дата>` | Заморозить ревьюера до даты включительно (формат `dd.mm.yyyy`) |
| `@bot unfreeze @user` | Досрочно разморозить ревьюера |
| `@bot help` | Список команд |

Реролл выполняется реплаем на сообщение бота с назначенным ревьюером (или на исходное сообщение, к которому уже было назначение) — без ключевого слова.

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

`docker-compose.yml` всегда поднимает контейнер `postgres`, но какой репозиторий реально использует бот, определяется `STORAGE_TYPE` в `.env`:
- `postgres` — миграции применяются автоматически при старте через отдельное соединение, основное соединение остаётся открытым для работы бота;
- `fs` — состояние хранится в JSON-файле по пути `STORAGE_PATH` (контейнер `postgres` в этом случае поднимается, но не используется).

---

## TODO

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