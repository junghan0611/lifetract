---
name: lifetract
description: "Query personal life-tracking data: Samsung Health (sleep, steps, heart rate, stress, exercise) and aTimeLogger (time categories). Use when user asks about health metrics, sleep patterns, daily activity, time usage, or exercise history."
---

# lifetract - Life Tracking CLI

Query and analyze personal health and time-tracking data from Samsung Health exports and aTimeLogger.

## Prerequisites

Binary is bundled in the skill directory. Invoke via `{baseDir}/lifetract`.

## Commands

### Status check

```bash
{baseDir}/lifetract status
```

Shows data source availability, record counts, and date ranges.

### Today's summary

```bash
{baseDir}/lifetract today
```

Unified daily summary: steps, sleep, heart rate, stress, time categories.

### Timeline (denotecli-compatible)

```bash
{baseDir}/lifetract timeline --days 7
{baseDir}/lifetract timeline --days 30
```

Date-indexed unified view. Each entry keyed by `date: "YYYY-MM-DD"` — same format as denotecli journal entries. Use to correlate health/time data with notes on the same day.

### Sleep analysis

```bash
{baseDir}/lifetract sleep --days 7
{baseDir}/lifetract sleep --days 30 --summary
```

### Step counts

```bash
{baseDir}/lifetract steps --days 7
```

### Heart rate

```bash
{baseDir}/lifetract heart --days 7
```

### Stress levels

```bash
{baseDir}/lifetract stress --days 7
```

### Time tracking (aTimeLogger)

```bash
{baseDir}/lifetract time --days 7
{baseDir}/lifetract time --days 7 --category "Deep Work"
```

### Exercise sessions

```bash
{baseDir}/lifetract exercise --days 30
```

## Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--days N` | Days to look back | 7 |
| `--data-dir DIR` | Self-tracking data directory | `~/repos/gh/self-tracking-data` |
| `--summary` | Summary/aggregated mode | false |
| `--category CAT` | Filter by time category | all |

## Output

All output is JSON. Examples:

**status** returns data source info:
```json
{"samsung_health": {"path": "...", "csv_count": 77, "date_range": ["2021-01-21", "2025-10-06"]}, "atimelogger": {"path": "...", "available": true}}
```

**today** returns unified daily metrics:
```json
{"date": "2025-10-06", "steps": 8432, "sleep_hours": 7.2, "avg_hr": 72, "stress_avg": 35, "time_categories": [{"name": "Deep Work", "minutes": 180}]}
```

**sleep** returns session data:
```json
[{"date": "2025-10-05", "start": "23:30", "end": "06:42", "duration_hours": 7.2, "stages": {"deep": 85, "light": 210, "rem": 95, "awake": 22}}]
```

**steps** returns daily counts:
```json
[{"date": "2025-10-06", "steps": 8432}, {"date": "2025-10-05", "steps": 6210}]
```

**heart** returns daily HR stats:
```json
[{"date": "2025-10-06", "avg_hr": 72, "min_hr": 58, "max_hr": 135, "samples": 1440}]
```

**time** returns time category breakdown:
```json
[{"date": "2025-10-06", "categories": [{"name": "Deep Work", "minutes": 180}, {"name": "Reading", "minutes": 45}]}]
```

## Data Sources

| Source | Format | Period | Location |
|--------|--------|--------|----------|
| Samsung Health | CSV (77 files) | 2021-01 ~ 2025-10 | `self-tracking-data/samsunghealth_*/` |
| aTimeLogger | SQLite DB | ongoing | `self-tracking-data/atimelogger/database.db3` |

## Data Coverage

| Metric | Samsung Health CSV | Key Fields |
|--------|-------------------|------------|
| Sleep | `sleep_stage`, `sleep`, `sleep_combined` | stage, start/end time, duration |
| Steps | `pedometer_step_count`, `step_daily_trend` | count, date |
| Heart Rate | `tracker.heart_rate` | bpm, timestamp |
| Stress | `stress` | score, timestamp |
| Exercise | `exercise` | type, duration, calories |
| Weight | `health.weight` | kg, date |
| SpO2 | `respiratory_rate` | percentage |
