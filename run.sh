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
    -h|--help|help|"")
        cat <<'EOF'
lifetract — Life tracking CLI for AI agents

Usage:
  ./run.sh build [DIR]   Build lifetract, install to DIR (default: ~/.local/bin)
  ./run.sh test          Run all tests
  ./run.sh bench         Run benchmarks
EOF
        ;;
    *)
        echo "Unknown command: $1" >&2
        "$0" --help
        exit 1
        ;;
esac
