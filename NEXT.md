# NEXT.md — 다음 할 일

운영 baseline 은 [AGENTS.md](AGENTS.md). 후속/미완 검증은 여기에. 끝난 항목 지우고, 새로 발견한 후속 추가. 영속할 사실은 AGENTS.md / [docs/plan.md](docs/plan.md) / commit history 로 옮긴다.

오늘의 의미축은 [[denote:20260517T211731]] (botlog) 에 보존되어 있다 — 데이터를 옮기는 일을 멈추고, 인터페이스가 데이터를 만나러 오게 한 날.

---

## 1. Home Assistant REST → Lazy Ingest (Phase 6 재정의)

**2026-05-17 — Oracle Docker 에 HA 띄움 ([nixos-config](../nixos-config/) `53a8d2e`), Companion App 등록 (디바이스 `SM-S942N-S26-GLGMAN`, mobile_app `b6a48768e1c2b22a`), Health Connect 16개 센서 활성, PoC 통과: `sensor.sm_s942n_s26_glgman_sleep_duration = 427 min` (어젯밤 7h7m).**

**2026-05-18 노트북 turn — 방향 확정 (GLG 합의)**: cron-only batch 가 아니라 **on-query lazy ingest**. 디바이스별 `lifetract.db` 를 sync 하지 않고, 각 디바이스가 HA REST 를 자기 SSOT 로 두고 lazy 누적. cron 은 보조 (Oracle 에서만 매일 1회 신선도 baseline).

`docs/plan.md` Phase 6 *"Google Drive에서 Health Connect backup zip 다운로드"* 는 **폐기**.

### 새 경로 — Lazy Ingest 모델

```
Galaxy device
  → Samsung Health → Health Connect → HA Companion App
                                          ↓ (HA Mobile App API)
Oracle ARM Docker (ha.junghanacs.com)        ← 진짜 SSOT, recorder 30일 보관
  HA Core (sensor.sm_s942n_s26_glgman_*)
        ↑ pull (lazy on query, 또는 oracle cron)
        |
어느 디바이스든 (oracle / NUC / laptop)
  lifetract today / read 2026-05-17
    └─ DB miss? → ha.go fetch → DB upsert → 답
    └─ DB hit?  → DB 답 (~90ms, 지금과 동일)
        |
lifetract.db (디바이스별 lazy 캐시. sync 안 함.)
```

**핵심 약속**
- HA = 진짜 SSOT. lifetract.db = lazy 캐시
- 디바이스별 db sync 없음. 각자 자기 db 에 lazy 누적
- "지금 심박" 같은 live query 는 안 함 (필요 시 나중에 `--live` 옵션 자리만 열어둠)
- cron 은 oracle 에서 매일 1회 = oracle db 신선도 baseline. 다른 디바이스는 첫 호출 때만 한 번 HA hit

### 결정 사항 (확정)

| 항목 | 결정 | 근거 |
|---|---|---|
| Lazy vs cron-only | **on-query lazy ingest + oracle 보조 cron** | 디바이스별 db sync 회피, 인터페이스 단순 유지 |
| DB 스키마 | **기존 테이블 + `source TEXT` 컬럼** (`samsung_csv` / `ha_rest`), `(date, source)` upsert | import_log 와 결 일치, 출처 추적 가능 |
| Sleep stage | **duration 만 채우고 stages 빈 채로** | HA 가 stages 안 줌. CSV 후속 import 가 채움. 일상 질문 95% 는 duration 만으로 답 |
| cron 호스트 | **Oracle** | HA 가 같은 곳, 네트워크 hop 0, 24/7 |
| Live HR | **안 함 (인터페이스 자리만 둠)** | 일상 질문에 불필요. helper 분리로 나중에 `--live` 가능 |

### 구현 액션 (이 순서)

- [ ] **베이스라인 fix** — `lifetract today` off-by-one (§2 발견). 분리 commit 으로 먼저
- [ ] **`lifetract/ha.go` 신규 모듈** — HA REST helper. `pass show 2fa/totp/ha/junghanacs` 토큰 로딩 한 곳, `GET /api/states/<entity_id>` 호출, 엔티티 매핑 (`sensor.sm_s942n_s26_glgman_*` → DB 컬럼)
  - 우선 메트릭: `sleep_duration`, `heart_rate`, `resting_heart_rate`, `daily_steps`, `daily_distance`, `total_calories_burned`, `weight`
  - unknown 값 (`heart_rate_variability`, `oxygen_saturation`, blood_*, blood_glucose, active_calories_burned) 은 스킵
- [ ] **DB 마이그레이션** — `sleep`/`steps_daily`/`heart_rate`/`stress`/`weight` 에 `source TEXT` 컬럼 추가, 기존 데이터는 `'samsung_csv'` backfill, UNIQUE `(date, source)` (또는 sleep 은 `(start_time, source)`)
- [ ] **lazy hook** — `cmdToday`, `cmdRead` 가 DB miss 시 `ha.fetchAndUpsert(date)` 호출. 실패 시 stale 답 + 경고
- [ ] **`lifetract ingest --ha` 명시 커맨드** — cron / 수동 trigger 용. lazy hook 과 같은 helper 호출
- [ ] **`lifetract status` 에 HA 표시** — `ha.last_pull`, `ha.token_valid`, `ha.recent_entities` (지난 24h 안 받은 sensor warn)
- [ ] **Oracle cron 등록** — daily, [nixos-config](../nixos-config/) 측 설정 자리
- [ ] **`./run.sh update` 와 정합** — 수동 CSV update 와 HA lazy ingest 가 같은 DB 에 쓰므로 import_log 에 두 경로 명시

