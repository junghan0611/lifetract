# NEXT.md — 다음 할 일

운영 baseline 은 [AGENTS.md](AGENTS.md). 결정의 의미축은 [docs/plan.md](docs/plan.md) + [[denote:20260517T211731]] (botlog). 본 NEXT 는 다음 한 걸음.

---

## ✅ 닫힘 — 시간 계약 (2026-07-14, 푸시·배포 완료)

timeline 관측소(`junghan0611/timeline`)가 첫 *소비자* 로 붙으면서 시간축 구멍이
드러났다. **커밋·푸시·배포 전부 완료** (`cd08b18` · `b636b6f` · `3157ff7`).
공유 바이너리 두 자리(`~/.claude/skills/lifetract/`, `agent-config`) 갱신됨.

- [x] **Samsung export 폴더를 하나로** (`samsunghealth_gtgkjh/`) — export 는 언제나
      전체 이력 누적 덤프라 세대별 폴더를 쌓을 이유가 없다. 옛 폴더 삭제 후 import
      가 **203,539 행으로 동일**함을 확인 (잃는 것 없음).
- [x] **`newestCSV()`** — 합치면서 함정 2개가 드러났다. (a) 한 폴더에 두 세대가 있으면
      glob 순서상 **옛 CSV 가 먼저** 잡힌다. (b) pattern 은 접두사라
      `stress.` 가 `stress.histogram`(1KB) 도 잡는다 — `matches[len-1]` 로 고쳤더니
      **stress 27,598 행이 통째로 0** 이 됐고 import 는 "ok" 라고 했다.
      `<pattern><숫자>.csv` 만 고른다. 둘 다 회귀 테스트.
- [x] **`run.sh update`** — Syncthing zip(`~/sync/family/lifedata`) → 고정 폴더 통째
      교체. 교체 전에 지우므로 두 세대가 안 섞인다.

- [x] **SQL `localtime` → `'+9 hours'`** — 셸 `$TZ` 가 날짜 귀속을 바꾸던 자리.
      코드베이스 유일한 `localtime` 이었고, 시연된 버그 전부가 여기서 나왔다.
- [x] **`cutoffTime` KST 자정 스냅** — 창 첫날이 잘려 있었다. `--days 3` 이 7/11 독서를
      77.7 로, `--days 5` 는 477.2 로 답하던 자리. 조용히 400분이 사라졌다.
- [x] **HA 라이브 축 stale** — heart_rate 센서가 **2026-07-03 에 112 로 얼어붙었는데**
      `GetState` 가 그걸 계속 돌려줬고, punchout 이 11일간 저널에 "심박 평균 112" 를
      박제했다. 신선도 가드 + `avg_hr` 을 진짜 평균(history)으로.
- [x] **`--from/--to` 반개방 `[from, to)`** / **`status` 스트림별 최신성 + warnings**
- [x] **AGENTS.md §3.5 시간 계약** — 고정 KST · 반개방 · 시작일 귀속 · 스스로 신고하는
      낡음 + comment 프라이버시 경계. gitcli·denotecli 가 베껴갈 판본.
- [x] **`lifetract/timeaxis_test.go`** (+ HA stale 2건) — 전부 *되돌려서 실패하는지*
      확인함. in-process 로는 TZ 결정성을 못 잡는다 (SQLite 가 존을 프로세스 시작 때
      고정) → 서브프로세스 × TZ 5개.

**관측소 검산 통과**: TZ 3개 동일 해시, 깊이 0 8,400건 diff 0, 골든 케이스 유지.
공유 바이너리 두 자리는 **안 건드림** (여전히 Jun 17 `e24a185f…`).

### 배포하면 뒤따르는 것

- 관측소 `collect.py` 가 `days+2` 여유 → `--from/--to` 로 갈아탄다 (관측소 쪽 작업).
- `~/.claude/skills/lifetract/` + `~/repos/gh/agent-config/skills/lifetract/` 두 자리.

## ✅ 닫힘 — 수면 축 (GLG 2026-07-14)

**갈림길은 GLG 가 닫았다: Samsung export 가 본(本), HA 는 보조.**

> "가끔은 데이터를 넣어줘야 된다. 마지막 임포트 시점에서 오래되면 말을 해줘.
> **HA 로 끌고온 데이터보다 이 데이터가 우선이야.**"

HA 를 재보니 그 판단이 맞았다 — 실측:

- **HA recorder 보관 = 30일** (60/90/180 요청해도 같은 답). 영구 저장소가 아니다.
- **HA 는 stages · score · efficiency 를 못 준다.** `sleep_segment` 로 start/end/
  duration 까지가 한계고, 중복 resync 도 나온다.
- 그래서 **HA→DB 흡수는 짓지 않는다.** HA 는 *오늘 자리* 라이브 fallback 으로 남는다.

- [x] **새 export 넣음** (2026-07-14) — `samsunghealth_gtgkjh_20260714110176.zip`
      → `self-tracking-data/`, `import --exec` (203,539 rows).
      **`05-18`~`06-12` 26일 구멍 메워짐** (43세션, stages 포함).
      2026 수면 **192/194일**, 세션 381건 **전부 stages**. 전 스트림 `07-13` 까지 신선,
      `warnings` 없음.
- [x] **AGENTS.md §2** — Samsung SSOT / HA 보조 우선순위 + "가끔은 사람이 넣어줘야
      한다, 낡으면 도구가 먼저 말한다" + zip 경로(`~/sync/family/lifedata/`).

## 도구 밖의 일 — 사람이 해야 함

- **HA heart_rate 센서가 2026-07-03 부터 죽어 있다** (112 고정). 폰↔HA 연동.
  *심박은 GLG 가 접었다 (2026-07-14) — 도구는 이제 죽은 값을 거부만 한다.*
- 저널 7/03~7/13 의 "심박 평균 112" — 센서 고장의 흔적. 정정 여부는 GLG 판단.
  (전례: 5월 week21 "HA 히스토리 기준 재산출" 보정 줄.)

## 닫힌 자리 (이전 turn)

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
