#!/usr/bin/env bash
# Install CATMonitor Agent Skills (skills/catmonitor-*) into a Claude Code
# skills directory.
#
# Usage:
#   scripts/install_skills.sh                  # user-level ~/.claude/skills
#   scripts/install_skills.sh --project        # project-level ./.claude/skills
#   scripts/install_skills.sh --dest DIR       # custom destination
#   scripts/install_skills.sh --list           # preview only, install nothing
#
# Also available as: make skills-install  /  make skills-install DEST=DIR
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SKILLS_ROOT="$(cd "${SCRIPT_DIR}/../skills" && pwd)"

DEST=""
MODE="user"
LIST_ONLY=0

usage() {
    cat <<'EOF'
Usage: scripts/install_skills.sh [--dest DIR | --project] [--list]

Install CATMonitor Agent Skills for Claude Code.

Options:
  --dest DIR    Install into DIR (created if missing).
  --project     Install into ./.claude/skills of the current directory.
  --list        Only list what would be installed; install nothing.
  -h, --help    Show this help.

Default (no options): user-level ~/.claude/skills.
EOF
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --dest)
            [ "$#" -ge 2 ] || { echo "error: --dest requires a value" >&2; exit 2; }
            DEST="$2"
            MODE="custom"
            shift 2
            ;;
        --project)
            MODE="project"
            shift
            ;;
        --list)
            LIST_ONLY=1
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "error: unknown option: $1" >&2
            usage >&2
            exit 2
            ;;
    esac
done

case "$MODE" in
    project) DEST="./.claude/skills" ;;
    user)    DEST="${HOME}/.claude/skills" ;;
    custom)  : ;;
esac

if [ ! -d "$SKILLS_ROOT" ]; then
    echo "error: skills root not found: $SKILLS_ROOT" >&2
    exit 1
fi

# Collect installable skill directories (skills/catmonitor-*).
SKILL_DIRS=()
for dir in "$SKILLS_ROOT"/*/; do
    [ -f "${dir}SKILL.md" ] || continue
    SKILL_DIRS+=("${dir%/}")
done

if [ "${#SKILL_DIRS[@]}" -eq 0 ]; then
    echo "error: no skills with SKILL.md found under $SKILLS_ROOT" >&2
    exit 1
fi

if [ "$LIST_ONLY" -eq 1 ]; then
    echo "Would install ${#SKILL_DIRS[@]} skill(s) into: $DEST"
    for dir in "${SKILL_DIRS[@]}"; do
        echo "  $(basename "$dir")  ->  $DEST/$(basename "$dir")"
    done
    exit 0
fi

echo "Installing ${#SKILL_DIRS[@]} skill(s) into: $DEST"
mkdir -p "$DEST"
for dir in "${SKILL_DIRS[@]}"; do
    name="$(basename "$dir")"
    target="$DEST/$name"
    # Replace any previous copy wholesale so removed files do not linger.
    rm -rf "$target"
    cp -r "$dir" "$target"
    echo "  installed: $name"
done

cat <<EOF

Done. Skills are active for new Claude Code sessions. Try prompts like:
  - "帮我体检一下这台服务器"        (catmonitor-health)
  - "查一下 NPU 的温度和利用率"     (catmonitor-metrics)
  - "最近有没有掉卡记录？"          (catmonitor-faults)

Uninstall: rm -rf $DEST/catmonitor-*
EOF
