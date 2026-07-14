# NEXT.md — 다음 할 일

운영 baseline 은 [AGENTS.md](AGENTS.md). 결정의 의미축은 [docs/plan.md](docs/plan.md) + [[denote:20260517T211731]] (botlog). 본 NEXT 는 다음 한 걸음.

---

## 🔶 커밋 대기 — 잃음 신고 (2026-07-14, 관측소 요청건)

**`import` 가 스트림을 통째로 잃고도 `"ok"` 라고 하던 자리를 닫았다.** 오늘 아침
stress 27,598 → 0 을 잡은 건 테스트가 아니라 *사람 눈에 띈 총 행수* 였다 (203,539 →
175,941). 그 눈을 도구 안으로 넣었다. **작업 트리에만 있음 — 커밋·푸시는 GLG.**

- [x] **`import_ledger.go`** — 직전 import 행수를 `import_log` 원장에서 읽어 대조.
      `status: ok|warning`, 스트림별 `empty`/`shrunk` + `prev_rows`/`delta`,
      `prev_total_rows`. 형제 제안의 전제 하나를 뒤집었다: 원장은 **매 import 마다
      DB 와 함께 지워지고 있었고**(`os.Remove`), 0 행은 **아예 기록되지 않았다**
      (`if rows > 0`). 그래서 지우기 전에 읽고, 새 DB 로 이월하고, 0 도 적는다.
- [x] **`import_loss_test.go`** (6건) — 조용한 0(파싱 성공·0행) / 시끄러운 0(CSV 실종) /
      다음 import 에도 계속 경고 / shrunk / 첫 import 는 경고 안 함 / 원장 생존.
      **되돌려서 실패 확인** — 판단·이월 각각 되돌려 `status="ok", want="warning"` 재현.
- [x] **실 export 로 재현** — 심링크 농장에 histogram 을 stress 자리에 놓으니
      `warning` + `stress: 27,598 → 0` + `175,941 / prev 203,539`. **아침 그 숫자 그대로.**
      실 DB 정상 경로는 `ok`, 전 스트림 delta 0, 원장 이월 확인(9+9=18행).
- [x] **`run.sh deploy`** — 스킬 두 자리에 **바이너리 + SKILL.md 를 세트로** 반영 + 해시
      확인. 뿌리 원인: `build` 주석은 "copy skill" 이라 말했지만 아무도 안 옮겼고,
      **SKILL.md 가 5-26 자에서 멈춰 있었다** (시간 계약도 `--from/--to` 도 없는 판본).
- [x] **`run.sh update`** — import 가 잃으면 `exit 1`. 조용히 다음 줄로 안 넘어간다.
- [x] **AGENTS.md §3.5 5항 신설** — "잃음도 스스로 신고한다" (gitcli·denotecli 가 베낄 판본).
      계약의 뿌리 한 줄: *조용한 실패는 실패가 아니라 거짓말이다.*
- [x] **낡은 숫자 청소** — dry-run 이 aTimeLogger 를 `"13,094 intervals"` 로 **하드코딩**
      하고 있었다 (실제 14,617). 이제 세보고 말한다. SKILL.md `status` 예시에 최신성
      필드(`last_*`/`stale_days`/`warnings`)가 통째로 빠져 있던 것도 채움.

### 관측소 검산에서 나온 것 (2026-07-14, 교차 리뷰)

관측소가 코드·실물로 검산해 **데이터에 앉는 함정 하나**를 잡아줬다. 커밋 전에 닫았다.

- [x] **`import_id` 신설** — `imported_at` 은 import 식별자가 **아니었다.** `logImport` 가
      행마다 `time.Now()` 를 찍어 한 import 가 초 경계를 넘으며 쪼개졌다 (실측: import
      2회 → 스탬프 5개, 첫 것은 3개로 분열). 지금 로직은 `ORDER BY id` 라 안전했지만,
      원장을 읽는 **다음 소비자**가 `GROUP BY imported_at` 하면 반쪽 run 과 대조하게
      되고 그 틀린 기준은 조용하다. run 당 도장 하나 + `import_id`.
      **스키마 함정은 데이터에 앉으면 못 뺀다** — 그래서 커밋 전에.
