# lifetract 개발 플랜

## 비전

"시간과정신의방" 데이터 레이어의 정량 데이터 파트.
denotecli(정성: 노트/저널)와 lifetract(정량: 건강/시간)가 같은 날짜축으로 줄지어,
에이전트가 "그 날 무엇을 했고, 어떤 상태였는지"를 통합 조회할 수 있게 한다.

## 아키텍처

```
denotecli  ──┐
              ├── date: "YYYY-MM-DD" ── 단일 타임라인
lifetract  ──┘

denotecli search "2025-10-04" → 그날의 노트/저널
lifetract timeline --days 1   → 그날의 건강/시간 데이터
```

## 데이터 소스

| 소스 | 포맷 | 크기 | 기간 | Phase |
|------|------|------|------|-------|
| Samsung Health CSV | CSV 77개 | ~950MB | 2017~2025 | 1 ✅ |
| aTimeLogger | SQLite | 5MB | ongoing | 2 |
| Health Connect | SQLite (zip) | ~수십MB | 실시간 | 3 |

## Phase 1 (현재) ✅

- [x] Samsung Health CSV 파서 (sleep, steps, heart, stress, exercise)
- [x] timeline 커맨드 (날짜별 통합 뷰)
- [x] status, today 커맨드
- [x] flake.nix
- [x] SKILL.md (pi-skills)
- [x] JSON 출력, denotecli 호환 날짜키

## Phase 2 — aTimeLogger SQLite

- [ ] modernc.org/sqlite 또는 sqlite3 CLI 연동
- [ ] aTimeLogger DB 스키마 파악
- [ ] time 커맨드 구현 (카테고리별 시간)
- [ ] timeline에 시간 데이터 통합
- [ ] flake.nix vendorHash 업데이트

## Phase 3 — Health Connect 실시간

- [ ] Google Drive에서 Health Connect backup zip 다운로드 (gogcli)
- [ ] SQLite 파싱 (samsung-health-skill 참고)
- [ ] 최신 데이터 우선, CSV는 아카이브 폴백
- [ ] Oracle VM 배치 파이프라인 (선택)

## Phase 4 — 상관분석

- [ ] correlate 커맨드: 수면-딥워크, 운동-스트레스 등
- [ ] denotecli CLOCK 데이터와 연동 (작업 세션)
- [ ] 주간/월간 리포트 생성

## 관련 프로젝트

- ~/repos/gh/denotecli — 정성 데이터 (노트 3,000+)
- ~/repos/gh/self-tracking-data — 원본 데이터
- ~/repos/3rd/samsung-health-skill — Python 참조 구현
- ~/repos/gh/memacs-config — 원래 Org 변환 접근 (대체됨)
