# WorkTime Sync — архитектура

## Высокоуровневая схема

```
                          ┌───────────────┐
                          │     Caddy     │
                          │   (TLS,       │
                          │    routing)   │
                          └───────┬───────┘
                                  │
                ┌─────────────────┼─────────────────┐
                │                 │                 │
                ▼                 ▼                 ▼
        ┌────────────┐    ┌─────────────┐    ┌──────────────┐
        │ SvelteKit  │    │  Fiber API  │    │   SSE stream │
        │ (web:3000) │    │ (api:8080)  │    │ /api/v1/notif│
        └────────────┘    └──────┬──────┘    │  /stream     │
                                 │           └──────┬───────┘
                  ┌──────────────┼──────────────────┘
                  │              │              │
                  ▼              ▼              ▼
         ┌──────────────┐  ┌──────────┐  ┌──────────────┐
         │  PostgreSQL  │  │  Redis   │  │   GigaChat   │
         │              │  │          │  │   (https)    │
         │ users        │  │ AI cache │  │              │
         │ employees    │  │ Metrics  │  │ (опционально)│
         │ work_profile │  │ pub/sub  │  └──────────────┘
         │ exceptions   │  │ Asynq    │
         │ integrations │  │ Locks    │
         │ events       │  └────┬─────┘
         │ recommends   │       │
         │ notif…       │       │
         │ audit_log    │       │
         │ ai_msgs      │       │
         └──────────────┘       │
                                │
                  ┌─────────────┼─────────────┐
                  ▼             ▼             ▼
            ┌──────────┐  ┌──────────┐  ┌──────────┐
            │  Asynq   │  │  Asynq   │  │ Webhooks │
            │  worker  │  │ scheduler│  │  receive │
            └─────┬────┘  └────┬─────┘  └────┬─────┘
                  │            │             │
                  ▼            ▼             ▼
                  └─── Provider Registry ────┘
                            │
            ┌───────────────┼───────────────┬──────────────┐
            ▼               ▼               ▼              ▼
        ┌───────┐     ┌─────────┐     ┌───────────┐   ┌──────┐
        │ iCal  │     │ CalDAV  │     │   Jira    │   │ Y.   │
        │ feed  │     │ Yandex/ │     │  REST v3  │   │Track-│
        │       │     │ Apple/  │     │           │   │ er   │
        │       │     │NextCloud│     │           │   │ REST │
        └───────┘     └─────────┘     └───────────┘   └──────┘
```

## Процессы (Docker Compose)

| Service | Role |
|---|---|
| `caddy` | reverse-proxy + автоматический TLS на VPS |
| `web` | SvelteKit (adapter-node), порт 3000 |
| `api` | Fiber v3 HTTP/REST + SSE, порт 8080 |
| `worker` | Asynq worker — sync, metrics, smart-notifier |
| `scheduler` | Asynq cron-планировщик (тикает sync и notifier'a) |
| `postgres` | основная БД (PG 16) |
| `redis` | кэш, очередь Asynq, pub/sub |
| `migrate` | golang-migrate one-shot |

## Поток данных: ингест

```
Provider (Google/iCal/CalDAV/Jira/...)
       │
       ▼ (webhook | polling)
┌─────────────────────────────────┐
│  /api/v1/webhooks/:provider     │
│  → webhook_inbox + enqueue      │
│   sync:incremental:{integ_id}   │
└────────────┬────────────────────┘
             │
             ▼
        Asynq worker
       (distributed lock через
        Redis SETNX по integ_id)
             │
             ▼
    Registry.Calendar(provider)
    → FetchEvents(from, to)
             │
             ▼
    CalendarEventRepo.Upsert
    (ON CONFLICT по source_event_id)
             │
             ▼
     Redis: metrics cache invalidate
```

## Поток данных: метрики и рекомендации

```
        GET /api/v1/metrics/employee/:id
                    │
                    ▼
          ┌──────── cache hit ──────────┐
          │              │              │
          │              ▼              │
          │       Redis: metrics:emp    │
          │              │              │
          │              ▼              │
          │      analytics.Risk(...)    │
          │              │              │
          │   ┌──────────┴──────────┐   │
          ▼   ▼                     ▼   ▼
   Freshness  Conflicts            Load  TZ-drift  HR-mismatch
   (days)     (events vs profile)  (busy/work)     (HR ↔ active)
          │              │              │
          └─────── R = w1(1-A) + w2C + w3L + w4Z + w5H ─────┐
                                                            │
                                                            ▼
                                            POST /recommendations/generate
                                                            │
                                                            ▼
                                          ┌─────── AI Recommender ──────┐
                                          │                              │
                                          ▼                              ▼
                                  GigaChat (с кэшем)         RuleBased (fallback)
                                          │                              │
                                          └────────► Recommendation row ─┘
```

## Smart-notifier — поток

```
asynq scheduler @every 60min
        │
        ▼
   workers.handleNotificationSend
        │
        ▼
   SmartNotifier.Run()
        │
        ├──► HRRoadmap.Build() → critical/high
        │
        ▼ for each item
   ┌────────────────────────────┐
   │  recipientsFor(employee)   │  → [employee, manager, ALL hr]
   └────────────────────────────┘
        │
        ▼
   alreadySentRecently(24h)?
        │ no
        ▼
   NotificationService.Push
        │
        ├──► INSERT notifications
        └──► Redis PUBLISH ws:user:<id>
                         │
                         ▼
                /api/v1/notifications/stream (SSE)
                         │
                         ▼
                SvelteKit notificationsStore
```

## Технологии

- **Backend:** Go 1.26, Fiber v3, pgx v5, hibiken/asynq, redis v9, zerolog, golang-jwt v5, bcrypt
- **Crypto:** AES-256-GCM (encryption of OAuth tokens), SHA-256 (AI cache key)
- **Frontend:** SvelteKit 2, Svelte 5 (runes), TailwindCSS, Tabler Icons
- **Integrations:** emersion/go-webdav (CalDAV), arran4/golang-ical, teambition/rrule-go
- **AI:** GigaChat (Sber) с TLS-настройкой под сертификат Минцифры РФ
- **Realtime:** SSE (Server-Sent Events) поверх HTTP, Redis pub/sub для масштабирования

## Решения и trade-offs

| Решение | Альтернатива | Почему так |
|---|---|---|
| SSE | WebSocket | Fiber v3 (beta) не имеет стабильного WS-пакета. SSE покрывает 100% наших потребностей (только server→client пуш) |
| Модульный монолит | Микросервисы | Хакатон + один кодбейс + общая БД — быстрее доставка. Каждый `cmd/*` уже изолирован, мигрирует в микросервисы за 1-2 дня |
| Temporal work_profile | Audit-таблица | Активная запись valid_to IS NULL, история — старые строки. SQL запросы проще |
| Provider Registry interface | Жёсткая зависимость | Новый календарь добавляется одним методом RegisterCalendar() |
| AI fallback на rules | Только AI | Демо не падает, если GigaChat недоступен или ошибка валидации JSON |
| Rule-based smart-notifier | AI smart-notifier | На дне 7 — простой и понятный. AI-вариант — день 9-10 при наличии токена |
