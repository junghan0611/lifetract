# lifetract 로드맵

## 비전

denotecli(정성: 노트/저널) + lifetract(정량: 건강/시간) → 같은 Denote ID 축으로
에이전트가 "그 날 무엇을 했고, 어떤 상태였는지"를 통합 조회.

## Phase 1 — Samsung Health CSV ✅

- [x] CSV 파서 8종 (sleep, sleep_stage, heart_rate, steps, stress, exercise, weight, hrv)
- [x] timeline, today, read, status 커맨드
- [x] Denote ID 체계 (Day + Event level)
- [x] flake.nix (CGO_ENABLED=0 정적 빌드)
- [x] SKILL.md, run.sh (pi-skills 배포)
- [x] 47 tests, 68% coverage

## Phase 2 — SQLite DB ✅

- [x] modernc.org/sqlite (pure Go, CGO 불필요)
- [x] `lifetract import --exec`: CSV+aTimeLogger → lifetract.db (33MB, 183,635 rows, 1.5초)
- [x] DB first / CSV fallback 전략
- [x] aTimeLogger 파싱 (18 카테고리, 13,102 intervals)
- [x] `time` 커맨드 작동 (aTimeLogger 카테고리별 일별 조회)
- [x] 모든 커맨드 DB 쿼리 전환 (timeline 3x 빠름)
- [x] status에 DB 상태 표시

## Phase 3 — Org 저널 통합 (계획)

- [ ] denotecli 저널을 lifetract에서 교차 참조
- [ ] Org 기록 방식 변화 이력 정리 필요 (형식 변경 여러 번)
- [ ] 접근: subprocess로 denotecli 호출 또는 DB에 저널 메타 캐시

## Phase 4 — 공개 DB + Export

- [ ] `lifetract export --exec`: 개인정보 제거된 공개용 DB
- [ ] deviceuuid, pkg_name, client_data 등 제거
- [ ] GitHub public release

## Phase 5 — 상관분석

- [ ] correlate 커맨드: 수면↔본짓, 운동↔스트레스 등
- [ ] 주간/월간 리포트 생성
- [ ] Traction 비율 추이 (Indistractable 지표)

## Phase 6 — 라이브 입력 스트림 ✅ (2026-05-17 ~ 2026-05-19)

> **방향 정정 (2026-05-18)**: 옛 안 "Google Drive 에서 Health Connect backup zip 다운로드" 는 폐기. HA recorder 는 적립 인프라이므로 9년 timeline backfill 불가 — 본 데이터 SSOT 는 Samsung CSV 주기 덤프, HA 는 라이브 인터페이스. 두 source 가 5/17 자리에서 겹쳐 시간축 단절 없음. 자세한 의미축은 [[denote:20260517T211731]] (botlog).

- [x] **Home Assistant REST 인터페이스** — `ha.go` (HAClient, GetState/GetAllStates/GetHistory/Ping), `KnownEntities` 24 sensor declarative 등록, CLI `ha ping|state|states|entities|history`, mock test + 라이브 검증 통과
- [x] **Samsung CSV 정기 덤프 사이클** — `./run.sh update` (`~/repos/gh/self-tracking-data/<YYYYMMDD>/` → 폴더 이동 + db3 교체 + `import --exec`). 2026-05-19 첫 정기 갱신 (198,547 rows, Samsung CSV → 2026-05-18)
- [ ] **새 sleep 파일군** (`sleep_data`, `sleep_combined`, `sleep_raw_data`, `sleep_snoring`) schema 확장 — Galaxy S26 변경 영향
- [ ] **새 sensor** (`respiratory_rate`, `oxygen_saturation`) schema 확장
- [ ] **aTimeLogger 자동 갱신** (현재는 수동 db3 교체)

## Phase 7 — HA → DB lazy ingest (시급성 낮음)

> *언젠가* 의미 있음. 본 시급은 아님 — Samsung 주기 덤프가 SSOT 를 갱신하므로 갭이 자연스럽게 닫힘.

- [ ] DB 스키마: 기존 테이블 + `source TEXT` 컬럼 (`samsung_csv` / `ha_rest`), `(date, source)` upsert
- [ ] `lifetract today` / `read` 의 "오늘 자리" 만 HA hit (5/19 today 의 `avg_hr=0`, `stress_avg=0` 자리 해결)
- [ ] sleep stage 빈 자리는 다음 Samsung 덤프가 채움 (HA 가 stages 안 줌)
