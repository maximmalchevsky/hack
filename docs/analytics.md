# WorkTime Sync — формулы аналитики

Все метрики ∈ [0, 1]. Считаются в `internal/analytics`, кэшируются в Redis на 15 мин.

## A — Актуальность (Freshness)

```
A = max(0, 1 − d/D)

где:
  d — дни с last_profile_update_at
  D — порог (по умолчанию 90, конфигурируется через /admin/rules)
```

- `A = 1.0` — профиль свежий (обновлён сегодня)
- `A = 0.5` — обновлялся 45 дней назад
- `A = 0` — обновлялся ≥90 дней назад (или вообще не задан)

**Использование:**
- В диагностике: A ≥ 0.7 → "fresh", 0.5–0.7 → "needs_confirm", < 0.5 → "stale"
- В формуле R: вход `(1 − A)`

## C — Конфликты (Conflicts ratio)

```
C = M_out / M_all

где:
  M_all — все события сотрудника за период [now−30d; now+7d]
          (без cancelled, без is_excluded)
  M_out — события, которые НЕ попадают в рабочее окно work_profile
          (с учётом TZ)
```

- Если событие пересекает день (DTSTART < midnight, DTEND ≥ midnight) — считается конфликтом
- Если событие попадает в активное `time_exception` — НЕ считается конфликтом
- Если на день недели нет `DayHours` (выходной) — событие считается конфликтом

## L — Загрузка (Load)

```
L = H_busy / H_work

где:
  H_busy — сумма часов всех событий ≤ 8ч (фильтруем «весь день»),
           с merge'ом overlap'ов
  H_work — сумма рабочих часов профиля за период [now−30d; now+7d]
```

- Идём по каждому дню в TZ профиля
- Берём `DayHours.Start` и `DayHours.End`
- Складываем рабочие часы за период

**Threshold'ы:**
- `L > 0.8` → перегружен
- `L > 0.95` → критическая перегрузка
- `L > 1.5` → клампим в 1.5 (антивыбросы)

## Z — TZ-drift

```
Z = N_drift / N_total

где:
  N_total — события за период (без cancelled, без длинных > 8ч)
  N_drift — события, у которых:
            (a) event.Timezone != profile.Timezone
            (b) И локальный час начала в TZ профиля
                выходит за work_start − 1ч и work_end + 1ч
```

Признак того, что сотрудник реально живёт в другом часовом поясе, а в HR/профиле — старый.

## H — HR mismatch

Сравнение `employees.hr_work_format` ↔ `work_profile.work_format`:

| HR | profile | H |
|---|---|---|
| office | office | 0 |
| remote | remote | 0 |
| hybrid | * | 0 (hybrid толерантен) |
| office | remote | **1.0** (сильный mismatch) |
| remote | office | 0.6 |
| остальные | разные | 0.4 |

## R — Интегральный риск

```
R = w1·(1 − A) + w2·C + w3·L + w4·Z + w5·H

где:
  w1..w5 — настраиваемые веса, сумма = 1.0
  По умолчанию: w1=0.30, w2=0.25, w3=0.20, w4=0.15, w5=0.10
```

Конфигурируется через UI: `Admin → Rules → Веса`. Изменение сразу влияет на пересчёт
(после инвалидации кэша или истечения TTL).

### Интерпретация R

| R | Статус | Действие |
|---|---|---|
| `0 ≤ R ≤ 0.3` | низкий риск | — |
| `0.3 < R ≤ 0.6` | средний риск | проверить причины |
| `0.6 < R ≤ 0.85` | высокий риск | smart-notifier шлёт уведомления |
| `R > 0.85` | критический | блокировать новые встречи (TODO) |

## Командные метрики

### Team Availability

Команда из N сотрудников, период [пн; пт] × 11 часов (8:00–18:00).

```
Состояние клетки [day, hour]:
  - off       — клетка вне рабочих часов профиля
  - off       — клетка попадает в активное исключение
  - busy      — клетка пересекается с событием
  - conflict  — клетка с событием, НО вне рабочих часов
  - free      — клетка рабочая и без событий
```

### Find Meeting Window

```go
for t := alignTo30Min(now+1h); t < now+horizon; t += 30min {
    skip if hour < 8 || hour > 19 || saturday || sunday
    available := 0
    for each member of team {
        if memberAvailable(t, t+duration, member) {
            available++
        }
    }
    if available > 0 {
        candidates = append(candidates, {t, available})
    }
}
sort candidates: by available DESC, then by time ASC
return top-N
```

`memberAvailable` проверяет:
1. Слот целиком в рабочих часах профиля
2. Не пересекается с событиями
3. Не пересекается с исключениями
