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
~/sync/family/lifedata/              ← 폰이 Syncthing 으로 떨구는 자리 (덤프 원본)
    └── samsunghealth_gtgkjh_<ts>.zip    # 최신 zip 하나만 쓴다

~/repos/gh/lifetract/                ← 본 repo (코드, 빌드, 문서)
~/repos/gh/self-tracking-data/       ← 데이터 SSOT, 별도 repo (private)
    ├── samsunghealth_gtgkjh/            # CSV export — 폴더는 언제나 하나
    ├── atimelogger/database.db3         # 단일 SQLite (덮어쓰기)
    └── lifetract.db                     # 빌드 결과 (CSV+ATL → 통합 DB)
```

**최신 데이터: 2026-07-13** (Samsung export 2026-07-14 덤프, `lifetract.db` 203,539 rows).
*이 줄은 `./run.sh update` 할 때마다 갱신한다 — 날짜는 폴더명이 아니라 여기에 산다.*

**Samsung 폴더는 하나만 둔다.** export 는 언제나 전체 이력(2017-03~)을 담은 누적
덤프라 옛 폴더를 남길 이유가 없다. 그리고 남기면 **터진다** — Samsung 이 export 시각을
파일명에 박기 때문에(`com.samsung.shealth.sleep.20260714110176.csv`) 두 세대가 한 폴더에
섞이면 glob 순서상 **옛 CSV 가 먼저 잡힌다.** `./run.sh update` 가 교체 전에 폴더를
지우고, `newestCSV()` 가 그래도 최신을 고른다 (`TestNewestExportWins`).

코드 repo 안에 절대 데이터를 두지 않는다. CSV/DB 모두 위 경로 기준.

### 세 입력 스트림

| 소스 | 형식 | 기간 | 갱신 방식 |
|---|---|---|---|
| Samsung Health | CSV export (8 종) | 2017-03 ~ | 폰에서 수동 export → 폴더 배치 → `./run.sh update` |
| aTimeLogger | SQLite (`database.db3`) | 2021-10 ~ | 폰에서 .eml 백업 → 데이터 디렉토리 교체 → `./run.sh update` |
| Home Assistant REST | live state + recorder (**30일만 보관**) | 2026-05-17 ~ | 폰 Companion App → HA → on-demand fetch. SSOT 아닌 라이브 인터페이스. |

### 🔴 우선순위 — Samsung 이 본(本), HA 는 보조

**Samsung Health export 가 SSOT 다. HA 가 이긴 적은 없다.**

| | Samsung export | HA REST |
|---|---|---|
| 보관 | **영구** (9년치) | **30일** — 그 뒤 recorder 가 지운다 |
| 수면 | 시작·끝·길이 + **점수 · 효율 · 단계(deep/light/rem)** | 시작·끝·길이 **만** |
| 성격 | 주기적 덤프 (사람이 넣어준다) | 오늘 자리 라이브 인터페이스 |

HA 는 *DB 가 빈 자리만* 채운다 (phase 7 read-only fallback). Samsung row 를 덮지
않는다. 나중에 HA→DB 흡수를 만들더라도 `source='ha_rest'` 로 들어가고 **`samsung_health`
row 를 이기지 못한다.** HA 로 끌어온 값은 *덜 아는 값* 이다.

### 🔴 가끔은 사람이 데이터를 넣어줘야 한다 — 낡으면 말한다

Samsung export 는 자동으로 흐르지 않는다. **폰에서 손으로 내보내야 한다.**
그 사실 자체가 이 시스템의 구조다 — 그러니 도구는 *언제 마지막으로 받았는지* 를
알고, 낡으면 **먼저 말해야 한다.**

- 새 export 는 `~/sync/family/lifedata/samsunghealth_gtgkjh_<타임스탬프>.zip`
  (Syncthing 으로 폰 → thinkpad). 최신 zip 을 풀어 `self-tracking-data/` 에 두고
  `./run.sh update`.
- `lifetract status` 가 스트림별 `last_*` + `stale_days` + `warnings[]` 를 낸다
  (§3.5 계약 4항). **`warnings` 가 비어 있지 않으면 폰 export 가 밀린 것이다.**
- 에이전트는 건강 수치를 사실로 기록하기 전에 `status` 를 본다. **적은 숫자가
  "그날 아무것도 안 했다"는 뜻이 아니다.**

*선례: 2026-05-18 ~ 07-13 두 달간 export 가 멈춰 있었고 아무도 몰랐다. 모든 쿼리가
그냥 더 적은 숫자를 돌려주고 있었다. 그 침묵을 닫으려고 `status` 최신성이 있다.*

### DB-first / CSV-fallback

```
lifetract <command>
  └─ lifetract.db 있음?  → SQLite 쿼리 (~90ms) → JSON
                          → 없음 → CSV 직접 파싱 (~300ms) → JSON
                                   (aTimeLogger 는 CSV 모드 미지원)
  └─ today / read <오늘>  → DB miss/stale 자리는 HA REST 로 자동 채움 (read-only, JSON 에 ha_sources 노출)