- [x] **옛 원장 이관** — 옛 스탬프는 **고쳐 쓰지 않는다** (기록된 사실을 발명하지 않음).
      구조로 run 경계를 복원한다 (한 run 은 스트림을 한 번만 적는다 → 반복 = 새 run).
      실 DB 확인: 스탬프 3개짜리 · 2개짜리가 각각 run 1 · run 2 로 복원, 합계 203,539 동일.
- [x] **옛 스키마 DB 도 읽는다** — `import_id` 없는 DB 에서 베이스라인이 날아가면 다음
      import 가 자기를 첫 import 라 부르며 **손실 검사를 통째로 건너뛴다.** fallback 쿼리.
- [x] **🔴 내 안전망의 침묵 (테스트가 스스로 파냄)** — 원장 읽기가 실패하면 조용히 빈
      베이스라인을 돌려주고 있었다. **검사기가 조용히 검사를 그만두는 것도 침묵이다.**
      이제 `status: warning` + `ledger unreadable — no loss check this run`.
- [x] **시계 주입** (`nowStamp`) — 3행짜리 목은 한 초 안에 끝나서 **되돌린 코드도 테스트를
      통과했다.** 눈금이 도는 시계를 넣어 결정적으로 잡히게 함 (`run 1: 9 distinct
      imported_at, want 1`). *목이 현실을 안 닮으면 되돌리기 검증도 통과한다* — 관측소 말대로.
- [x] **프루닝 안 함** (관측소 발견 3) — 의도된 선택. 오래된 run 을 자르면 죽은 스트림의
      *마지막 비영 행수* 가 날아가고, 그게 경고를 계속 울리는 근거다. 잊는 원장은 침묵이다.

### 2차 검산에서 나온 것 (관측소, 2026-07-14 오후) — 여섯 중 다섯 닫힘

- [x] **🔴 잃은 DB 를 운영에 승격하고 있었다** (`de94794`). "잃었다고 말한다"만 닫고
      "잃은 DB 를 넘기지 않는다"는 안 닫혀 있었다 — `os.Remove(path)` 가 먼저 도니
      경고가 찍힐 때쯤엔 성한 DB 가 이미 없었다. **후보 → 검증 → 원자적 승격**
      (`promoteDB`, WAL checkpoint 후 rename). 승격을 막는 것 셋: 스트림 손실 /
      못 읽은 소스 / 원장 기록 실패. *행동 전에 도착하지 않는 경고는 묘비명이다.*
- [x] **🟠 원장이 내부 오류를 삼켰다** — `rows.Scan` continue, `rows.Err()` 미확인,
      `carryForward`/`logImport` 오류 무시. 전부 판정에 반영. **반쪽 원장은 없는 원장보다
      나쁘다** (없는 쪽은 스스로를 알린다). `initSchema` 를 var 로 열어 실패 경로를 테스트.
- [x] **🟠 첫 import 에서 "소스를 못 읽음"이 ok 였다** — "비교할 게 없다"는 손실을 주장하지
      않을 이유지, **못 읽은 소스를 성하다고 부를 이유가 아니었다.** 목도 현실을 닮게 고침
      (weight·hrv fixture + 진짜 aTimeLogger DB).
- [x] **🔴 deploy 가 해시를 출력만 했다** (`1e38ec4`, `8e3471d`) — 검사가 아니라 검사처럼
      보이는 출력. 실제로 `~/.local/bin` 은 미커밋 빌드(`210ae55` dirty)인 채 스킬 자리만
      갱신돼 있었고 **나는 그 출력을 증거로 읽었다.** 이제 dirty 트리 거부 ·
      `vcs.revision == HEAD` · `vcs.modified == false` · 세 자리 SHA256 일치를 **강제**.
- [x] **🟡 dry-run 이 import 행수인 척했다** (`25a1a70`) — 225,364 vs 203,539. `total_rows`
      → `raw_source_rows` + source 별 `imported` 플래그. **한 낱말이 두 숫자를 가리키면
      도구는 실수로 거짓말을 시작한다.**
- [x] **문서 정합** (`00a7f23`) — SKILL.md 가 "계약 넷"이라 말하고 AGENTS.md 는 다섯이었다.
      Data Coverage 2026-05-19 / 14,331 → 2026-07-13 / 14,617 실측.

**배포 provenance 닫힘**: 세 자리 전부 `8e3471d` clean (`vcs.modified=false`),
`tool_sha256=424c7be7…`. `run.sh deploy` 가 fingerprint 를 찍어 준다.

### 남은 것