## 2. 베이스라인 정렬 — 코드 Mar 17 정지 상태 점검

문서가 현재 동작과 어긋난 부분 있을 가능성. 본 NEXT 항목 들어가기 전에 한 번 통과.

- [x] `./run.sh build && ./run.sh test` 그대로 통과하는지 — **통과 (47 tests, 2026-05-18 노트북)**
- [x] `lifetract status` 출력이 README/SKILL.md 예시와 일치하는지 — **shape 일치. size 미세 차이 (DB 35MB / SKILL.md 예시 33.3MB, atimelogger 5.25MB / 5.0MB) 는 데이터 누적이라 무시 가능**
- [x] `lifetract today` 가 현재 데이터(2026-03-10 이후 누적 안 됨)로도 의미 있는 출력 내는지 — **부분적. 두 이슈 발견 (아래)**
- [x] `~/repos/gh/self-tracking-data/` 의 실제 폴더 패턴이 `config.go` 의 glob 과 일치하는지 — **일치. `samsunghealth_*` glob 이 `samsunghealth_gtgkjh_20251006104703` 매칭**

### 2026-05-18 노트북 turn 발견 — 후속 항목

- [ ] **`lifetract today` off-by-one 버그** — `lifetract/query.go:96` 가 `dateStr(cutoffTime(0).AddDate(0, 0, 1))` 로 *내일* 날짜 반환 (시스템 2026-05-18 → today 출력 `2026-05-19`). `TestCmdToday` 는 빈 문자열만 검사해 잡지 못함. **fix 는 §1 진입 전 분리 ticket** — GLG 확인 받고 진행 (간단 fix 지만 query.go 의 시간축 정의 한 곳이라 영향 범위 확인 필요)
- [ ] **데이터 SSOT 시간 단절** — DB 마지막 데이터 `2026-03-10`. 노트북에 `samsunghealth_gtgkjh_20251006104703` 한 폴더만. NEXT §1 (HA REST) 의 본질적 이유 = 수동 export 단절. §1 도입 후 자연 해결
- [ ] **SKILL.md `today` 예시 shape 호도 가능** — `TimeCategories []TimeCategory json:"time_categories,omitempty"` 라 빈 날엔 누락. 현재 SKILL.md `today` JSON 예시는 `time_categories: [...]` 포함 (데이터 있는 날 기준). 예시를 *데이터 있는 날* (예: `read 2025-10-04`) 명시로 바꾸거나 `today` 예시 두 가지 (있을 때 / 없을 때) 병기. 본 repo SKILL.md + pi-skills/lifetract/SKILL.md 양쪽 동기화 필요
- [ ] **AGENTS.md §2 "2026-03-10 시점" 라벨** — DB 36MB / 198k rows 도 같은 시점. HA REST 도입 후 자연스럽게 갱신될 자리. 지금은 정확

## 3. 에이전트 호출 패턴 표준화

"어제 힣 잘잤나?" 한 줄에 답이 나오도록.

- [ ] `lifetract read yesterday` / `lifetract read today` 같은 시간 단축형 지원 (Day Denote ID 자동 산출)
- [ ] JSON 출력에 *자연어 한 줄 요약* 필드 추가 검토 — 예 `"summary_ko": "어제 7시간 7분 잤고 11,044보 걸음"`. 에이전트가 LLM 호출 한 번 줄임
- [ ] `lifetract` 스킬 ([pi-skills](https://github.com/junghan0611/pi-skills/tree/main/lifetract)) SKILL.md 에 *에이전트 호출 예제* 섹션 추가 — "어제 잘 잤나?" → 어떤 명령으로 답하는지

## 4. geworfen 표면

[geworfen](https://github.com/junghan0611/geworfen) 홈페이지가 이 데이터의 사람 표면. 인터페이스 일치를 위해:

- [ ] geworfen 측에서 lifetract 호출 (subprocess / HTTP / 파일 캐시) 결정
- [ ] 같은 JSON 키로 geworfen 위젯 그대로 매핑되는지

## 5. plan.md 갱신

`docs/plan.md` Phase 6 를 새 경로 반영해 다시 쓴다. 이번 NEXT 1번이 끝난 후.

- [ ] Phase 6 → "Home Assistant REST 실시간 ingest" 로 재정의
- [ ] Phase 7 신설 후보 — *에이전트 호출 표준화 + geworfen 표면 통합*

## 6. 영속화 옮길 자리 (NEXT.md 휘발 → 영속 destination)

본 NEXT 단계가 닫히면 아래로 옮기고 여기서 지운다.

- `AGENTS.md` §2.두 입력 스트림 → 세 스트림 (HA REST 추가)
- `AGENTS.md` §5.Operational workflow → HA ingest 호출 절차 추가
- `docs/plan.md` Phase 6 재정의 결과
- `README.md` Architecture 다이어그램에 HA 입력 화살표
- `SKILL.md` ingest 커맨드 + 에이전트 호출 예제

## 7. Cross-repo 연결

- [ ] [nixos-config](https://github.com/junghan0611/nixos-config) `NEXT.md §5` 의 baton 항목 다 닫히는 시점에 nixos-config 측 NEXT 정리
- [ ] [homeagent-config](https://github.com/junghan0611/homeagent-config) — HA 가 떴으니 homeagent 측 통합 테스트 후보 환경으로 dogfooding
- [ ] [pi-skills/lifetract](https://github.com/junghan0611/pi-skills/tree/main/lifetract) — 새 ingest 커맨드 도입 후 스킬 번들 재배포

---

본 도구의 책임은 *데이터가 어떻게 흘러나가는가* 다 (AGENTS.md §7 마무리). NEXT 1번이 들어가야 비로소 그 책임을 안정적으로 진다.
