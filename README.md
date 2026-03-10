# lifetract

**Life + Traction — A CLI that lets AI agents query your health and time-tracking data.**

> Go · modernc.org/sqlite · Single static binary · JSON output

> **AI Agent Skill**: [pi-skills/lifetract](https://github.com/junghan0611/pi-skills/tree/main/lifetract)

[![License](https://img.shields.io/badge/license-Apache%202.0-green.svg)](LICENSE)

---

## What This Does

Unifies **Samsung Health** CSV exports (8 types) and **aTimeLogger** SQLite (18 categories) into a single SQLite database (`lifetract.db`). AI agents query health and time data via JSON — sleep, heart rate, steps, stress, exercise, weight, HRV, and how you spend your hours.

Every record carries a [Denote ID](https://protesilaos.com/emacs/denote) (`YYYYMMDDTHHMMSS`), enabling cross-referencing with [denotecli](https://github.com/junghan0611/denotecli) on the same time axis.

Samsung Health and aTimeLogger are widely-used apps with standard export formats. **lifetract is a general-purpose tool** — anyone using these apps can use it. Data itself is private; public dataset is planned separately.

```bash
./run.sh update                     # Place new data + rebuild DB (one step)
lifetract today                     # Today's unified summary
lifetract read 2025-10-04           # Full day: health + time tracking
lifetract timeline --days 30        # 30-day cross-sectional view
lifetract time --days 30            # Time category analysis
```

---

## Install

```bash
git clone https://github.com/junghan0611/lifetract.git
cd lifetract
./run.sh build    # → ~/.local/bin/lifetract
```

Requires Go 1.21+. Nix users: `nix build` (CGO_ENABLED=0 static binary).

### Updating Data

Export Samsung Health + aTimeLogger backup from your phone into a date folder, then one command:

```bash
# Put exports in self-tracking-data/YYYYMMDD/, then:
./run.sh update
```

Automatically: moves Samsung Health folder → replaces aTimeLogger DB → rebuilds `lifetract.db`.

---

## Architecture

```
./run.sh update (or lifetract import --exec)
  Samsung Health CSVs ────────────┐
  aTimeLogger SQLite ─────────────┼→ lifetract.db → JSON API
                                  │
lifetract <command>               │
  DB exists? → SQLite query (~90ms)
  No DB?     → CSV fallback (~300ms)
```

### DB Tables

| Table | Source | Rows |
|-------|--------|------|
| `sleep` | Samsung Health | 4,489 |
| `sleep_stage` | Samsung Health | 78,591 |
| `heart_rate` | Samsung Health | 62,036 |
| `steps_daily` | Samsung Health | 9,692 |
| `stress` | Samsung Health | 25,768 |
| `exercise` | Samsung Health | 2,195 |
| `weight` | Samsung Health | 283 |
| `hrv` | Samsung Health | 1,058 |
| `atl_category` | aTimeLogger | 18 |
| `atl_interval` | aTimeLogger | 13,918 |

**Total: 198,030 rows, 36MB** (as of 2026-03-10)

---

## Commands

| Command | Description |
|---------|-------------|
| `status` | Data source availability and stats |
| `import [--exec]` | Build DB (dry-run / execute) |
| `today` | Today's unified health + time summary |
| `read <id>` | Query by Denote ID (day or event level) |
| `timeline [--days N]` | Date-indexed cross-sectional view |
| `sleep [--days N] [--summary]` | Sleep session analysis |
| `steps [--days N]` | Daily step counts |
| `heart [--days N]` | Heart rate trends |
| `stress [--days N]` | Stress levels |
| `exercise [--days N]` | Exercise sessions |
| `time [--days N] [--category X]` | Time category analysis (aTimeLogger) |
| `export` | Public-safe export plan |

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--days N` | 7 | Query period |
| `--data-dir DIR` | `~/repos/gh/self-tracking-data` | Data root |
| `--shealth-dir DIR` | Auto-detect latest | Samsung Health directory |
| `--summary` | false | Summary mode |
| `--category CAT` | all | Time category filter |
| `--exec` | false | Execute mode (for import) |

---

## Denote ID System

```
Day level:   20251004T000000  ← same key as denotecli journals
Event level: 20251004T233000  ← individual sleep/exercise events
```

Cross-reference:
```bash
lifetract read 2025-10-04       # Body state + time usage that day
denotecli search "20251004"     # Notes and journal entries that day
```

---

## Data Sources

| Source | Format | Period | Location |
|--------|--------|--------|----------|
| Samsung Health | CSV export | 2017-03 – present | `self-tracking-data/samsunghealth_*/` |
| aTimeLogger | SQLite DB | 2021-10 – present | `self-tracking-data/atimelogger/database.db3` |

### aTimeLogger Categories (Indistractable Framework)

| Class | Categories |
|-------|------------|
| **Traction** | Deep work, Reading, Practice, Exercise, Walking, Self-talk |
| **Maintenance** | Sleep, Nap, Meals, Prep, Chores, Commute, Shopping |
| **Distraction** | Distraction, YouTube, Short break, Leisure |
| **Family** | Family |

---

## Project Structure

```
lifetract/
├── run.sh              # Build, test, update
├── flake.nix           # Nix packaging (CGO_ENABLED=0)
├── docs/plan.md        # Roadmap
└── lifetract/          # Go source
    ├── main.go         # CLI routing
    ├── config.go       # Configuration + auto-detect
    ├── helpers.go      # Shared utils (Denote ID, time parsers)
    ├── csv.go          # Samsung Health CSV parser + record types
    ├── db.go           # SQLite schema + connection
    ├── db_query.go     # DB query functions
    ├── import.go       # Import manifest
    ├── import_exec.go  # CSV→DB conversion
    ├── export.go       # Public-safe export + category policy
    ├── query.go        # Query commands (DB↔CSV routing)
    ├── timeline.go     # Timeline builder
    ├── read.go         # Denote ID lookup
    └── *_test.go       # 47 tests, 68% coverage
```

---

## Related Projects

| Project | Role |
|---------|------|
| [denotecli](https://github.com/junghan0611/denotecli) | Qualitative data (3,000+ notes/journals) — same Denote ID axis |
| [self-tracking-data](https://github.com/junghan0611/self-tracking-data) | Raw data repository |

---

## Data Update Log

| Date | Total Rows | DB Size | Samsung Health | aTimeLogger | Notes |
|------|------------|---------|----------------|-------------|-------|
| 2025-10-06 | 183,635 | 33MB | ~2025-10 (77 CSVs) | ~2025-10 (13,102 intervals) | Initial build |
| 2026-03-10 | 198,030 | 36MB | ~2026-03 (78 CSVs) | ~2026-03 (13,918 intervals) | +14,395 rows, 5 months added |

---

**Author**: [@junghanacs](https://github.com/junghan0611) · Apache 2.0
