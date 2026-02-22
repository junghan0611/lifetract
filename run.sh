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
        (cd "$SCRIPT_DIR/lifetract" && go build -o "$INSTALL_DIR/lifetract" .)
        echo "Installed: $INSTALL_DIR/lifetract"
        # Install skill to pi-skills
        SKILL_DIR="$HOME/.pi/agent/skills/pi-skills/lifetract"
        if [[ -d "$HOME/.pi/agent/skills/pi-skills" ]]; then
            mkdir -p "$SKILL_DIR"
            cp "$SCRIPT_DIR/SKILL.md" "$SKILL_DIR/SKILL.md"
            echo "Skill installed: $SKILL_DIR/SKILL.md"
        fi
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
