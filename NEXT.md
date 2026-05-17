# NEXT.md — 다음 할 일

운영 baseline 은 [AGENTS.md](AGENTS.md). 후속/미완 검증은 여기에. 끝난 항목 지우고, 새로 발견한 후속 추가. 영속할 사실은 AGENTS.md / [docs/plan.md](docs/plan.md) / commit history 로 옮긴다.

오늘의 의미축은 [[denote:20260517T211731]] (botlog) 에 보존되어 있다 — 데이터를 옮기는 일을 멈추고, 인터페이스가 데이터를 만나러 오게 한 날.

---

## 1. Home Assistant REST → 새 입력 스트림 (Phase 6 재정의)

**2026-05-17 — Oracle Docker 에 HA 띄움 ([nixos-config](../nixos-config/) `53a8d2e`), Companion App 등록 (디바이스 `SM-S942N-S26-GLGMAN`, mobile_app `b6a48768e1c2b22a`), Health Connect 16개 센서 활성, PoC 통과: `sensor.sm_s942n_s26_glgman_sleep_duration = 427 min` (어젯밤 7h7m).**

`docs/plan.md` Phase 6 *"Google Drive에서 Health Connect backup zip 다운로드"* 는 **폐기**. 새 경로는 HA REST API polling.

### 새 경로

```
Galaxy device
  → Samsung Health → Health Connect → HA Companion App
                                          ↓ (HA Mobile App API)
Oracle ARM Docker (ha.junghanacs.com)
  HA Core (sensor.sm_s942n_s26_glgman_*)
        ↓
NUC/laptop cron (lifetract 호스트)
  lifetract ingest --ha    ← 신규 구현 항목
        ↓
lifetract.db (sleep / heart_rate / steps_daily / ...)
```

### 구현 액션

- [ ] **lifetract ingest --ha 신규 커맨드** — `~/.lifetract/ha.env` 또는 환경변수에서 토큰 로딩, `GET /api/states/<entity_id>` 호출
  - 우선 메트릭: `sleep_duration`, `heart_rate`, `resting_heart_rate`, `daily_steps`, `daily_distance`, `total_calories_burned`, `weight`
  - unknown 값(`heart_rate_variability`, `oxygen_saturation`, blood_*, blood_glucose, active_calories_burned)은 스킵 정책
- [ ] **토큰 로딩 패턴** — `pass show 2fa/totp/ha/junghanacs` (JWT long-lived access token, password-store 보관). 로딩 헬퍼 한 곳에 둠
- [ ] **DB 스키마 합류** — HA 측 메트릭을 기존 `sleep`/`heart_rate`/`steps_daily`/`weight` 테이블에 흡수할지, 별도 `ha_*` 테이블에 둘지 결정
  - 단순한 안: 기존 테이블 그대로 + `source` 컬럼 추가 (`samsung_csv` / `ha_rest`)
  - 같은 시각에 두 소스가 같은 데이터를 가져오면 dedupe 정책 필요
- [ ] **cron 일1회** — NUC 또는 laptop. HA 의 recorder 가 30일 보관이므로 누락 위험은 작지만 안전하게 매일
- [ ] **`lifetract status` 에 HA 표시** — `ha.last_pull`, `ha.entity_count`, `ha.token_valid`
- [ ] **`./run.sh update` 와 정합** — 수동 update 와 HA ingest 가 같은 DB 에 쓰므로 import_log 에 두 경로 명시

### 결정 포인트 (작업 들어가기 전 한 번 정리)

- *Drop-in 흡수* 와 *별도 테이블* 중 어느 쪽? — 정량 의미는 같지만 출처가 다르다. `source` 컬럼이 자연스러울 듯
- *수면 stage* 는 HA Companion App 이 안 주는 듯하다 (단일 `sleep_duration` 만). stage 분포는 당분간 Samsung Health CSV 만 — 이중 입력 구조 받아들여야 함
- *Resting HR* / *HRV* 는 CSV 만 정확. 라이브 HR 은 HA 가 강함. 두 결을 분리해서 보존
- *cron 호스트* — NUC vs laptop. NUC 가 살아있는 서버라 NUC 권장. 토큰은 NUC 의 password-store

## 2. 베이스라인 정렬 — 코드 Mar 17 정지 상태 점검

문서가 현재 동작과 어긋난 부분 있을 가능성. 본 NEXT 항목 들어가기 전에 한 번 통과.

- [ ] `./run.sh build && ./run.sh test` 그대로 통과하는지
- [ ] `lifetract status` 출력이 README/SKILL.md 예시와 일치하는지
- [ ] `lifetract today` 가 현재 데이터(2026-03-10 이후 누적 안 됨)로도 의미 있는 출력 내는지
- [ ] `~/repos/gh/self-tracking-data/` 의 실제 폴더 패턴이 `config.go` 의 glob 과 일치하는지

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
