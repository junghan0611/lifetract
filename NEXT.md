# NEXT.md — 다음 할 일

운영 baseline 은 [AGENTS.md](AGENTS.md). 결정의 의미축은 [docs/plan.md](docs/plan.md) + [[denote:20260517T211731]] (botlog). 본 NEXT 는 다음 한 걸음.

---

## 1. SSOT 첫 정기 갱신 (2026-05-19)

Phase 6 (HA 라이브 + Samsung CSV SSOT) 가 정착된 첫 turn. lifetract.db = 198,547 rows, Samsung CSV → 2026-05-18. HA 라이브 (5/17~) 와 5/17 자리에서 겹쳐 시간축 단절 없음.

검증된 자리:
- [x] `lifetract sleep --days 30 --summary` — 29일, 68 sessions, avg 3.1h (낮잠 포함)
- [x] `lifetract read 2026-04-15` (갭 한복판) — health + time + sleep_sessions 모두 답
- [x] `lifetract read 2026-05-17` — DB 가 답 (Samsung 마지막 sleep 5/17 12:53)
- [x] `lifetract ha history sleep_duration --days 7` — 두 entries (5/17, 5/18 새벽)

남은 자리:
- [x] **today 의 health 일부 빔** — 2026-05-26: Phase 7 read-only fallback 으로 자동 해결. DB miss 시 HA `GetState` (steps/heart_rate) 가 채움. `source: "db+ha"`, `ha_sources` 로 자리 노출.
- [x] **today.sleep_hours 가 옛 row 잡는 자리** — 2026-05-26: `todaySleepStale` 이 DB 의 가장 최근 sleep date 가 today/yesterday 가 아니면 stale 로 보고, HA history (최근 36h sleep_duration 합산, main + nap) 로 덮어씀.
- [ ] **DB epoch-0 잡음** — `heart_rate.min(start_time) = 1970-01-01` row 1건. import 시 invalid timestamp filter 자리.
- [ ] **HA → DB lazy upsert (Phase 7 후반부)** — read-only fallback 이 매번 HA 를 때리는 자리 → `source TEXT` 컬럼 + `(date, source)` upsert 로 적립. offline 모드 보장.

## 2. 새 데이터 schema 확장 (Galaxy S26 영향)

이번 export 에 *기존 import 가 모르는 새 파일* 들이 들어옴 — silent skip 중. 의미 있는 자리만 schema 확장:

- [ ] **sleep 새 파일군** — `sleep_data`, `sleep_combined`, `sleep_raw_data`, `sleep_snoring`. 본 SSOT 의 sleep stages 자리 (HA 가 못 주는 자리) 가 본 쪽에서 더 풍부해질 가능성
- [ ] **호흡수** `com.samsung.health.respiratory_rate` — HA 측에도 `respiratory_rate` sensor 있어 두 source 가 연결되는 자리
- [ ] **혈중산소** `com.samsung.shealth.tracker.oxygen_saturation`

## 3. AGENTS.md / README 영속화 (남은 자리)

- [ ] AGENTS.md §2 "두 입력 스트림" → "세 입력 스트림" (HA REST 라이브 인터페이스 추가)
- [ ] AGENTS.md §5 Operational workflow 에 HA 호출 절차 한 줄
- [ ] README architecture 다이어그램에 HA 입력 화살표

## 4. aTimeLogger 자동 갱신 (TODO)

현재는 폰 → backup → 수동 cp. 자동 동기화 자리:
- 폰의 aTimeLogger pro 가 cloud sync 지원하는지 확인
- 아니면 폰 → 호스트 자동 push (Syncthing/rsync) 후 `./run.sh update` cron

## 5. Phase 7 — HA → DB lazy ingest (진행 중)

- 2026-05-26: **read-only fallback** 완료. `today` / `read <오늘>` 이 DB 빈 자리를 HA 라이브 값으로 자동 채움. 에이전트가 punchout / recall / day-query 어디서 lifetract 를 부르든 *life 정보를 무시하고 넘어가지 않는다*. (1번 자리에 이미 [x] 표시)
- **후반부**: DB upsert (source 컬럼 + (date,source) upsert) — read-only fallback 이 매번 HA 를 때리는 자리를 적립으로 닫음. offline 보장. plan.md Phase 7 후반 자리.

## 6. Cross-repo

- [ ] [pi-skills/lifetract](https://github.com/junghan0611/pi-skills/tree/main/lifetract) — SKILL.md 본 갱신 반영 (symlink, push 만)
- [ ] [homeagent-config](https://github.com/junghan0611/homeagent-config) — HA dogfooding

---

본 도구의 책임은 *데이터가 어떻게 흘러나가는가* 다. 2026-05-19 — SSOT 첫 정기 갱신이 정착됐다. "한달치 수면 물어봐도 답할 수 있다." 다음 자리는 *새 sensor schema 확장* 또는 *오늘 자리 채우는 phase 7*.
