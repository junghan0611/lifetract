---
name: lifetract
description: "Query personal life-tracking data: Samsung Health (sleep, steps, heart rate, stress, exercise) and aTimeLogger (time categories). Use when user asks about health metrics, sleep patterns, daily activity, time usage, exercise history, or needs to correlate health data with notes/journals."
---

# lifetract - Life Tracking CLI

Query and analyze personal health and time-tracking data. All records carry Denote IDs (`YYYYMMDDTHHMMSS`) for cross-referencing with denotecli.

## Prerequisites

Binary is bundled in the skill directory. Invoke via `{baseDir}/lifetract`.
Static binary (CGO_ENABLED=0). No external dependencies at runtime.

## Commands

### Status check

```bash
{baseDir}/lifetract status
```

Shows data source availability, record counts, detected export directories.

### Read by Denote ID

```bash
{baseDir}/lifetract read 20250115T000000              # Day summary by Denote Day ID
{baseDir}/lifetract read 2025-01-15                    # Same (date shorthand)
{baseDir}/lifetract read 20250115T233000               # Specific sleep session
{baseDir}/lifetract read 20250115T073000               # Specific exercise event
```

Returns the matching day timeline or individual event. Day reads include all health metrics + sleep sessions + exercise for that date.

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

Date-indexed unified view. Each entry keyed by Denote Day ID (`YYYYMMDDT000000`) and `date: "YYYY-MM-DD"` — same format as denotecli journal entries.

**Cross-referencing pattern:**
```bash
# What you wrote on that day
denotecli search "2025-01-15" --dirs ~/org
# How your body was on that day
{baseDir}/lifetract read 20250115T000000
```

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

### Exercise sessions

```bash
{baseDir}/lifetract exercise --days 30
```

### Time tracking (aTimeLogger)

```bash
{baseDir}/lifetract time --days 7
{baseDir}/lifetract time --days 7 --category "Deep Work"
```

> Note: aTimeLogger SQLite parser is Phase 2 (stub). Currently returns status info.

## Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--days N` | Days to look back | 7 |
| `--data-dir DIR` | Self-tracking data directory | `~/repos/gh/self-tracking-data` |
| `--shealth-dir DIR` | Specific Samsung Health export dir | latest auto-detected |
| `--summary` | Summary/aggregated mode | false |
| `--category CAT` | Filter time category | all |

## Denote ID System

All records carry Denote-compatible identifiers for unified timeline:

| Level | ID Format | Example | Usage |
|-------|-----------|---------|-------|
| **Day** | `YYYYMMDDT000000` | `20250115T000000` | Same as denotecli journal ID |
| **Event** | `YYYYMMDDTHHMMSS` | `20250115T233000` | Sleep session, exercise event |

## Output

All output is JSON.

**status:**
```json
{"samsung_health": {"path": "...", "available": true, "csv_count": 77, "all_dirs": ["...v1", "...v2"]}, "atimelogger": {"path": "...", "available": true, "size_mb": 5.0}}
```

**read (day):**
```json
{"id": "20250115T000000", "date": "2025-01-15", "health": {"steps": 8432, "sleep_hours": 7.2, "sleep_score": 85, "avg_hr": 74, "min_hr": 65, "max_hr": 85, "stress_avg": 53.5}, "exercise": [{"type": "Walking", "minutes": 30}], "sleep_sessions": [{"id": "20250115T233000", "date": "2025-01-15", "start": "23:30", "end": "06:42", "duration_hours": 7.2, "stages": {"deep_min": 85, "light_min": 165, "rem_min": 90, "awake_min": 12}}]}
```

**read (event):**
```json
{"id": "20250115T233000", "date": "2025-01-15", "start": "23:30", "end": "06:42", "duration_hours": 7.2, "sleep_score": 85, "efficiency": 92.5, "stages": {"deep_min": 85, "light_min": 165, "rem_min": 90, "awake_min": 12}}
```

**timeline:**
```json
[{"id": "20250115T000000", "date": "2025-01-15", "health": {"steps": 8432, "sleep_hours": 7.2, "avg_hr": 74, "stress_avg": 53.5}, "exercise": [{"type": "Walking", "minutes": 30}]}]
```

**sleep:**
```json
[{"id": "20250115T233000", "date": "2025-01-15", "start": "23:30", "end": "06:42", "duration_hours": 7.2, "sleep_score": 85, "stages": {"deep_min": 85, "light_min": 165, "rem_min": 90, "awake_min": 12}}]
```

**steps:**
```json
[{"id": "20250115T000000", "date": "2025-01-15", "steps": 8432}]
```

**heart:**
```json
[{"id": "20250115T000000", "date": "2025-01-15", "avg_hr": 74, "min_hr": 65, "max_hr": 85, "samples": 3}]
```

**stress:**
```json
[{"id": "20250115T000000", "date": "2025-01-15", "avg_score": 53.5, "min_score": 45, "max_score": 62, "samples": 2}]
```

**exercise:**
```json
[{"id": "20250115T073000", "date": "2025-01-15", "type": "Walking", "duration_minutes": 30, "calories": 120.5, "avg_hr": 110.5, "max_hr": 145}]
```

## Data Sources

| Source | Format | Period | Location |
|--------|--------|--------|----------|
| Samsung Health | CSV exports | 2017-12 ~ 2025-10 | `self-tracking-data/samsunghealth_*/` |
| aTimeLogger | SQLite DB | ongoing | `self-tracking-data/atimelogger/database.db3` |

### Multiple Export Versions

Samsung Health exports are timestamped directories:
```
self-tracking-data/
├── samsunghealth_user_20251006104703/   # v1 (latest = default)
├── samsunghealth_user_20260222120000/   # v2 (if added later)
└── atimelogger/database.db3
```

- **Default**: Uses latest directory (alphabetically last)
- **Explicit**: `--shealth-dir path/to/specific/export`
- **Status**: `lifetract status` shows all detected directories

## Data Coverage

| Metric | CSV Pattern | Key Fields | Records |
|--------|-------------|------------|---------|
| Sleep | `shealth.sleep`, `health.sleep_stage` | stage, start/end, duration, score | ~4,000 |
| Steps | `shealth.step_daily_trend`, `tracker.pedometer_step_count` | count, date | ~2,800 days |
| Heart Rate | `tracker.heart_rate` | bpm, timestamp | ~2,600 days |
| Stress | `shealth.stress` | score, timestamp | ~1,700 days |
| Exercise | `shealth.exercise` | type, duration, calories, HR | ~2,000 |
| Weight | `health.weight` | kg, date | — |
| SpO2 | `respiratory_rate` | percentage | — |

## Environment Paths

| Environment | Data Path | Example |
|-------------|-----------|---------|
| **Local** (NixOS/Claude Code) | `~/repos/gh/self-tracking-data` | `lifetract timeline --days 7` |
| **Container** (Docker/OpenClaw) | mount or `--data-dir` | `lifetract status --data-dir /data/tracking` |

## Related Skills

| Skill | Cross-Reference |
|-------|----------------|
| **denotecli** | Same Denote ID axis — notes/journals for the same dates |
| **gogcli** | Google Calendar events for the same dates |
| **bibcli** | Research references linked to journal entries |
