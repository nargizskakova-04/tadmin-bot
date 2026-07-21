# Admin Bot — Telegram-бот для управления Piscine

Бот для администраторов школы программирования [Tomorrow School](https://tomorrow-school.com). Автоматически отправляет анонсы по расписанию в чат админов. Создаёт Google Sheets таблицы защит по нажатию кнопки.

## Что делает бот

Бот поддерживает три типа Piscine: **Go** (4 недели), **JS** (4 недели), **AI** (3 недели). Каждую неделю он автоматически определяет текущую неделю через 01-edu API и отправляет нужные сообщения:

| День | Время | Сообщение | Недели |
|------|-------|-----------|--------|
| Пн | 10:00 | FAQ для новичков | только 1-я |
| Чт | 14:30 | Анонс экзамена + регистрация на рейд | 1–3 (не финальная) |
| Чт | 14:00 | Анонс Final Exam | только финальная |
| Пт | 10:00 | Анонс хакатона (Go и JS) | только 3-я |
| Вс | 15:00 | Напоминание о таблице защит + сообщение студентам | 1–3 (не финальная) |

Воскресное сообщение для админов содержит inline-кнопки для создания Google Sheets таблицы защит.

## Архитектура

```
cmd/bot/main.go                          — точка входа
internal/
├── config/                              — конфигурация из .env
├── domain/                              — типы, интерфейсы (порты)
│   ├── piscine.go                       — PiscineType, RaidInfo, маппинг рейд→неделя
│   ├── message.go                       — MessageType (6 типов сообщений)
│   ├── ports.go                         — интерфейсы OneEduClient, BotSender и др.
│   └── errors.go                        — доменные ошибки
├── usecase/
│   ├── raid.go                          — RaidUseCase (определение недели, сборка сообщений)
│   ├── defense.go                       — расчёт таблицы защит (строки, перерывы, время)
│   └── strategy/                        — стратегии по типам Piscine
│       ├── strategy.go                  — интерфейс PiscineStrategy
│       ├── base.go                      — общая логика (SupportsMessage, Type, TemplateVars)
│       ├── go.go, js.go, ai.go          — конкретные стратегии
├── delivery/telegram/
│   ├── handler.go                       — обработчики команд и callback'ов
│   └── router.go                        — регистрация обработчиков
├── infra/
│   ├── oneedu/                          — HTTP-клиент к 01-edu GraphQL API
│   │   ├── client.go                    — runQuery + GetCurrentPiscineID, GetRaidsByPiscineID, ...
│   │   ├── types.go                     — структуры GraphQL-ответов
│   │   └── queries/
│   │       ├── loader.go                — embed + извлечение query по operationName (per-file cache, thread-safe)
│   │       └── raids.graphql            — 5 GraphQL-запросов
│   ├── scheduler/scheduler.go           — CronScheduler (robfig/cron)
│   ├── telegram/adapter.go              — обёртка над go-telegram/bot
│   ├── templates/loader.go              — рендеринг .txt шаблонов (single-pass regex)
│   └── sheets/client.go                 — Google Sheets API v4
messages/                                — шаблоны сообщений (.txt)
```

## Быстрый старт

### Требования

- Go 1.22+
- Telegram Bot Token (от [@BotFather](https://t.me/BotFather))
- Доступ к 01-edu API (base URL + access token)
- Google Cloud сервисный аккаунт (для Google Sheets, опционально)

### Установка

```bash
git clone <repo-url>
cd admin-bot

# Подтянуть зависимости из go.mod
go mod tidy
```

### Конфигурация

Скопируйте `.env.example` в `.env` и заполните:

```bash
cp .env.example .env
```

| Переменная | Обязательная | Описание |
|---|---|---|
| `TELEGRAM_TOKEN` | да | Токен Telegram-бота |
| `ONEEDU_BASE_URL` | да | URL платформы 01-edu (например `learn.tomorrow-school.com`) |
| `PLATFORM_ACCESS_TOKEN` | да | Access token для 01-edu API |
| `CHAT_IDS` | да | ID чатов через запятую (например `-100123456789,-100987654321`) |
| `TIMEZONE` | нет | Часовой пояс для cron (по умолчанию `Asia/Almaty`) |
| `TEMPLATES_PATH` | нет | Путь к шаблонам (по умолчанию `messages`) |
| `GOOGLE_CREDENTIALS_FILE` | нет | Путь к JSON-ключу сервисного аккаунта Google |

### Настройка Google Sheets (опционально)

1. Создайте проект в [Google Cloud Console](https://console.cloud.google.com/)
2. Включите **Google Sheets API** и **Google Drive API**
3. Создайте **Service Account** → скачайте JSON-ключ
4. Положите JSON-файл в корень проекта (например `credentials.json`)
5. Укажите путь в `.env`: `GOOGLE_CREDENTIALS_FILE=credentials.json`

Без этой настройки бот работает, но кнопка «Создать таблицу» будет недоступна.

### Запуск

```bash
go run cmd/bot/main.go
```

## Команды бота

| Команда | Описание |
|---------|----------|
| `/help` | Список команд |
| `/week` | Текущая неделя для всех Piscine |
| `/raidgo` | Информация о рейде Piscine Go |
| `/raidjs` | Информация о рейде Piscine JS |
| `/raidai1` | Информация о рейде Piscine AI 1 |
| `/raidai2` | Информация о рейде Piscine AI 2 |
| `/raidai3` | Информация о рейде Piscine AI 3 |
| `/raidrust` | Информация о рейде Piscine RUST |
| `/create_tables` | Создать Google Sheets таблицы для всех активных рейдов |
| `/get_astana_updates` | Статистика обновлений Astana |
| `/get_region_updates` | Статистика по регионам (кроме Astana) |

## Как работает определение недели

1. Запрос `GetCurrentPiscineId` — получает ID активного Piscine (по `startAt <= now() <= endAt`)
2. Запрос `GetRaidsByPiscine*Id` — получает все рейды этого Piscine
3. `findActiveRaid` — ищет активный рейд (где `startAt <= now <= endAt`)
4. Если активного нет, `countEndedRaids` — если все рейды закончились, это финальная неделя
5. Иначе `findNextUpcomingRaid` — следующий рейд определяет неделю

По имени рейда определяется номер недели через маппинг в `domain.RaidWeekMap`:

| Piscine Go | Piscine JS | Piscine AI 1/2/3 |
|---|---|---|
| quad → неделя 1 | crossword → неделя 1 | backtesting-sp500 → неделя 1 |
| sudoku → неделя 2 | sortable → неделя 2 | forest-prediction → неделя 2 |
| quadchecker → неделя 3 | clonernews → неделя 3 | |
| (нет) → неделя 4 = Final | (нет) → неделя 4 = Final | |

Три потока Piscine AI используют одинаковую карту рейдов (одна программа, разные `parent.id`).
Piscine RUST не имеет захардкоженных имён рейдов: рейды берутся generic-запросом
`GetRaidsByParentId`, а номер недели вычисляется по порядку рейдов (`startAt` по возрастанию).

## Расчёт таблицы защит

Для N команд (`usecase.CalculateDefenseSchedule`):

- **Строк** = ceil(N / 3)
- **Слотов** = строк × 3
- **Перерывы**: < 5 строк — нет; 5–10 — один (в середине); > 10 — два (на трети). Дополнительные перерывы вставляются, если иначе будет > 5 строк подряд без перерыва.
- **Время**: старт 11:00, шаг 30 минут, перерыв 30 минут.

Пример для 35 команд: 12 строк × 3 = 36 слотов, перерывы в 13:00 и 15:30, расписание 11:00–17:30.

## Тестирование

```bash
# Все тесты с детектором гонок
go test -race ./...

# С подробным выводом
go test -race -v ./...

# Конкретный пакет
go test -race -v ./internal/usecase/...
go test -race -v ./internal/domain/...
go test -race -v ./internal/usecase/strategy/...
go test -race -v ./internal/infra/oneedu/queries/...
go test -race -v ./internal/infra/templates/...
go test -race -v ./internal/delivery/telegram/...
go test -race -v ./internal/config/...
```

Покрытие тестами:

| Пакет | Что тестируется | Тесты |
|---|---|---|
| `domain` | Маппинг рейд→неделя, TotalWeeks, HasHackathon, IsFinalWeek, GetRaidQueryName, инварианты RaidWeekMap | 8 |
| `usecase` | Расчёт таблицы защит, DetectCurrentWeek (все ветви), BuildMessage, BuildDefenseReminder | 24 |
| `usecase/strategy` | Полная матрица SupportsMessage (piscine × messageType × week), TemplateKey, TemplateVars | 7 |
| `config` | Загрузка env, обязательные поля, defaults, парсинг ChatIDs, схема URL | 9 |
| `infra/oneedu` | parseJWTExpiry (валидный, малформед, без exp), extractToken | 7 |
| `infra/oneedu/queries` | Загрузка операций, кэш, concurrent-safe, ошибки | 5 |
| `infra/templates` | Подстановка, missing var, repeated, empty value, no recursion, ErrTemplateNotFound | 8 |
| `delivery/telegram` | parsePiscineFromCallback, nextMonday (все дни недели + границы месяца) | 3 |

Итого: 71 тестовая функция, ~200 sub-cases.

## Стек

- **Go** 1.22+
- [go-telegram/bot](https://github.com/go-telegram/bot) — Telegram Bot API
- [robfig/cron/v3](https://github.com/robfig/cron) — cron-планировщик
- [google.golang.org/api](https://pkg.go.dev/google.golang.org/api) — Google Sheets & Drive API
- [joho/godotenv](https://github.com/joho/godotenv) — загрузка .env
- 01-edu GraphQL API — данные о рейдах и командах

query {
  user_aggregate{ #registatration count
    aggregate {
      count
    }
  }
  registration_user{ #checkin, piscine
    userLogin
    registration{
      path
      id
    }
  }

}





query onboarding_games_stats(
  $adminRole: String!
  $startDate: timestamptz!
  $endDate: timestamptz!
) {
  total: user_aggregate{ 
    aggregate {
      count
    }
  }
  succeeded: progress_aggregate(
    where: {
      path: { _eq: "/astanahub/onboarding/games" }
      isDone: { _eq: true }
      grade: { _gte: 1 }
      updatedAt: { _gt: $startDate, _lt: $endDate }
      _not: { user: { roles: { slug: { _in: ["admin", $adminRole] } } } }
    }
    distinct_on: [userId]
  ) {
    aggregate { count }
  }
  
  registration_user{ #checkin, piscine
    userLogin
    registration{
      path
      id
    }
  }
  
}


готовый скрипт по астане
query onboarding_games_stats(
  $startDate: timestamptz!
  $endDate: timestamptz!
) {
  total_astana: user_aggregate{ 
    aggregate {
      count
    }
  }
  succeeded_astana: progress_aggregate(
    where: {
      path: { _eq: "/astanahub/onboarding/games" }
      isDone: { _eq: true }
      grade: { _gte: 1 }
      updatedAt: { _gt: $startDate, _lt: $endDate }
      _not: { user: { roles: { slug: { _in: ["admin", "campus_admin_astanahub"] } } } }
    }
    distinct_on: [userId]
  ) {
    aggregate { count }
  }
  
  checkin_astana: registration_user_aggregate(
    where: {
      registration:{path: {_eq: "/astanahub/onboarding/checkin"}}
    }
  ){
    aggregate{
      count
    }
  }
  
  piscinego_astana: registration_user_aggregate(
    where: {
      registration:{path: {_eq: "/astanahub/piscinego"}}
    }
  ){
    aggregate{
      count
    }
  }
}