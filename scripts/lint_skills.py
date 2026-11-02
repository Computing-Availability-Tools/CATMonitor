#!/usr/bin/env python3
"""Validate SKILL.md frontmatter for CATMonitor Agent Skills.

Checks (see skills/README.md "编写规范"):
  1. Frontmatter block delimited by leading '---' lines exists and parses.
  2. `name` is present, kebab-case, and matches the skill directory name.
  3. `description` is present and 20-1024 chars (supports folded '>' blocks).
  4. Relative Markdown links inside SKILL.md point to existing files.

Intentionally dependency-free (no PyYAML): frontmatter here is restricted to
flat scalar keys and folded/literal blocks, which a small parser handles.

Usage: python3 scripts/lint_skills.py [--fix-hints]
Exit code: 0 if all skills pass, 1 otherwise.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"
MIN_DESC = 20
MAX_DESC = 1024
NAME_RE = re.compile(r"^[a-z0-9]+(-[a-z0-9]+)*$")
KEY_RE = re.compile(r"^([A-Za-z][A-Za-z0-9_-]*):\s*(.*)$")


def parse_frontmatter(text: str) -> dict[str, str] | None:
    """Parse a flat YAML-ish frontmatter block (scalars + folded blocks)."""
    lines = text.splitlines()
    if not lines or lines[0].strip() != "---":
        return None
    try:
        end = next(i for i in range(1, len(lines)) if lines[i].strip() == "---")
    except StopIteration:
        return None

    result: dict[str, str] = {}
    key: str | None = None
    for line in lines[1:end]:
        if key is not None and line[:1] in (" ", "\t"):
            # Continuation of a folded ('>') or literal ('|') block.
            result[key] += " " + line.strip()
            continue
        match = KEY_RE.match(line)
        if not match:
            continue  # Unknown shape (e.g. nested map); keep its key empty.
        key = match.group(1)
        value = match.group(2).strip()
        if value in (">", ">-", ">+", "|", "|-", "|+"):
            value = ""
        result[key] = value
    return result


def check_local_links(skill_dir: Path, text: str) -> list[str]:
    """Verify relative Markdown links resolve to existing files."""
    problems = []
    body_start = text.split("---", 2)[2] if text.startswith("---") else text
    for match in re.finditer(r"\]\(([^)]+)\)", body_start):
        target = match.group(1).split("#")[0]
        if not target or "://" in target or target.startswith("mailto:"):
            continue
        resolved = (skill_dir / target).resolve()
        if not resolved.exists():
            problems.append(f"broken relative link: ({target})")
    return problems


def lint_skill(skill_dir: Path) -> list[str]:
    problems: list[str] = []
    md_path = skill_dir / "SKILL.md"
    if not md_path.is_file():
        return [f"{skill_dir.name}: SKILL.md missing"]

    text = md_path.read_text(encoding="utf-8")
    fm = parse_frontmatter(text)
    if fm is None:
        return [f"{skill_dir.name}: frontmatter block (--- ... ---) missing at top"]

    name = fm.get("name", "").strip()
    if not name:
        problems.append(f"{skill_dir.name}: frontmatter 'name' is required")
    elif not NAME_RE.match(name):
        problems.append(f"{skill_dir.name}: 'name' must be kebab-case, got '{name}'")
    elif name != skill_dir.name:
        problems.append(
            f"{skill_dir.name}: 'name' ({name}) must match directory name ({skill_dir.name})"
        )

    desc = fm.get("description", "").strip()
    if not desc:
        problems.append(f"{skill_dir.name}: frontmatter 'description' is required")
    elif not (MIN_DESC <= len(desc) <= MAX_DESC):
        problems.append(
            f"{skill_dir.name}: 'description' length {len(desc)} outside "
            f"{MIN_DESC}..{MAX_DESC} chars"
        )

    problems.extend(check_local_links(skill_dir, text))
    return [f"{skill_dir.name}: {p}" if not p.startswith(f"{skill_dir.name}:") else p
            for p in problems]


def main() -> int:
    if not SKILLS_DIR.is_dir():
        print(f"error: skills directory not found: {SKILLS_DIR}", file=sys.stderr)
        return 1

    skill_dirs = sorted(d for d in SKILLS_DIR.iterdir() if d.is_dir())
    if not skill_dirs:
        print("error: no skill directories found under skills/", file=sys.stderr)
        return 1

    failed = False
    for skill_dir in skill_dirs:
        problems = lint_skill(skill_dir)
        if problems:
            failed = True
            for p in problems:
                print(f"FAIL {p}")
        else:
            print(f"OK   {skill_dir.name}")

    if failed:
        print("\nSKILL.md lint failed. See skills/README.md 编写规范.", file=sys.stderr)
        return 1
    print(f"\n{len(skill_dirs)} skill(s) passed lint.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
