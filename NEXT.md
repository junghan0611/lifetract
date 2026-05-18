# NEXT.md — 다음 할 일

운영 baseline 은 [AGENTS.md](AGENTS.md). 후속/미완 검증은 여기에. 끝난 항목 지우고, 새로 발견한 후속 추가. 영속할 사실은 AGENTS.md / [docs/plan.md](docs/plan.md) / commit history 로 옮긴다.

오늘의 의미축은 [[denote:20260517T211731]] (botlog) 에 보존되어 있다 — 데이터를 옮기는 일을 멈추고, 인터페이스가 데이터를 만나러 오게 한 날.

---

## 1. Home Assistant REST → 라이브 인터페이스 (Phase 6 재정의)

**2026-05-17** — Oracle Docker 에 HA 띄움 ([nixos-config](../nixos-config/) `53a8d2e`), Companion App 등록 (디바이스 `SM-S942N-S26-GLGMAN`, mobile_app `b6a48768e1c2b22a`), Health Connect 16개 센서 활성, PoC 통과: `sensor.sm_s942n_s26_glgman_sleep_duration = 427 min` (어젯밤 7h7m).

**2026-05-18 오전 — 방향 1차 확정 (GLG 합의)**: cron-only batch 가 아니라 **on-query lazy ingest**. 디바이스별 `lifetract.db` 를 sync 하지 않고, 각 디바이스가 HA REST 를 자기 SSOT 로 두고 lazy 누적. cron 은 보조 (Oracle 에서만 매일 1회 신선도 baseline).

**2026-05-18 오후 — 방향 2차 정정 (오라클 GPT힣 multi-sensor 검증 후)**:

GPT힣 의 두 라이브 시도 ("어제 잘 잤나?" 통과 / "1주일치는?" 경계 발견) 가 phase 3 의 본질을 정확히 짚었다:

- **HA = *라이브 인터페이스*** (오늘/지금 답). 적립 인프라 본질상 5/17 이전 데이터는 영원히 없음
- **Samsung Health = 본 데이터 SSOT** (과거 9년치). 주기적 덤프로 채움 — 사용자 영역
- **빈 자리** — 사용자가 Samsung Health 덤프해서 직접 채움
- 따라서 **Phase 4 (DB upsert + lazy hook) 의 *시급성* 낮음**. 본 시급 자리는 *인터페이스 검토* (phase 3.5 가 잘 정착되는지)
- Phase 4 는 *언젠가* 의미 있음 (HA 적립 데이터를 DB 에 누적하면 좋음). 단 본 turn 의 본질적 필요는 아님

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
| 데이터 축 정의 (2026-05-18 오후) | **Samsung Health 주기적 덤프 = 본 데이터 SSOT / HA = 라이브 인터페이스** | HA recorder 는 적립 인프라라 과거 backfill 못 함. 9년 timeline 은 Samsung CSV 만이 답 |
| Lazy vs cron-only | **on-query lazy ingest + oracle 보조 cron** (phase 4 자리, 시급성 낮음) | 디바이스별 db sync 회피, 인터페이스 단순 유지. *현재* 본질적 필요는 약함 |
| DB 스키마 | **기존 테이블 + `source TEXT` 컬럼** (`samsung_csv` / `ha_rest`), `(date, source)` upsert | import_log 와 결 일치, 출처 추적 가능. phase 4 진입 시 |
| Sleep stage | **duration 만 채우고 stages 빈 채로** | HA 가 stages 안 줌. CSV 후속 import 가 채움. 일상 질문 95% 는 duration 만으로 답 |
| cron 호스트 | **Oracle** (phase 4 자리) | HA 가 같은 곳, 네트워크 hop 0, 24/7 |
| Live HR | **안 함 (인터페이스 자리만 둠)** | 일상 질문에 불필요. helper 분리로 나중에 `--live` 가능. phase 3.5 `ha state` 가 사실상 이 자리 |

### 구현 액션 (진행 상황)

**Phase 3 — read-only HA 인터페이스 (DONE 2026-05-18 노트북 turn)**
- [x] **베이스라인 fix** — `lifetract today` off-by-one (`c90c344`)
- [x] **`lifetract/ha.go` 신규 모듈** (`fe696e7`) — HAClient, 3단계 토큰 로딩 (`HA_TOKEN` env → `pass show 2fa/totp/ha/junghanacs` → `~/.lifetract/ha.env`), GetState/GetAllStates/Ping
- [x] **`KnownEntities` declarative 등록 24개** (`fe696e7`) — sleep/movement/heart/body/energy/context/vitals 그룹. 새 sensor 추가 = 한 줄
- [x] **CLI surface** (`fe696e7`) — `lifetract ha ping|state|states|entities`
- [x] **SKILL.md 양쪽 sync** (`87835b3` 본 repo + `08d8f22` agent-config)

