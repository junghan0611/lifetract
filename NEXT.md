# NEXT.md — 다음 할 일

운영 baseline 은 [AGENTS.md](AGENTS.md). 결정의 의미축은 [docs/plan.md](docs/plan.md) + [[denote:20260517T211731]] (botlog). 본 NEXT 는 다음 한 걸음.

---

## 닫힌 자리 (최근 turn)

- [x] **Phase 7 read-only fallback** (2026-05-26) — `today` / `read <오늘>` 이 DB miss/stale 자리를 HA `GetState` (steps/heart_rate) + `GetHistory` (sleep_duration 최근 36h 합산) 로 자동 채움. `source: "db+ha"`, `ha_sources` 자리 노출. 에이전트가 lifetract 부를 때 *life 정보를 무시하고 넘어가는 자리* 닫힘.
- [x] **today.sleep_hours 가 옛 row 잡는 자리** (2026-05-26) — `todaySleepStale` heuristic 으로 DB 최근 sleep date ≠ today/yesterday 면 stale 판정 + HA 로 덮어씀.
- [x] **AGENTS.md "세 입력 스트림"** (2026-05-26) — §2 / §5 갱신. HA REST 라이브 인터페이스 명시 + Operational workflow 한 줄.

## 다음 한 걸음 후보 (시급순)

### A. 새 sleep 파일군 schema 확장 (중)

Galaxy S26 export 에 `sleep_data`, `sleep_combined`, `sleep_raw_data`, `sleep_snoring` 신규 — 현재 silent skip. sleep stages 자리 (HA 가 못 주는 자리) 가 본 쪽에서 풍부해질 가능성. `import_exec.go` + `db.go` schema 두 자리.

### B. aTimeLogger 자동 동기화 (중)

현재는 폰 → backup → 수동 cp. AGENTS.md gotcha 의 "사람이 손대지 않아도 흐르는 자리만 살아남는다" 자리. 옵션:
- 폰의 aTimeLogger pro 가 cloud sync 지원하는지 확인
- 폰 → 호스트 자동 push (Syncthing/rsync) 후 `./run.sh update` cron

### C. 새 sensor schema 확장 (낮음)

- `com.samsung.health.respiratory_rate` — HA 측에도 `respiratory_rate` sensor 있어 두 source 가 연결되는 자리
- `com.samsung.shealth.tracker.oxygen_saturation`

### D. Phase 7 후반부 — HA → DB lazy upsert (낮음)

read-only fallback 이 사용자 가시 자리는 다 채움. *언젠가* 의 자리:
- 기존 테이블 + `source TEXT` 컬럼 (`samsung_csv` / `ha_rest`)
- `(date, source)` upsert
- offline 모드 보장 (HA 못 닿아도 같은 답)

### E. 잡 정리 (낮음)

- DB epoch-0 잡음 — `heart_rate.min(start_time) = 1970-01-01` row 1건. import 시 invalid timestamp filter 자리.
- README architecture 다이어그램에 HA 입력 화살표
- pi-skills/lifetract — SKILL.md 본 갱신 반영 (symlink, push 만)
- [homeagent-config](https://github.com/junghan0611/homeagent-config) — HA dogfooding

---

다음에 부르면: **A (새 sleep 파일군)** 가 의미축이 가장 살아 있는 자리. sleep stages 는 HA 가 못 주는 자리고, 본 SSOT 에서 풍부해진다.

본 도구의 책임은 *데이터가 어떻게 흘러나가는가* 다. 2026-05-26 — phase 7 read-only fallback 정착. "에이전트가 부르면 항상 답한다." 다음 자리는 *입력 source 의 풍부함* (A) 또는 *흐름의 자동화* (B).
