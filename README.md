# lifetract

**Life tracking CLI for AI agents: Samsung Health + aTimeLogger unified query**

> Go + modernc.org/sqlite. Single binary. JSON output. Korean-native.

[![License](https://img.shields.io/badge/license-Apache%202.0-green.svg)](LICENSE)

---

## What This Does

CLI tool that gives AI agents (Claude Code, pi, etc.) structured access to personal life-tracking data. Parses Samsung Health CSV exports and aTimeLogger SQLite DB. Returns JSON.

```
./run.sh build                    # Build + install to ~/.local/bin
lifetract status                  # DB/data status overview
lifetract today                   # 건강 + 시간 통합 요약
lifetract sleep --days 7          # 수면 분석
lifetract steps --days 7          # 걸음 수
lifetract heart --days 7          # 심박 추이
lifetract stress --days 7         # 스트레스 추이
lifetract time --days 7           # 시간 카테고리 분석 (aTimeLogger)
lifetract exercise --days 30      # 운동 세션
```

---

## Install

```bash
git clone https://github.com/junghan0611/lifetract.git
cd lifetract
./run.sh build    # Builds binary → ~/.local/bin/lifetract
```

Requires Go 1.21+.

---

## Data Sources

### Samsung Health CSV Export (수동 내보내기)

```
~/repos/gh/self-tracking-data/samsunghealth_gtgkjh_20251006104703/
├── com.samsung.health.sleep_stage.*.csv        # 수면 스테이지
├── com.samsung.shealth.sleep.*.csv             # 수면 세션
├── com.samsung.shealth.tracker.heart_rate.*.csv # 심박
├── com.samsung.shealth.tracker.pedometer_step_count.*.csv # 걸음
├── com.samsung.shealth.stress.*.csv            # 스트레스
├── com.samsung.shealth.exercise.*.csv          # 운동
├── com.samsung.health.weight.*.csv             # 체중
└── ... (77 CSV files total)
```

- Period: 2021-01 ~ 2025-10
- Format: CSV with BOM, comma-separated

### aTimeLogger SQLite DB

```
~/repos/gh/self-tracking-data/atimelogger/database.db3
```

- Format: SQLite 3.x (5MB)
- Content: Time tracking categories and intervals

---

## Commands

### status

```bash
lifetract status
```

Shows data source availability, record counts, date ranges.

### today

```bash
lifetract today
```

Unified daily summary: steps, sleep, heart rate, stress, time categories.

### sleep

```bash
lifetract sleep --days 7
lifetract sleep --days 30 --summary
```

### steps

```bash
lifetract steps --days 7
lifetract steps --days 30
```

### heart

```bash
lifetract heart --days 7
```

### stress

```bash
lifetract stress --days 7
```

### time

```bash
lifetract time --days 7
lifetract time --days 7 --category "Deep Work"
```

### exercise

```bash
lifetract exercise --days 30
```

---

## Output

All output is JSON:

```json
// status
{"samsung_health": {"path": "...", "csv_count": 77, "date_range": ["2021-01-21", "2025-10-06"]},
 "atimelogger": {"path": "...", "size_mb": 5.0, "available": true}}

// today
{"date": "2025-10-06", "steps": 8432, "sleep_hours": 7.2, "avg_hr": 72, "stress_avg": 35,
 "time_categories": [{"name": "Deep Work", "minutes": 180}, ...]}

// sleep
[{"date": "2025-10-05", "start": "23:30", "end": "06:42", "duration_hours": 7.2,
  "stages": {"deep": 85, "light": 210, "rem": 95, "awake": 22}}]
```

---

## Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--days N` | Days to look back | 7 |
| `--data-dir DIR` | Self-tracking data directory | `~/repos/gh/self-tracking-data` |
| `--summary` | Summary mode (aggregated) | false |
| `--category CAT` | Filter by time category | all |

---

## As an AI Skill

### pi / Claude Code

```bash
# Option 1: Build and use
./run.sh build
lifetract today

# Option 2: Via pi-skills (pre-installed)
# See https://github.com/junghan0611/pi-skills
```

See [SKILL.md](SKILL.md) for full skill documentation.

---

## Project Structure

```
lifetract/
├── run.sh                 # Build + install entry point
├── SKILL.md               # AI skill definition (pi-skills compatible)
├── lifetract/
│   ├── go.mod             # Go module
│   ├── main.go            # CLI routing
│   ├── config.go          # Configuration + paths
│   ├── shealth.go         # Samsung Health CSV parser
│   ├── shealth_test.go    # Samsung Health tests
│   ├── atimelogger.go     # aTimeLogger SQLite parser
│   ├── atimelogger_test.go
│   └── output.go          # JSON output formatting
└── docs/                  # Design docs
```

---

## Related Projects

| Project | Description |
|---------|-------------|
| [denotecli](https://github.com/junghan0611/denotecli) | Denote knowledge base CLI (sister project — same architecture) |
| [pi-skills](https://github.com/junghan0611/pi-skills) | AI skill collection for Claude Code |
| [self-tracking-data](https://github.com/junghan0611/self-tracking-data) | Raw data source |
| [samsung-health-skill](https://github.com/MudgesBot/samsung-health-skill) | Python reference (Health Connect SQLite) |

---

**Author**: [@junghanacs](https://github.com/junghan0611)

## License

Apache 2.0
