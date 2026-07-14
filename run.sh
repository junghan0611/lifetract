#!/usr/bin/env bash
# run.sh — lifetract 프로젝트 메인 진입점
#
# Usage:
#   ./run.sh build [INSTALL_DIR]   — Build lifetract + install
#   ./run.sh deploy                — build + ~/.local/bin 에 커밋된 바이너리만 반영
#                                    (스킬 자리는 agent-config 가 관리한다)
#   ./run.sh test                  — Run all tests
#   ./run.sh bench                 — Run benchmarks
#   ./run.sh update                — 덤프 배치 + DB 재빌드 (잃으면 exit 1)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

case "${1:-}" in
    build)
        echo "Building lifetract..."
        INSTALL_DIR="${2:-$SCRIPT_DIR/lifetract}"
        mkdir -p "$INSTALL_DIR"
        (cd "$SCRIPT_DIR/lifetract" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$INSTALL_DIR/lifetract" .)
        echo "Installed: $INSTALL_DIR/lifetract"
        echo "스킬 자리에도 반영하려면: ./run.sh deploy"
        ;;
    deploy)
        # 이 리포는 **바이너리 한 자리만** 배포한다: $BIN_DIR (기본 ~/.local/bin).
        #
        # 스킬 자리(바이너리 + SKILL.md)는 agent-config 가 단독으로 관리한다
        # (`agent-config/run.sh setup:build`, 테스트 게이트 포함). SSOT 는
        # agent-config/skills/lifetract/SKILL.md 하나다. 여기서 또 밀어 넣으면
        # 같은 자리를 두 리포가 쓰는 꼴이라, 어느 쪽이 참인지 아무도 모른다.
        #
        # 왜 여기서 뺐나 — 옛 판본은 스킬 자리를 두 개로 알고 "세 자리 SHA256 일치"를
        # 검사했다. 그런데 ~/.claude/skills 자체가 agent-config/skills 로 걸린 심링크라
        # 두 항목은 **같은 디렉토리 하나**였다. 즉 한 자리를 두 번 세고 세 자리를 봤다고
        # 말하던 검사다 — 하필 "검사가 검사인 척하는 것"을 죽이려고 만든 물건이.
        #
        # 남긴 것은 provenance 가드다. 이건 오늘 실제로 잘못된 배포를 막았다:
        #   dirty worktree 거부 · vcs.revision == HEAD · vcs.modified == false.
        # 배포된 숫자에 대응하는 커밋이 없으면 그 숫자는 출처가 없다.
        #
        # 세트 보장("바이너리와 SKILL.md 는 따로 움직이지 않는다")은 버린 게 아니라
        # agent-config 매니저 층으로 올렸다. 거기서 다섯 바이너리 전부에 걸린다.
        BIN_DIR="${LIFETRACT_BIN_DIR:-$HOME/.local/bin}"

        if [ -n "$(git -C "$SCRIPT_DIR" status --porcelain)" ]; then
            echo "❌ 작업 트리가 dirty 하다 — 커밋 전 바이너리를 배포하지 않는다." >&2
            echo "   배포된 숫자에 대응하는 커밋이 없으면 provenance 가 끊긴다." >&2
            git -C "$SCRIPT_DIR" status --short >&2
            exit 1
        fi

        HEAD_SHA=$(git -C "$SCRIPT_DIR" rev-parse HEAD)

        # 검증 전에는 운영 바이너리를 건드리지 않는다.
        #
        # 옛 판본은 $BIN_DIR 로 곧장 빌드한 뒤 provenance 를 검사했다. 검사가 실패하면
        # exit 1 이지만 **이미 덮은 뒤**다 — 거부된 바이너리가 운영 자리에 앉은 채로.
        # 이건 import 가 후보에 짓고 성한 run 만 승격하는 것과 같은 규율이다:
        # 임시 자리에 짓고, 검증하고, 통과한 것만 원자적으로 옮긴다.
        # staging 은 $BIN_DIR 안에 둔다 — mv 가 원자적 교체이려면 같은 파일시스템이어야
        # 한다. /tmp 에 지으면 mv 는 복사+삭제가 되고, 그 중간에 반쪽짜리 바이너리가
        # 운영 자리에 보인다.
        mkdir -p "$BIN_DIR"
        STAGE=$(mktemp -d "$BIN_DIR/.lifetract-stage.XXXXXX")
        trap 'rm -rf "$STAGE"' EXIT

        echo "🔨 build → (staging)  (HEAD ${HEAD_SHA:0:12})"
        (cd "$SCRIPT_DIR/lifetract" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$STAGE/lifetract" .)

        # 바이너리가 스스로 무엇인지 말하게 한다 (go version -m).
        # go version -m 은 `build\tvcs.revision=<sha>` 처럼 key=value 를 한 필드로 준다.
        BIN_INFO=$(go version -m "$STAGE/lifetract")
        BIN_REV=$(printf '%s' "$BIN_INFO" | sed -n 's/.*vcs\.revision=\([0-9a-f]*\).*/\1/p')
        BIN_MOD=$(printf '%s' "$BIN_INFO" | sed -n 's/.*vcs\.modified=\([a-z]*\).*/\1/p')
        if [ "$BIN_REV" != "$HEAD_SHA" ] || [ "$BIN_MOD" != "false" ]; then
            echo "❌ 빌드가 HEAD 를 안 담았다: revision=${BIN_REV:0:12} modified=$BIN_MOD" >&2
            echo "   운영 바이너리는 그대로다 ($BIN_DIR/lifetract)." >&2
            exit 1
        fi
        echo "   ✓ vcs.revision=${BIN_REV:0:12} vcs.modified=false"

        # 통과했으니 이제 옮긴다 (같은 파일시스템 → rename, 원자적 교체).
        mv "$STAGE/lifetract" "$BIN_DIR/lifetract"

        WANT_BIN=$(sha256sum "$BIN_DIR/lifetract" | awk '{print $1}')
        echo "   ✓ ${WANT_BIN:0:8}  $BIN_DIR/lifetract"

        echo ""
        echo "   배포본 fingerprint (관측소 manifest 용):"
        echo "     tool_sha256=$WANT_BIN"
        echo "     tool_vcs_revision=$HEAD_SHA"
        echo "     tool_vcs_modified=false"
        echo ""
        echo "   스킬 자리(바이너리 + SKILL.md)는 agent-config 가 배포한다:"
        echo "     cd ~/repos/gh/agent-config && ./run.sh setup:build"
        ;;
    test)
        echo "Running tests..."
        (cd "$SCRIPT_DIR/lifetract" && go test -v ./...)
        ;;
    bench)
        echo "Running benchmarks..."
        (cd "$SCRIPT_DIR/lifetract" && go test -bench=. -benchtime=3s -v)
        ;;
    update)
        # 폰이 Syncthing 으로 떨군 최신 덤프를 받아 배치하고 DB 재빌드.
        #
        # Samsung export 는 자동으로 흐르지 않는다 — 사람이 폰에서 내보내야 한다.
        # 그래서 여기 오는 건 가끔이고, 그 사이 데이터는 낡는다. `lifetract status`
        # 가 낡음을 신고한다 (AGENTS.md §3.5).
        DATA_DIR="$HOME/repos/gh/self-tracking-data"
        DROP_DIR="${LIFEDATA_DIR:-$HOME/sync/family/lifedata}"
        LIFETRACT="${LIFETRACT_BIN:-$HOME/.local/bin/lifetract}"

        echo "📂 덤프 위치: $DROP_DIR"

        # 1. Samsung Health — 최신 zip 을 풀어 고정 폴더를 통째로 교체
        #
        # 폴더는 하나만 둔다. Samsung 은 export 시각을 파일명에 박기 때문에
        # (com.samsung.shealth.sleep.20260714110176.csv), 새 CSV 를 옛 CSV 옆에
        # 그냥 풀면 두 세대가 한 폴더에 섞인다. 그래서 교체 전에 지운다.
        # export 는 언제나 전체 이력을 담은 누적 덤프라 옛 폴더를 버려도 잃는 게 없다.
        SHEALTH_ZIP=$(ls -1t "$DROP_DIR"/samsunghealth_*.zip 2>/dev/null | head -1)
        if [ -n "$SHEALTH_ZIP" ]; then
            echo "📦 최신 Samsung export: $(basename "$SHEALTH_ZIP")"
            TMP=$(mktemp -d)
            trap 'rm -rf "$TMP"' EXIT
            unzip -q "$SHEALTH_ZIP" -d "$TMP"

            INNER=$(find "$TMP" -maxdepth 1 -type d -name 'samsunghealth_*' | head -1)
            if [ -z "$INNER" ]; then
                echo "❌ zip 안에 samsunghealth_* 폴더가 없습니다" >&2
                exit 1
            fi
            # samsunghealth_gtgkjh_20260714110176 → samsunghealth_gtgkjh
            STABLE=$(basename "$INNER" | sed -E 's/_[0-9]{14,}$//')

            rm -rf "${DATA_DIR:?}/$STABLE"
            mv "$INNER" "$DATA_DIR/$STABLE"
            echo "✅ Samsung Health 교체: $STABLE/ ($(find "$DATA_DIR/$STABLE" -type f | wc -l) 파일)"
        else
            echo "⚠️  Samsung Health zip 없음 (스킵): $DROP_DIR/samsunghealth_*.zip"
        fi

        # 2. aTimeLogger — 같은 덤프 폴더에서 SQLite 를 찾아 교체.
        # .eml 은 확장자만 메일이고 실제로는 SQLite 라 헤더로 판별한다.
        # .atl2bkp(XML) 은 손대지 않는다 — 가족 이름이 평문으로 들어 있다.
        ATL_UPDATED=false
        for candidate in $(ls -1t "$DROP_DIR"/aTimeLogger*.eml "$DROP_DIR"/*.db3 2>/dev/null); do
            [ -f "$candidate" ] || continue
            if head -c 16 "$candidate" 2>/dev/null | grep -q "SQLite format"; then
                mkdir -p "$DATA_DIR/atimelogger"
                cp "$candidate" "$DATA_DIR/atimelogger/database.db3"
                echo "✅ aTimeLogger DB 갱신: $(basename "$candidate")"
                ATL_UPDATED=true
                break
            fi
        done
        if ! $ATL_UPDATED; then
            echo "⚠️  aTimeLogger SQLite 파일 없음 (스킵): $DROP_DIR"
        fi

        # 3. lifetract import --exec
        if [ -x "$LIFETRACT" ]; then
            echo ""
            echo "🔨 lifetract import --exec ..."
            IMPORT_OUT=$("$LIFETRACT" import --exec)
            echo "$IMPORT_OUT"
            echo ""
            echo "📊 lifetract status:"
            "$LIFETRACT" status

            # import 가 스트림을 잃었으면 여기서 멈춘다 (AGENTS.md §3.5 5항).
            # 운영 DB 는 승격되지 않았으니 조회는 여전히 직전의 성한 DB 를 읽는다.
            # 조용히 다음 줄로 넘어가는 것이 2026-07-14 에 stress 27,598 행을 죽인 채로
            # 배포할 뻔한 그 침묵이다.
            if printf '%s' "$IMPORT_OUT" | grep -q '"status": "warning"'; then
                echo ""
                echo "❌ import 가 성하지 않다:"
                printf '%s' "$IMPORT_OUT" | grep -E '^\s+"[a-z_]+: |ledger|untouched' || true
                echo ""
                echo "   운영 DB 는 그대로다 (승격 안 됨) — 조회는 직전의 성한 DB 를 읽는다."
                echo "   export zip 이 온전한지 먼저 봐라. 후보 DB 는 candidate_path 에 남아 있다."
                exit 1
            fi
        else
            echo "⚠️  lifetract 바이너리 없음: $LIFETRACT"
            echo "   ./run.sh build 후 다시 시도하세요"
        fi
        ;;
    -h|--help|help|"")
        cat <<'EOF'
lifetract — Life tracking CLI for AI agents

Usage:
  ./run.sh build [DIR]   Build lifetract, install to DIR (default: ./lifetract/)
  ./run.sh deploy        build + ~/.local/bin (커밋된 빌드만; provenance 강제)
                         스킬 자리는 agent-config ./run.sh setup:build 가 맡는다
  ./run.sh test          Run all tests
  ./run.sh bench         Run benchmarks
  ./run.sh update        Syncthing 덤프에서 데이터 배치 + DB 재빌드
                         (import 가 스트림을 잃으면 exit 1)
EOF
        ;;
    *)
        echo "Unknown command: $1" >&2
        "$0" --help
        exit 1
        ;;
esac
