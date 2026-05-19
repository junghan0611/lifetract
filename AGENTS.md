# lifetract AGENTS.md

> **다음 할 일은 [NEXT.md](NEXT.md).** AGENTS.md 는 *지금 이 도구가 무엇인지*, NEXT.md 는 *다음 한 걸음*.

lifetract 는 *나의 시간축*에 대한 **데이터 인터페이스** 다. 정성 기록은 denotecli(노트/저널), 정량 기록은 여기 — 같은 [Denote ID](https://protesilaos.com/emacs/denote) 축(`YYYYMMDDTHHMMSS`)으로 사람과 에이전트가 같은 단어로 묻는다.

이 정체성은 2026-05-17 에 다시 분명해졌다. 그 의미축은
[[denote:20260517T211731]] (botlog) 에 보존되어 있다 — 데이터를 옮기는 것이
불편해 흐름이 끊겼고, Home Assistant 가 그 흐름을 회복시켰다. 자세한 운영
상태는 아래 §2, 다음 변화는 NEXT.md.

## 1. 정체성 — 자료실이 아니라 인터페이스

| 무엇이 아닌가 | 무엇인가 |
|---|---|
| 또 하나의 데이터 dump | Samsung Health + aTimeLogger 를 *통합 조회*하는 CLI |
| Python/pandas 노트북 | 단일 정적 Go 바이너리, JSON 출력, 에이전트가 호출하기 좋음 |
| 가끔 돌리는 분석 스크립트 | "어제 잘 잤나?" 한 줄에 즉시 답하는 인터페이스 |

향할 표면(surface) 두 가지가 같은 인터페이스를 공유한다.

- [geworfen](https://github.com/junghan0611/geworfen) 웹 페이지 — 사람에게 시간축을 보여주는 자리
- 에이전트 호출 — `lifetract today` / `lifetract read 어제` 같은 한 줄에 backend 가 되는 자리

같은 출력(JSON, Denote ID 기준) 을 양쪽이 공유한다. 이 일치를 깨지 않는 것이 본 도구의 첫째 책임.

## 2. 현재 운영 상태 (2026-05-17 기준)

### Stack

- 언어/런타임: Go 1.21+, `modernc.org/sqlite` (CGO 없는 순수 Go)
- 산출물: 단일 정적 바이너리 (`CGO_ENABLED=0 -ldflags="-s -w"`)
- 설치: `./run.sh build` → `~/.local/bin/lifetract`
- Nix: `flake.nix` 정적 빌드 동등 결과
- 테스트: 47 tests, 68% coverage (Go std test + benches)

### 데이터 SSOT — repo 밖

```
~/repos/gh/lifetract/                ← 본 repo (코드, 빌드, 문서)
~/repos/gh/self-tracking-data/       ← 데이터 SSOT, 별도 repo
    ├── samsunghealth_YYYYMMDDHHMMSSS/   # CSV exports, 시간순 폴더
    ├── atimelogger/database.db3         # 단일 SQLite (덮어쓰기)
    └── lifetract.db                     # 빌드 결과 (CSV+ATL → 통합 DB)
        # 198,547 rows, 36MB (2026-05-19 갱신, Samsung CSV 5/18 까지). Day/Event Denote ID 인덱싱
```

코드 repo 안에 절대 데이터를 두지 않는다. CSV/DB 모두 위 경로 기준.

### 두 입력 스트림

| 소스 | 형식 | 기간 | 갱신 방식 (현재) |
|---|---|---|---|
| Samsung Health | CSV export (8 종) | 2017-03 ~ | 폰에서 수동 export → 폴더 배치 → `./run.sh update` |
| aTimeLogger | SQLite (`database.db3`) | 2021-10 ~ | 폰에서 .eml 백업 → 데이터 디렉토리 교체 → `./run.sh update` |

세 번째 스트림은 NEXT.md 에서 진행 중 — Home Assistant REST API polling. CSV
수동 export 의존을 끊는 방향이다.

### DB-first / CSV-fallback

```
lifetract <command>
  └─ lifetract.db 있음?  → SQLite 쿼리 (~90ms) → JSON
                          → 없음 → CSV 직접 파싱 (~300ms) → JSON
                                   (aTimeLogger 는 CSV 모드 미지원)
```

DB 가 본 경로, CSV 는 안전망. `lifetract import --exec` 가 두 소스를 통합 DB 로 빌드 (~1.5초, 36MB).

### DB Schema (Samsung Health + aTimeLogger 통합)

```
sleep / sleep_stage / heart_rate / steps_daily / stress /
exercise / weight / hrv          ← Samsung Health 8 테이블
atl_category / atl_interval      ← aTimeLogger 18 카테고리 / 13,918 intervals
import_log                       ← 임포트 메타
```

## 3. Denote ID 축

```
Day:   20251004T000000   ← denotecli journal 과 동일 키
Event: 20251004T233000   ← 개별 수면/운동 세션
```

`denotecli search "20251004"` 와 `lifetract read 2025-10-04` 가 같은 날짜를
가리킨다 → 정성/정량을 같은 축으로 본다. 이 축이 흔들리면 시스템 전체가
흔들린다.

## 4. Commands (SSOT 는 [SKILL.md](SKILL.md))

상세 옵션·예시는 SKILL.md 가 권위. 여기는 손에 익혀야 할 최소만.

```bash
./run.sh build      # 빌드 + 설치 (~/.local/bin)
./run.sh update     # ~/repos/gh/self-tracking-data/<YYYYMMDD>/ 에서 데이터 배치 + DB 재빌드
./run.sh test       # go test ./...

lifetract status          # 데이터 소스 + DB 상태
lifetract today           # 오늘 통합 요약 (sleep, heart, steps, time 등)
lifetract read 2026-05-16 # 특정 날짜 종합
lifetract timeline --days 30
lifetract sleep   --days 7 --summary
lifetract time    --days 7 --category 본짓
```

JSON 출력이 기본. 에이전트는 stdout 그대로 파싱.

## 5. Operational workflow

### 평소 갱신 (현재 방식)

1. 폰에서 Samsung Health export + aTimeLogger 백업 받음
2. `~/repos/gh/self-tracking-data/<YYYYMMDD>/` 에 두 파일 배치
3. `./run.sh update` — Samsung Health 폴더 이동 + aTimeLogger DB 교체 + `lifetract import --exec`
4. `lifetract status` 로 갱신 확인 + 데이터 update log 정리

### 코드 변경

- 새 커맨드: `lifetract/<feature>.go` 한 파일 + `main.go` 라우팅 한 줄
- 새 데이터 소스: `lifetract/import_exec.go` 와 `lifetract/db.go` 스키마
- 인터페이스 변경 시 반드시 JSON 출력 호환성 유지 — 양쪽 표면(geworfen, 에이전트)이 깨지지 않게

### 빌드 정책

- `CGO_ENABLED=0` 절대 유지 — modernc.org/sqlite 가 본 선택의 이유
- 단일 바이너리 — Termux/Android 까지 그대로 들고 다닐 수 있어야 함
- Nix flake 와 `./run.sh build` 결과가 동등해야 함

## 6. Related projects

| Repo | 관계 |
|---|---|
| [denotecli](https://github.com/junghan0611/denotecli) | 정성 — 노트/저널. 같은 Denote ID |
| [self-tracking-data](https://github.com/junghan0611/self-tracking-data) | 데이터 raw SSOT |
| [pi-skills/lifetract](https://github.com/junghan0611/pi-skills/tree/main/lifetract) | 에이전트 스킬 (바이너리 + SKILL.md 번들) |
| [nixos-config](https://github.com/junghan0611/nixos-config) | Oracle Docker, Home Assistant 인프라 — 새 입력 스트림의 호스트 |
| [homeagent-config](https://github.com/junghan0611/homeagent-config) | IoT/Home automation 측 연결고리 (Open Home Foundation 방향) |

## 7. Gotchas

- *수동 update 사이클이 본 발목이었다*. 데이터를 옮기는 일이 손이 많이 가면, 흐름이 끊긴다. 새 입력(HA REST) 도입 시 이 교훈을 잊지 말 것 — 결국 사람이 손대지 않아도 흐르는 자리만 살아남는다. (NEXT.md §1)
- `CGO_ENABLED=0` 깨면 Android/ARM 어디서나 도는 단일 바이너리 전제가 무너진다. 누군가 `mattn/go-sqlite3` 로 바꾸자고 하면 거절.
- `lifetract.db` 는 빌드 산출물 — repo 에 절대 커밋하지 말 것. `self-tracking-data` 쪽 .gitignore 와 본 repo .gitignore 둘 다 책임.
- Day Denote ID 는 `YYYYMMDDT000000`. Event 는 실제 시각. 둘을 섞으면 cross-join 이 깨진다.
- aTimeLogger 카테고리 한글명("본짓/수면/가족/식사/...") 은 본 코드가 의존하는 핵심 키 — 폰 앱에서 카테고리명 바꾸면 import 가 침묵 실패할 수 있다.

## 8. Commands 빠른 참조

```bash
# 데이터 갱신 (현재 방식, 수동 export 기반)
./run.sh update

# 빌드
./run.sh build

# 테스트
./run.sh test

# 일상 호출
lifetract status
lifetract today
lifetract read <YYYY-MM-DD or YYYYMMDDTHHMMSS>
lifetract timeline --days 30
lifetract time --days 7 --category 본짓
```

---

도구의 정체성은 *데이터가 어떻게 모이는가* 보다 *데이터가 어떻게 흘러나가는가* 에서 결정된다. lifetract 의 책임은 후자.