- **관측소 쪽 (내 리포 아님)**: `timeline/collect.py` 의 manifest 에 lifetract fingerprint
  (`tool_sha256` / `tool_vcs_revision` / `tool_vcs_modified`) 추가. `code_sha256` 은
  collector 파일 하나만 고정하므로 **어느 lifetract 바이너리로 뽑았는지 snapshot 에
  안 남는다.** 첫 public projection 전에 닫아야 한다. deploy 가 그 세 값을 출력한다.
- **프루닝 안 함** (검산 발견 3) — 의도. 오래된 run 을 자르면 죽은 스트림의 마지막 비영
  행수가 날아가고, 그게 경고를 계속 울리는 근거다. 잊는 원장은 침묵이다.

## 🔶 커밋 대기 — 빈 것과 배포면 (2026-07-14, agent-config 검수건)

agent-config 매니저가 스킬 면을 검수하며 결함 둘 + GLG 결정 하나를 보냈다. 셋 다 닫았다.
**SKILL.md 수정은 agent-config 작업 트리에 남겨뒀다 — 커밋은 그쪽 매니저가 한다.**

- [x] **빈 창이 `null` 이었다** — `exercise --days 30` 이 `null` 을 내서
      `for x in json.loads(out)` 이 `TypeError` 로 죽었다. **영(零)을 구멍으로 내보내면
      "운동을 안 했다"와 "도구가 깨졌다"가 구별되지 않는다.** 출력 직전 한 관문
      (`main.go:emptyList`)에서 nil 슬라이스를 `[]` 로 막는다 — 커맨드마다 막으면 다음
      커맨드가 또 뚫는다. `time` 의 `{"hint":…}` 맵도 배열로 통일하고, staleness 신호는
      **stderr** 로 살렸다. DB 가 없으면 `[]` 가 아니라 **에러 + exit 1** — "안 썼다"와
      "못 봤다"는 같은 모양으로 나가면 안 된다.
- [x] **`warnings` 가 omitempty 라 비면 키째 사라졌다** — 문서가 보여주는 `"warnings": []`
      가 **나올 수 없는 출력**이었고, 계약 4항의 jq 는 `null` 을 받았다. omitempty 를 빼고
      **non-nil 초기화까지** 했다 (omitempty 만 빼면 `"warnings": null` 이라 병이 그대로다).
      `candidate_path` 의 omitempty 는 유지 — 그 부재는 "승격됨"을 실어 나른다.
- [x] **스킬 면을 `run.sh deploy` 에서 뺐다** (GLG 결정) — SSOT 는
      `agent-config/skills/lifetract/SKILL.md` 하나. 리포의 SKILL.md 삭제, 배포는
      `~/.local/bin` 한 자리만. 뿌리: `~/.claude/skills` 자체가 agent-config/skills 로 걸린
      **심링크**라 옛 `SKILL_DIRS` 두 항목은 같은 디렉토리였다 — **"세 자리 SHA256 일치"가
      한 자리를 두 번 세고 있었다.** 하필 "검사가 검사인 척하는 것"을 죽이려던 물건이.
      provenance 가드(dirty 거부 · `vcs.revision == HEAD` · `vcs.modified == false`)는 유지.
- 검증: 90개 통과, vet·race 초록, TZ 3개(UTC/KST/NY) 동일 해시. 실 데이터로 6개 커맨드
  전부 python 루프 통과.

### 검수 중 나온 별건 (안 고침 — 판단 필요)

- **`--days` 가 `--to` 앞에서 조용히 무시된다.** `--days 3/30/3000 --to 2026-07-01` 이
  전부 같은 3,114 행. `flagRange` 가 `--from` 없으면 하한을 1970-01-01 로 열기 때문
  (`config.go:46`, 의도된 설계). 그런데 **`--days N --to X` 를 N일 창으로 읽는 것이
  자연스러운 기대**라 조용한 무시는 함정이다. 셋 중 하나: (a) `--from` 없을 때 `--days`
  를 하한으로 쓴다, (b) 조합을 거부한다, (c) 문서에 못박는다.
- **센티널 타임스탬프 14행** — `heart_rate` 에 1970-01-01(13행) · 2000-01-01(1행).
  Samsung export 산 쓰레기 값이고, 하한이 열린 질의에 그대로 딸려 나온다
  (2017년부터의 나머지 64,541행은 정상 이력). import 에서 걸러낼지 판단 필요.

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