**Phase 3.5 — HA history (DONE 2026-05-18 노트북 turn)**
- [x] **`ha.go GetHistory`** (`16e76f8`) — `GET /api/history/period/<start>?filter_entity_id=...&end_time=...` URL encoding 처리, `[[HAState,...]]` flatten
- [x] **`ha history` CLI** (`16e76f8`) — `HAHistoryResult` shape (entity/kind/unit/days/from/to/count/points), `--days N` 플래그 (default 7)
- [x] **mock test 3개 + 라이브 검증** (`16e76f8`) — oracle GPT힣 multi-sensor 검증 통과 ("코드는 정상, sleep_duration sensor 자체가 2건": HA recorder 적립 인프라 본질)
- [x] **SKILL.md 양쪽 sync** (`16e76f8` 본 repo + `5ec2ad2` agent-config) — "적립 인프라" 본질 명시

**Phase 4 — DB upsert + lazy hook (시급성 낮음, 언젠가)**

본질적 시급성 약함 — 데이터 축은 Samsung 주기적 덤프가 본 답. 다음 자리 들어가기 전 명시적 결정 받을 자리.

- [ ] **DB 마이그레이션** — `sleep`/`steps_daily`/`heart_rate`/`stress`/`weight` 에 `source TEXT` 컬럼 추가, 기존 데이터는 `'samsung_csv'` backfill, UNIQUE `(date, source)`
- [ ] **`ha.UpsertState(db, state)` helper** — Kind 별 컬럼 매핑 후 upsert. phase 3.5 의 GetState/GetHistory 가 이미 데이터 준비
- [ ] **lazy hook** — `cmdToday`, `cmdRead` 가 DB miss 시 자동 HA fetch + upsert
- [ ] **`lifetract ingest --ha` 명시 커맨드** — 수동 trigger
- [ ] **`lifetract status` 에 HA 표시** — `ha.last_pull`, `ha.token_valid`
- [ ] **Oracle cron 등록** — daily, [nixos-config](../nixos-config/) 측
- [ ] **`./run.sh update` 와 정합** — import_log 에 두 경로 명시

## 2. 베이스라인 정렬 — 코드 Mar 17 정지 상태 점검

문서가 현재 동작과 어긋난 부분 있을 가능성. 본 NEXT 항목 들어가기 전에 한 번 통과.

- [x] `./run.sh build && ./run.sh test` 그대로 통과하는지 — **통과 (47 tests, 2026-05-18 노트북)**
- [x] `lifetract status` 출력이 README/SKILL.md 예시와 일치하는지 — **shape 일치. size 미세 차이 (DB 35MB / SKILL.md 예시 33.3MB, atimelogger 5.25MB / 5.0MB) 는 데이터 누적이라 무시 가능**
- [x] `lifetract today` 가 현재 데이터(2026-03-10 이후 누적 안 됨)로도 의미 있는 출력 내는지 — **부분적. 두 이슈 발견 (아래)**
- [x] `~/repos/gh/self-tracking-data/` 의 실제 폴더 패턴이 `config.go` 의 glob 과 일치하는지 — **일치. `samsunghealth_*` glob 이 `samsunghealth_gtgkjh_20251006104703` 매칭**

### 2026-05-18 노트북 turn 발견 — 후속 항목 정리

- [x] **`lifetract today` off-by-one 버그** — `c90c344` 로 fix. 테스트도 강화 (시스템 일자 일치 검증)
- [ ] **데이터 SSOT 시간 단절** — DB 마지막 데이터 `2026-03-10`. **방향 정정 (2026-05-18 오후)**: Samsung Health 주기적 덤프가 본 답. 사용자가 직접 채울 자리. 자동화는 후순위
- [x] **SKILL.md `today` 예시 shape 호도** — `87835b3` 에서 두 예시 (데이터 있는 날 / 없는 날) 병기 + omitempty 동작 명시. 양쪽 SKILL.md sync 됨
- [ ] **AGENTS.md §2 "2026-03-10 시점" 라벨** — 그대로. Samsung 덤프 갱신 시 자연스럽게 변동될 자리
- [ ] **AGENTS.md §2 "두 입력 스트림" → "세 입력 스트림"** — HA REST 추가 (phase 3.5 완료 반영). §6 영속화 자리

