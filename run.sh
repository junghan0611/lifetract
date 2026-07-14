#!/usr/bin/env bash
# run.sh — lifetract 프로젝트 메인 진입점
#
# Usage:
#   ./run.sh build [INSTALL_DIR]   — Build lifetract + install + copy skill
#   ./run.sh test                  — Run all tests
#   ./run.sh bench                 — Run benchmarks
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

case "${1:-}" in
    build)
        echo "Building lifetract..."
        INSTALL_DIR="${2:-$SCRIPT_DIR/lifetract}"
        mkdir -p "$INSTALL_DIR"
        (cd "$SCRIPT_DIR/lifetract" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$INSTALL_DIR/lifetract" .)
        echo "Installed: $INSTALL_DIR/lifetract"
        echo "Skill docs: https://github.com/junghan0611/pi-skills/tree/main/lifetract"
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
            "$LIFETRACT" import --exec
            echo ""
            echo "📊 lifetract status:"
            "$LIFETRACT" status
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
  ./run.sh test          Run all tests
  ./run.sh bench         Run benchmarks
  ./run.sh update        날짜 폴더(YYYYMMDD)에서 데이터 배치 + DB 재빌드
EOF
        ;;
    *)
        echo "Unknown command: $1" >&2
        "$0" --help
        exit 1
        ;;
esac
