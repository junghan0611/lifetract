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

## Phase 6 — Health Connect 실시간

- [ ] Google Drive에서 Health Connect backup zip 다운로드
- [ ] SQLite 파싱 (samsung-health-skill 참고)
- [ ] 최신 데이터 우선, CSV는 아카이브 폴백