## 3. 에이전트 호출 패턴 표준화 (현재 본 시급 자리)

"어제 힣 잘잤나?" 한 줄에 답이 나오도록.

**2026-05-18 turn 의 두 라이브 검증** 이 이 자리의 출발점:
1. 첫 시도 (`ha state sleep_duration`) → 어제 답 즉시 가능 — *코드 변경 없이* 에이전트가 lazy 모델을 자연어 수준에서 잡음
2. 두 번째 시도 (`ha history` 7일치 multi-sensor) → 진행 중 (oracle GPT힣 검증 대기)

→ phase 3.5 의 `ha` 커맨드가 사실상 "어제 잘 잤나?" 의 backend 첫 자리. 본 §3 의 후속:

- [ ] `lifetract read yesterday` / `lifetract read today` 같은 시간 단축형 (Day Denote ID 자동 산출)
- [ ] JSON 출력에 *자연어 한 줄 요약* 필드 추가 검토 — 예 `"summary_ko": "어제 7시간 7분 잤고 11,044보 걸음"`. 에이전트가 LLM 호출 한 번 줄임
- [ ] `lifetract` 스킬 ([pi-skills](https://github.com/junghan0611/pi-skills/tree/main/lifetract)) SKILL.md 에 *에이전트 호출 예제* 섹션 추가 — "어제 잘 잤나?" → `ha state sleep_duration`, "최근 1주 심박" → `ha history heart_rate --days 7`
- [ ] multi-sensor 묶음 — 시간축 위에 sleep + heart + steps 정렬한 *주간 리포트* 커맨드 검토 (`lifetract weekly`?). 또는 에이전트 자연어 추론에 맡길지 결정

## 4. geworfen 표면

[geworfen](https://github.com/junghan0611/geworfen) 홈페이지가 이 데이터의 사람 표면. 인터페이스 일치를 위해:

- [ ] geworfen 측에서 lifetract 호출 (subprocess / HTTP / 파일 캐시) 결정
- [ ] 같은 JSON 키로 geworfen 위젯 그대로 매핑되는지

## 5. plan.md 갱신

`docs/plan.md` Phase 6 를 새 경로 반영해 다시 쓴다.

- [ ] Phase 6 → "Home Assistant REST 라이브 인터페이스 (phase 3 + 3.5 done)" 로 재정의. lazy ingest 는 phase 4 자리지 본 phase 6 의 본질이 아님을 명시
- [ ] Phase 7 신설 후보 — *에이전트 호출 표준화 + geworfen 표면 통합*

## 6. 영속화 옮길 자리 (NEXT.md 휘발 → 영속 destination)

본 NEXT 단계가 닫히면 아래로 옮기고 여기서 지운다.

- `AGENTS.md` §2 두 입력 스트림 → **세 입력 스트림** (HA REST 라이브 인터페이스 추가, phase 3.5 완료 후 자연 자리)
- `AGENTS.md` §5 Operational workflow → HA 호출 절차 (한 줄: `ha state <kind>` / `ha history <kind> --days N`)
- `docs/plan.md` Phase 6 재정의 결과 (위 §5)
- `README.md` Architecture 다이어그램에 HA 입력 화살표
- `SKILL.md` ingest 커맨드 (phase 4 후) — 지금은 `ha` 커맨드까지만 반영됨

## 7. Cross-repo 연결

- [ ] [nixos-config](https://github.com/junghan0611/nixos-config) `NEXT.md §5` 의 baton 항목 다 닫히는 시점에 nixos-config 측 NEXT 정리
- [ ] [homeagent-config](https://github.com/junghan0611/homeagent-config) — HA 가 떴으니 homeagent 측 통합 테스트 후보 환경으로 dogfooding
- [ ] [pi-skills/lifetract](https://github.com/junghan0611/pi-skills/tree/main/lifetract) — 본 repo + agent-config 측 SKILL.md 가 SSOT. pi-skills 는 symlink (재배포 자동)

---

본 도구의 책임은 *데이터가 어떻게 흘러나가는가* 다 (AGENTS.md §7 마무리). 본 turn 에서 phase 3 + 3.5 까지 *라이브 인터페이스* 가 들어왔다. Samsung Health 주기적 덤프 (사용자 영역) 와 HA 라이브 인터페이스 (이 자리) 가 같이 흐를 때 lifetract 의 시간축이 안정적으로 흐른다. phase 4 (DB upsert + lazy hook) 는 *언젠가* 의 자리.
