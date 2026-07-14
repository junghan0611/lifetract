#!/usr/bin/env bash
# run.sh — lifetract 프로젝트 메인 진입점
#
# Usage:
#   ./run.sh build [INSTALL_DIR]   — Build lifetract + install
#   ./run.sh deploy                — build + 스킬 자리에 바이너리·SKILL.md 세트 반영
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
        # 스킬 자리는 **바이너리 + SKILL.md 가 한 세트**다. 둘이 따로 움직이면
        # 에이전트는 없는 기능을 모르거나, 있는 계약을 어긴다.
        #
        # 2026-07-14 에 실제로 그랬다: 바이너리만 손으로 복사해 오는 동안 SKILL.md 는
        # 5-26 자에서 멈춰 있었다. 코드는 KST 고정 · 반개방 창 · stale 신고를 지키는데,
        # 에이전트가 읽는 문서에는 --from/--to 도 시간 계약도 통째로 없었다.
        # 지켜지는 줄 아무도 모르는 계약은 없는 계약이다. 그래서 복사를 손에서 뺀다.
        # 해시를 출력만 하던 판본이 있었다. 그건 검사가 아니라 검사처럼 보이는 출력이고,
        # 실제로 ~/.local/bin 은 미커밋 빌드인 채 스킬 자리만 갱신돼 있었다 —
        # 그리고 나는 그 출력을 증거로 읽었다. 이제 강제한다:
        #   dirty worktree 거부 · vcs.revision == HEAD · vcs.modified == false ·
        #   세 자리 SHA256 일치. 하나라도 어긋나면 배포는 실패한다.
        BIN_DIR="${LIFETRACT_BIN_DIR:-$HOME/.local/bin}"
        SKILL_DIRS=(
            "$HOME/.claude/skills/lifetract"
            "$HOME/repos/gh/agent-config/skills/lifetract"
        )

        if [ -n "$(git -C "$SCRIPT_DIR" status --porcelain)" ]; then
            echo "❌ 작업 트리가 dirty 하다 — 커밋 전 바이너리를 배포하지 않는다." >&2
            echo "   배포된 숫자에 대응하는 커밋이 없으면 provenance 가 끊긴다." >&2
            git -C "$SCRIPT_DIR" status --short >&2
            exit 1
        fi

        HEAD_SHA=$(git -C "$SCRIPT_DIR" rev-parse HEAD)

        echo "🔨 build → $BIN_DIR  (HEAD ${HEAD_SHA:0:12})"
        mkdir -p "$BIN_DIR"
        (cd "$SCRIPT_DIR/lifetract" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$BIN_DIR/lifetract" .)

        # 바이너리가 스스로 무엇인지 말하게 한다 (go version -m).
        # go version -m 은 `build\tvcs.revision=<sha>` 처럼 key=value 를 한 필드로 준다.
        BIN_INFO=$(go version -m "$BIN_DIR/lifetract")
        BIN_REV=$(printf '%s' "$BIN_INFO" | sed -n 's/.*vcs\.revision=\([0-9a-f]*\).*/\1/p')
        BIN_MOD=$(printf '%s' "$BIN_INFO" | sed -n 's/.*vcs\.modified=\([a-z]*\).*/\1/p')
        if [ "$BIN_REV" != "$HEAD_SHA" ] || [ "$BIN_MOD" != "false" ]; then
            echo "❌ 빌드가 HEAD 를 안 담았다: revision=${BIN_REV:0:12} modified=$BIN_MOD" >&2
            exit 1
        fi
        echo "   ✓ vcs.revision=${BIN_REV:0:12} vcs.modified=false"

        for d in "${SKILL_DIRS[@]}"; do
            if [ ! -d "$d" ]; then
                echo "⚠️  스킬 자리 없음 (스킵): $d"
                continue
            fi
            cp "$BIN_DIR/lifetract" "$d/lifetract"
            cp "$SCRIPT_DIR/SKILL.md" "$d/SKILL.md"
            echo "✅ $d ← lifetract + SKILL.md"
        done

        # 세트가 실제로 맞는지 강제한다. 하나라도 어긋나면 exit 1.
        echo ""
        echo "🔍 세트 검증:"
        WANT_BIN=$(sha256sum "$BIN_DIR/lifetract" | awk '{print $1}')
        WANT_DOC=$(sha256sum "$SCRIPT_DIR/SKILL.md" | awk '{print $1}')
        FAILED=0
        for d in "$BIN_DIR" "${SKILL_DIRS[@]}"; do
            [ -d "$d" ] || continue
            GOT=$(sha256sum "$d/lifetract" 2>/dev/null | awk '{print $1}')
            if [ "$GOT" != "$WANT_BIN" ]; then
                echo "   ❌ ${GOT:0:8} != ${WANT_BIN:0:8}  $d/lifetract" >&2
                FAILED=1
            else
                echo "   ✓ ${GOT:0:8}  $d/lifetract"
            fi
        done
        for d in "${SKILL_DIRS[@]}"; do
            [ -d "$d" ] || continue
            GOT=$(sha256sum "$d/SKILL.md" 2>/dev/null | awk '{print $1}')
            if [ "$GOT" != "$WANT_DOC" ]; then
                echo "   ❌ ${GOT:0:8} != ${WANT_DOC:0:8}  $d/SKILL.md" >&2
                FAILED=1
            else
                echo "   ✓ ${GOT:0:8}  $d/SKILL.md"
            fi
        done
        [ "$FAILED" -eq 0 ] || { echo "" >&2; echo "❌ 배포가 한 세트가 아니다." >&2; exit 1; }

        echo ""
        echo "   배포본 fingerprint (관측소 manifest 용):"
        echo "     tool_sha256=$WANT_BIN"
        echo "     tool_vcs_revision=$HEAD_SHA"
        echo "     tool_vcs_modified=false"
        echo ""
        echo "   agent-config 의 SKILL.md 는 git 추적 대상이다 — 커밋은 GLG 가 한다."
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
  ./run.sh deploy        build + 스킬 두 자리에 바이너리·SKILL.md 세트로 반영
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