```

DB 가 본 경로, CSV 는 안전망, HA 는 오늘 자리 라이브 메움. `lifetract import --exec` 가 두 SSOT 소스를 통합 DB 로 빌드 (~1.5초, 36MB). HA fallback 끄려면 `LIFETRACT_NO_HA=1`.

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

## 3.5 시간 계약 (Time Contract) — 불변

> 이 도구들을 만들 때 시간축 개념이 없었다. 당연히 구멍이 있었고, 2026-07-14 에
> timeline 관측소가 처음으로 *소비자* 로서 계약을 요구하면서 세 개가 한꺼번에
>드러났다. 셋 다 "조용히 틀린 답"이었다 — 에러도 경고도 없이 그냥 다른 숫자.
>
> 아래 다섯 조항은 **테스트로 강제된다** (`lifetract/timeaxis_test.go`,
> `lifetract/import_loss_test.go`).
> gitcli · denotecli 등 시간축을 갖게 될 다른 스킬은 이 판본을 베껴 쓴다.
>
> 계약의 뿌리는 하나다: **조용한 실패는 실패가 아니라 거짓말이다.** 1–3 항은 도구가
> 조용히 *틀린 숫자* 를 말하는 걸 막고, 4–5 항은 도구가 조용히 *없는 숫자* 를 말하는
> 걸 막는다.

**1. 고정 KST. 셸이 답을 바꾸지 못한다.**

`var KST = time.FixedZone("KST", 9*60*60)` 하나가 축이다. 패키지 어디서도
`time.Local` 을 쓰지 않는다. SQL 에서 SQLite `'localtime'` 수식어도 금지 —
그건 *호출한 셸의 `$TZ`* 를 읽는다. epoch → 날짜는 `'+9 hours'` 로 옮긴다.

```sql
DATE(start_time, 'unixepoch', '+9 hours')   -- O
DATE(start_time, 'unixepoch', 'localtime')  -- X: $TZ 따라 답이 달라진다
```

`LoadLocation("Asia/Seoul")` 대신 `FixedZone` 인 이유: 한국은 1988 이후 DST 가
없어 고정 오프셋이 정확하고, 정적 빌드에 tzdata 가 없어도 안 깨진다.

**2. 창은 반개방 `[from, to)`. 경계는 자정.**

`--to` 는 배타적이다. `--from 07-01 --to 07-08` = 7 일 (7/1~7/7). 이웃한 창이
겹치지도 새지도 않고 정확히 맞물린다.

경계는 **반드시 KST 자정**이다. "지금부터 N 일 전" 같은 한낮의 instant 를 경계로
쓰면 창의 가장 오래된 날이 잘려서 돌아온다 — `--days 3` 과 `--days 5` 가 같은
과거 날짜에 대해 다른 숫자를 낸다. **창은 렌즈지 편집이 아니다. 창을 넓힌다고
과거가 바뀌면 안 된다.**

재현이 필요한 질문에는 `--from/--to` 를 쓴다. `--days` 는 오늘 기준 상대라
내일이면 다른 질문이 된다.

**3. 블록은 시작일에 귀속. 자정을 넘어도 쪼개지 않는다.**

수면 `21:14 → 05:48` 은 전부 **시작한 날**의 것이다. 블록의 12% 가 자정을 넘으니
엣지 케이스가 아니라 상시다. timeline 관측소의 이벤트 귀속이 이 선례를 따르고
있으므로, 뒤집으려면 먼저 알려야 한다.

**4. 낡음은 스스로 신고한다.**

`status` 가 스트림별 최신 날짜와 `warnings` 를 낸다. 조용한 stale 은 금지 —
aTimeLogger 깊이가 두 달 밀려 있었는데 아무도 몰랐고, 모든 쿼리가 그냥 더 적은
숫자를 돌려주고 있었다. 스트림은 **각각** 본다: Samsung(CSV export)과
aTimeLogger(db3)는 경로가 달라 따로 멈춘다.

**5. 잃음도 스스로 신고한다. `import` 는 잃고서 "ok" 라고 하지 않는다.**

`import --exec` 는 스트림별 행수를 **직전 import 와 대조**하고, 잃었으면
`status: "warning"` + `warnings[]` 를 낸다 (`import_ledger.go`).

| 낱말 | 뜻 |
|---|---|
| `ok` | 읽었고, 지난번보다 줄지 않았다 |
| `empty` | 읽히긴 했는데 **0 행** — 지난번에 행이 있었다면 잃은 것 |
| `shrunk` | 지난 import 보다 **적다** (누적 덤프가 줄어드는 건 이상하다) |

- 베이스라인은 DB 안 `import_log` 원장이다. import 가 DB 를 지우므로 **지우기 전에
  읽어 새 DB 로 이월**한다 — 안 그러면 매번 첫 import 고, 첫 import 는 아무것도
  못 알아챈다. 0 행도 반드시 기록한다: **기록되지 않은 0 은 다음번에 못 알아채는 0 이다.**
- **한 import 는 `import_id` 하나다. `imported_at` 으로 묶지 마라.** 스탬프는 행마다
  `time.Now()` 였어서 한 import 가 초 경계를 넘으며 2~3 개로 쪼개졌다 (2회 import 가
  스탬프 5개). `GROUP BY imported_at` 으로 "직전 import" 를 재구성하면 **반쪽 run 과
  대조하게 되고, 그 틀린 기준은 조용하다.** 원장을 읽는 소비자는 `GROUP BY import_id`.
- **원장 읽기가 실패하면 첫 import 인 척하지 않는다.** `status: warning` +
  `ledger unreadable` — 검사기가 조용히 검사를 그만두는 것도 침묵이다.
- **프루닝하지 않는다.** 오래된 run 을 잘라내면 죽은 지 오래된 스트림의 *마지막 비영
  행수* 가 날아가고, 그 값이 바로 경고를 계속 울리게 하는 근거다. 잊는 원장은 우리가
  없애려는 그 침묵이다. import 당 9 행이니 기억할 여유가 있다.
- **임계값은 발명하지 않는다.** "지난번엔 있었고 지금은 없다"는 판단이 아니라 사실이라
  그것만 경고한다. 부분 하락은 모든 스트림에 `prev_rows`/`delta` 로 *숫자를 보여주고*,
  직전 import 대비 줄었을 때만 `shrunk` 로 말한다.
- **첫 import 는 경고하지 않는다** (비교 대상 없음). `note` 가 그렇게 말한다.

*선례: 2026-07-14, 글롭이 7MB stress 대신 1KB histogram 을 집어 27,598 행이 통째로 0 이
됐는데 import 는 `"ok"` 라고 했다. **테스트는 초록불이었다.** 잡은 건 총 행수가 203,539 →
175,941 로 떨어진 걸 사람이 눈으로 본 것뿐이다. 그 눈이 이 조항이다
(`import_loss_test.go`, 실 export 로 재현 검증).*

### 프라이버시 경계 — CLI 가 유일한 문

`atl_interval.comment` 에 **가족 이름이 평문**으로 있다 (150 블록). DB 에는 있고,
**CLI 가 그게 밖으로 나가는 유일한 문**이다. 어떤 SELECT 도 `comment` 를 읽지
않으며, 이건 우연이 아니라 계약이다 (`TestCommentNeverEscapes`).

`self-tracking-data` 가 private 인 것은 **리포 설정이지 계약이 아니다.**
지속시간과 카테고리는 나가도 되고, 코멘트는 안 된다. 원시 블록 노출을 언젠가
하더라도 코멘트는 *기본 제외* 가 아니라 *아예 나갈 수 없어야* 한다.

## 4. Commands (SSOT 는 [SKILL.md](SKILL.md))

상세 옵션·예시는 SKILL.md 가 권위. 여기는 손에 익혀야 할 최소만.

```bash
./run.sh build      # 빌드 + 설치 (~/.local/bin)
./run.sh update     # ~/repos/gh/self-tracking-data/<YYYYMMDD>/ 에서 데이터 배치 + DB 재빌드
./run.sh test       # go test ./...

