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
        INSTALL_DIR="${2:-$HOME/.local/bin}"
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
        # 날짜 폴더(YYYYMMDD)에서 데이터를 꺼내 lifetract가 인식하는 위치로 배치 후 DB 재빌드
        DATA_DIR="$HOME/repos/gh/self-tracking-data"
        LIFETRACT="${LIFETRACT_BIN:-$HOME/.local/bin/lifetract}"

        # 최신 날짜 폴더 찾기
        LATEST_DATE=$(find "$DATA_DIR" -maxdepth 1 -type d | grep -E '/[0-9]{8}$' | sort | tail -1)
        if [ -z "$LATEST_DATE" ]; then
            echo "❌ 날짜 폴더(YYYYMMDD)가 없습니다: $DATA_DIR" >&2
            exit 1
        fi
        echo "📂 최신 날짜 폴더: $LATEST_DATE"

        # 1. Samsung Health — samsunghealth_* 폴더를 데이터 루트로 이동
        SHEALTH_DIR=$(find "$LATEST_DATE" -maxdepth 1 -type d -name 'samsunghealth_*' | head -1)
        if [ -n "$SHEALTH_DIR" ]; then
            TARGET="$DATA_DIR/$(basename "$SHEALTH_DIR")"
            if [ -d "$TARGET" ]; then
                echo "⏭️  Samsung Health 이미 존재: $(basename "$SHEALTH_DIR")"
            else
                mv "$SHEALTH_DIR" "$DATA_DIR/"
                echo "✅ Samsung Health 이동: $(basename "$SHEALTH_DIR")"
            fi
        else
            echo "⚠️  Samsung Health 폴더 없음 (스킵)"
        fi

        # 2. aTimeLogger — SQLite DB 또는 .eml(실제 SQLite) 찾아서 교체
        ATL_UPDATED=false
        for candidate in "$LATEST_DATE"/aTimeLogger*.eml "$LATEST_DATE"/*.db3 "$LATEST_DATE"/database.db3; do
            [ -f "$candidate" ] || continue
            HEADER=$(head -c 16 "$candidate" 2>/dev/null || true)
            if echo "$HEADER" | grep -q "SQLite format"; then
                mkdir -p "$DATA_DIR/atimelogger"
                cp "$candidate" "$DATA_DIR/atimelogger/database.db3"
                echo "✅ aTimeLogger DB 갱신: $(basename "$candidate")"
                ATL_UPDATED=true
                break
            fi
        done
        if ! $ATL_UPDATED; then
            echo "⚠️  aTimeLogger SQLite 파일 없음 (스킵)"
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
  ./run.sh build [DIR]   Build lifetract, install to DIR (default: ~/.local/bin)
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
