# API

Everything is under `/api/v1`, JSON in and out. Authenticate with a session cookie (the UI), a personal token (`Authorization: Bearer ph_…`, Settings → API & shortcuts), a Pocket Core device/user token, or a Pocket Core link token (`pcx_…`, scopes below). Dates are `YYYY-MM-DD`; times are RFC 3339 with an offset.

## Pushing data in

### `POST /ingest/hae` — Health Auto Export

The body the iOS app *Health Auto Export* sends from a "REST API" automation (`{"data":{"metrics":[…],"workouts":[…]}}`). Every metric it offers is accepted; known ones are mapped to the catalog and converted to canonical units, unknown ones are kept under their own name. `heart_rate` rows with `Avg/Min/Max`, `blood_pressure` with `systolic/diastolic` and `sleep_analysis` (one row per night with the stages) are understood. Workouts carry distance, energy, heart rate, elevation and weather. A window sent twice updates the earlier samples (unique per metric, start time, source and device).

Returns `{"samples": n, "workouts": n, "sleep": n, "skipped": n, "unknown": [...], "days": n}`.

### `POST /ingest` — Pocket Health format

```json
{
  "samples":  [{"metric": "weight", "at": "2025-10-23T07:30:00+02:00", "value": 80.4, "unit": "kg", "device": "Withings", "note": ""}],
  "workouts": [{"kind": "running", "start": "2025-10-23T07:00:00+02:00", "end": "2025-10-23T07:35:00+02:00", "distance_m": 5200, "energy_kcal": 310, "avg_hr": 151, "max_hr": 171, "elevation_m": 42}],
  "sleep":    [{"start": "2025-10-22T23:10:00+02:00", "end": "2025-10-23T06:40:00+02:00", "asleep_min": 408, "deep_min": 66, "rem_min": 84, "core_min": 258, "awake_min": 42}]
}
```

`metric` is a catalog key (`GET /catalog`), a Health Auto Export name or an `HKQuantityTypeIdentifier…`. `unit` is optional and converted (`lb`, `mi`, `°F`, `mmol/L`, `fl oz`…). `?source=myapp` names the source (default `api`; a link token's app slug when through Pocket Core). Pair metrics (blood pressure) take `value` and `value2`. A sample that stands for a stretch of time (an hour's average or sum) may carry `end`, and an average its `min` and `max`. A night sent again from the same source with a different start replaces the earlier copy. The Pocket Homelab app pushes here with `?source=pocket-homelab`.

### `POST /samples` — one reading

`{"metric": "water", "value": 250, "unit": "mL", "at": "…", "note": ""}` → 201 with the sample. `PUT /samples/{id}` (value, value2, at, note), `DELETE /samples/{id}`.

### `POST /workouts`, `POST /sleep`

Same fields as above, one at a time. `PUT/DELETE /workouts/{id}`, `POST /workouts/{id}/habit {"habit_id": n}` ticks a Pocket Habits habit for it, `GET /workouts/{id}/route[?fresh=1]` is the route recorded by Pocket Movement between start and end (`points`, `distance_m`, `bounds`; cached a day), `POST /workouts/{id}/route/apply` copies that distance onto the workout, `DELETE /sleep/{id}`.

### Imports

- `POST /import/apple` — multipart `file` = Apple Health `export.zip` or `export.xml`, optional `since` (date). Returns `202` with a job; poll `GET /imports/{id}` (`status` running/done/error, `progress`, counts).
- `POST /import/csv?metric=weight&source=scale` — CSV body or multipart `file`; columns `date,value[,unit][,note]` or `date,metric,value[,unit]` (header optional).
- `POST /import` — a JSON export (below) merged into the caller's data.
- `GET /sources` — last write per source, devices seen, recent imports. `DELETE /sources/{source}` removes everything a source sent. `POST /rollup` recomputes every day.

## Reading

- `GET /catalog` — metrics (key, name, unit, imperial unit, aggregation, category, icon, colour), categories, workout kinds, the manual ones.
- `GET /metrics[?all=1]` — the person's metrics with data: days, first/last, latest value, 7-day sparkline, change vs last week, goal.
- `GET /metrics/{key}?range=w|m|3m|6m|y|all[&to=date]` — daily points, weekly averages, stats (avg/min/max/sum, change vs the previous period, trend per week for last-value metrics), goal + days met + streaks, latest, recent samples, weekday pattern.
- `GET /metrics/{key}/samples?from&to&limit` — raw samples.
- `GET /home?date=` — the Today page. `GET /dashboard` — a few numbers for a Homepage widget (`steps`, `active_kcal`, `exercise_min`, `sleep_text`, `resting_hr`, `weight_text`, `workouts_week`, `goals_text`, `meds_due`).
- `GET /day/{date}` (also `/summary/{date}`) — one day: every metric's number, workouts, nights, goals met, a one-line summary; medications and symptoms only for the person themselves (never for a link).
- `GET /overlay?from&to` — workouts (timed) and nights (all-day) as items for Pocket Calendar.
- `GET /workouts?from&to&kind&limit&offset`, `GET /workouts/{id}`, `GET /workouts/stats?range=`, `GET /workouts/kinds`.
- `GET /sleep?range=|from&to&limit`.
- `GET /goals` — with today's value, percent, met, streaks, days met of the last 30. `PUT /goals/{metric} {"target", "direction": "at_least"|"at_most", "active"}`, `DELETE /goals/{metric}`.
- `GET /medications[?all=1]`, `POST /medications`, `PUT/DELETE /medications/{id}`, `GET /medications/day/{date}` (slots with status), `POST /medications/{id}/log {"date","slot","status":"taken"|"skipped","taken_at","note"}` (`slot` empty = an extra dose), `DELETE /medications/log/{id}`, `GET /medications/log?from&to`.
- `GET /symptoms?range|from&to&limit`, `POST /symptoms {"name","severity":1-5,"at","duration_min","note","tags"}`, `PUT/DELETE /symptoms/{id}`, `GET /symptoms/names`.
- `GET /rules`, `POST /rules {"trigger":"workout"|"goal","match":"any"|kind|metric,"habit_id","habit_name"}`, `PUT/DELETE /rules/{id}`, `GET /habits/list` (habits from Pocket Habits through the connection in effect), `POST /habits/mark-now`.
- `GET /insights?range=` — trends, weekday patterns, workouts, sleep regularity, correlations, weather split, goals.
- `GET /weather?from&to`, `POST /weather/refresh`, `GET /weather/geocode?q=`.
- `POST /ai/recap {"range"}`, `POST /ai/ask {"question","range"}`, `GET /ai/status`, `GET /ai/models`.
- `GET /export.json`, `GET /export.csv` (one row per day, one column per metric), `GET /backup` (admin).

## Pocket Core links

Health offers `health:read` (metrics, summaries, workouts, nights, goals, the day and overlay endpoints) and `health:write` (`POST /ingest`, `POST /samples`, `POST /workouts`; a linked app may change only what it wrote). Health uses Pocket Habits (`habits:read`, `entries:write`), Pocket Movement (`movement:points`) and Pocket AI (`inference:write`).