lifetract status          # 데이터 소스 + DB 상태 + 최신성 warnings
lifetract today           # 오늘 통합 요약 (sleep, heart, steps, time 등)
lifetract read 2026-05-16 # 특정 날짜 종합
lifetract timeline --days 30
lifetract sleep   --days 7 --summary
lifetract time    --days 7 --category 본짓

# 재현 가능한 창 — 반개방 [from, to), --to 는 배타적 (§3.5)
lifetract time --from 2026-07-01 --to 2026-07-08   # 7일 (7/1~7/7)
```

JSON 출력이 기본. 에이전트는 stdout 그대로 파싱.

## 5. Operational workflow

### 평소 갱신 (현재 방식)

1. 폰에서 Samsung Health export + aTimeLogger 백업 받음
2. `~/repos/gh/self-tracking-data/<YYYYMMDD>/` 에 두 파일 배치
3. `./run.sh update` — Samsung Health 폴더 이동 + aTimeLogger DB 교체 + `lifetract import --exec`
4. `lifetract status` 로 갱신 확인 + 데이터 update log 정리

### 오늘 자리 호출 (라이브)

- `lifetract today` / `lifetract read <오늘>` — Samsung CSV 가 아직 안 들어온 자리는 자동으로 HA `GetState` (steps/heart_rate) + `GetHistory` (sleep_duration, 최근 36h 합산) 가 채움. `source: "db+ha"`, `ha_sources: [...]` 로 자리 노출.
- 토큰 자리: `pass show 2fa/totp/ha/junghanacs` → env `HA_TOKEN` → `~/.lifetract/ha.env`. HA 없으면 silent skip (기존 동작 유지).
- 과거 날짜는 enrichment 안 됨 — HA recorder 가 backfill 못 함.

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
