# lifetract

**Life + Traction — 삶의 정량 데이터를 AI 에이전트가 조회하는 CLI**

> Go · modernc.org/sqlite · Single static binary · JSON output · Korean-native

> **AI Agent Skill**: [pi-skills/lifetract](https://github.com/junghan0611/pi-skills/tree/main/lifetract) — 에이전트용 스킬 문서는 pi-skills 리포에서 관리합니다.

[![License](https://img.shields.io/badge/license-Apache%202.0-green.svg)](LICENSE)

---

## What This Does

Samsung Health CSV(8종) + aTimeLogger SQLite(18 카테고리)를 단일 SQLite DB(`lifetract.db`)로 통합.
AI 에이전트(Claude Code, pi 등)가 JSON으로 건강·시간 데이터를 즉시 조회.

모든 레코드에 Denote ID(`YYYYMMDDTHHMMSS`)를 부여 → [denotecli](https://github.com/junghan0611/denotecli)와 같은 시간축으로 교차 조회 가능.

```bash
lifetract import --exec             # CSV+aTimeLogger → lifetract.db (1.5초, 33MB)
lifetract today                     # 오늘 통합 요약
lifetract read 2025-10-04           # 특정 날짜 종합 (건강+시간추적)
lifetract timeline --days 30        # 30일 횡단 뷰
lifetract time --days 30            # aTimeLogger 시간 카테고리
```

---

## Install

```bash
git clone https://github.com/junghan0611/lifetract.git
cd lifetract
./run.sh build    # → ~/.local/bin/lifetract + pi-skills SKILL.md
```

Go 1.21+ 필요. Nix 사용자: `nix build` (CGO_ENABLED=0 정적 빌드).

---

## Architecture

```
lifetract import --exec
  Samsung Health CSVs (77개, 942MB) ─┐
  aTimeLogger SQLite (5MB) ──────────┼→ lifetract.db (33MB, 183,635 rows)
                                     │
lifetract <command>                  │
  DB 있음? → SQLite 쿼리 (~90ms) ────┘
  DB 없음? → CSV 직접 파싱 (~300ms, fallback)
```

### DB 테이블

| 테이블 | 소스 | 레코드 |
|--------|------|--------|
| `sleep` | Samsung Health | 4,212 |
| `sleep_stage` | Samsung Health | 71,245 |
| `heart_rate` | Samsung Health | 58,701 |
| `steps_daily` | Samsung Health | 9,227 |
| `stress` | Samsung Health | 23,627 |
| `exercise` | Samsung Health | 2,180 |
| `weight` | Samsung Health | 283 |
| `hrv` | Samsung Health | 1,058 |
| `atl_category` | aTimeLogger | 18 |
| `atl_interval` | aTimeLogger | 13,102 |

---

## Commands

| 커맨드 | 설명 |
|--------|------|
| `status` | 데이터 소스 + DB 상태 |
| `import [--exec]` | DB 생성 (dry-run / 실행) |
| `today` | 오늘 통합 요약 |
| `read <id>` | Denote ID로 조회 (일별/이벤트별) |
| `timeline [--days N]` | 날짜별 횡단 뷰 |
| `sleep [--days N] [--summary]` | 수면 분석 |
| `steps [--days N]` | 걸음 수 |
| `heart [--days N]` | 심박 추이 |
| `stress [--days N]` | 스트레스 |
| `exercise [--days N]` | 운동 세션 |
| `time [--days N] [--category X]` | aTimeLogger 시간 카테고리 |
| `export` | 공개용 내보내기 계획 |

### Flags

| Flag | Default | 설명 |
|------|---------|------|
| `--days N` | 7 | 조회 기간 |
| `--data-dir DIR` | `~/repos/gh/self-tracking-data` | 데이터 루트 |
| `--shealth-dir DIR` | 최신 자동감지 | Samsung Health 디렉토리 |
| `--summary` | false | 요약 모드 |
| `--category CAT` | 전체 | 시간 카테고리 필터 |
| `--exec` | false | import 실행 모드 |

---

## Denote ID System

```
Day level:   20251004T000000  ← denotecli 저널과 동일
Event level: 20251004T233000  ← 수면 시작, 운동 시작 등
```

교차 조회:
```bash
lifetract read 2025-10-04       # 그날 몸 상태 + 시간 사용
denotecli search "20251004"     # 그날 적은 노트/저널
```

---

## Data Sources

| 소스 | 포맷 | 기간 | 위치 |
|------|------|------|------|
| Samsung Health | CSV export (77개) | 2017-03 ~ 2025-10 | `self-tracking-data/samsunghealth_*/` |
| aTimeLogger | SQLite DB | 2021-10 ~ 2025-10 | `self-tracking-data/atimelogger/database.db3` |

### aTimeLogger 카테고리 (Indistractable 분류)

| 분류 | 카테고리 |
|------|----------|
| **Traction** | 본짓, 독서, 수행, 운동, 걷기, 셀프토크 |
| **Maintenance** | 수면, 낮잠, 식사, 준비, 집안일, 이동, 쇼핑 |
| **Distraction** | 딴짓, 유튜브, 짧은휴식, 여가활동 |
| **Family** | 가족 |

---

## Project Structure

```
lifetract/
├── run.sh              # Build + install + skill deploy
├── SKILL.md            # AI skill 정의 (pi-skills)
├── flake.nix           # Nix 패키징 (CGO_ENABLED=0)
├── docs/plan.md        # 로드맵
└── lifetract/          # Go source
    ├── main.go         # CLI 라우팅
    ├── config.go       # 설정
    ├── helpers.go      # 공유 유틸 (denoteID, 시간파서 등)
    ├── csv.go          # Samsung Health CSV 파서 + 레코드 타입
    ├── db.go           # SQLite 스키마 + 연결
    ├── db_query.go     # DB 쿼리 함수
    ├── import.go       # Import 매니페스트
    ├── import_exec.go  # CSV→DB 변환 로직
    ├── export.go       # 공개용 내보내기 + 카테고리 정책
    ├── query.go        # 조회 커맨드 (DB↔CSV 라우팅)
    ├── timeline.go     # 타임라인 빌더
    ├── read.go         # Denote ID 조회
    └── *_test.go       # 47 tests, 68% coverage
```

---

## Related Projects

| Project | 역할 |
|---------|------|
| [denotecli](https://github.com/junghan0611/denotecli) | 정성 데이터 (노트/저널 3,000+) — 같은 Denote ID 축 |
| [self-tracking-data](https://github.com/junghan0611/self-tracking-data) | 원본 데이터 저장소 |

---

**Author**: [@junghanacs](https://github.com/junghan0611) · Apache 2.0
