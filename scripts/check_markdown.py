"""检查 Markdown 的有限静态规则。

按 project.json 检查当前态文档中的特定向后指针模式、非豁免正文空括号、
标准相对 Markdown 链接、根目录文档行数和写作 kernel 同步状态。
不检查全部自然语言日期或反引号路径，不代表项目完整性或产品测试。

用法：python3 scripts/check_markdown.py
"""

from __future__ import annotations

import json
import re
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
CONFIG_PATH = REPO_ROOT / "project.json"

DEFAULT_CONFIG: dict[str, object] = {
    "frozen_snapshot_prefix": "docs/reviews/",
    "current_state": {"docs": ["README.md", "AGENTS.md", "CLAUDE.md", "PLAN.md"], "dirs": ["docs/specs", "docs/standards"]},
    "line_limits": {"PLAN.md": 60},
    "rule_definition_docs": ["docs/standards/writing.md"],
}

# 非 git 仓库兜底遍历时跳过的目录（git 可用时以 git 的文件集为准）
WALK_SKIP_DIRS = {".git", ".venv", "node_modules", "__pycache__", ".ruff_cache", ".agent-state", ".DS_Store"}

BACKWARD_POINTER_PATTERNS = [
    (r"20\d\d-\d\d-\d\d\s*(用户)?(决策|决定|定稿|勘误|确认|新增|实现|重构|更新|建档)", "决策日期标注"),
    (r"用户决策|用户裁决", "决策出处标注"),
    (r"复制入仓|迁移过来|移入本文件|由.{1,12}迁移", "迁移注记"),
    (r"已于\s*20\d\d[-年]", "删除/变更日期声明"),
    (r"存 git 历史", "删除声明"),
    (r"（20\d\d-\d\d(-\d\d)?[^）]{0,6}）", "裸日期括注"),
]

EMPTY_PAREN_PATTERN = re.compile(r"（\s*）|\(\s*\)")
INLINE_CODE_PATTERN = re.compile(r"`[^`]*`")
LINK_PATTERN = re.compile(r"\[[^\]]*\]\((?!https?://|#)([^)#\s]+)")


def load_config() -> dict[str, object]:
    """读取 `project.json`，缺项由默认值补齐。"""
    config = dict(DEFAULT_CONFIG)
    if CONFIG_PATH.exists():
        config.update(json.loads(CONFIG_PATH.read_text(encoding="utf-8")))
    return config


def tracked_markdown_files() -> list[Path]:
    """列出待检查的 md 文件：git 管辖（含未跟踪、未被 ignore）优先，无 git 时遍历全树。"""
    if (REPO_ROOT / ".git").exists():
        proc = subprocess.run(
            ["git", "ls-files", "--cached", "--others", "--exclude-standard", "*.md"],
            cwd=REPO_ROOT,
            capture_output=True,
            text=True,
        )
        if proc.returncode == 0:
            return [REPO_ROOT / line for line in proc.stdout.splitlines() if line and (REPO_ROOT / line).exists()]

    files: list[Path] = []
    for path in sorted(REPO_ROOT.rglob("*.md")):
        if any(part in WALK_SKIP_DIRS for part in path.relative_to(REPO_ROOT).parts):
            continue
        files.append(path)
    return files


def is_current_state(rel: str, config: dict[str, object]) -> bool:
    """判断文件是否属于当前态文档（只陈述现在的文件）。"""
    current = config["current_state"]  # type: ignore[index]
    if rel in current["docs"]:  # type: ignore[index]
        return True
    return any(rel.startswith(d + "/") for d in current["dirs"])  # type: ignore[index]


def in_code_block_map(lines: list[str]) -> list[bool]:
    """逐行标记是否处于 fenced code block 内（代码块内容不检查指针与括号）。"""
    flags: list[bool] = []
    inside = False
    for line in lines:
        if line.lstrip().startswith("```"):
            inside = not inside
            flags.append(True)
        else:
            flags.append(inside)
    return flags


def check_file(path: Path, config: dict[str, object]) -> list[str]:
    """检查单个文件，返回违规描述列表。"""
    violations: list[str] = []
    rel = path.relative_to(REPO_ROOT).as_posix()
    lines = path.read_text(encoding="utf-8").splitlines()
    code_flags = in_code_block_map(lines)

    frozen_snapshot = rel.startswith(str(config["frozen_snapshot_prefix"]))
    rule_definition = rel in config["rule_definition_docs"]  # type: ignore[operator]

    if is_current_state(rel, config) and not rule_definition:
        for i, (line, in_code) in enumerate(zip(lines, code_flags), start=1):
            if in_code:
                continue
            stripped = INLINE_CODE_PATTERN.sub("C", line)
            for pattern, label in BACKWARD_POINTER_PATTERNS:
                if re.search(pattern, stripped):
                    violations.append(f"{rel}:{i}: {label}: {line.strip()[:80]}")

    if not frozen_snapshot and not rule_definition:
        for i, (line, in_code) in enumerate(zip(lines, code_flags), start=1):
            if not in_code and EMPTY_PAREN_PATTERN.search(INLINE_CODE_PATTERN.sub("C", line)):
                violations.append(f"{rel}:{i}: 空括号: {line.strip()[:80]}")

    for i, line in enumerate(lines, start=1):
        for match in LINK_PATTERN.finditer(line):
            if not (path.parent / match.group(1)).resolve().exists():
                violations.append(f"{rel}:{i}: 链接目标不存在: {match.group(1)}")

    limit = config["line_limits"].get(path.name)  # type: ignore[union-attr]
    if limit is not None and path.parent == REPO_ROOT and len(lines) > limit:
        violations.append(f"{rel}: 行数 {len(lines)} 超上限 {limit}")

    return violations


def main() -> int:
    config = load_config()
    violations: list[str] = []
    for path in tracked_markdown_files():
        violations.extend(check_file(path, config))

    sync = subprocess.run(
        [sys.executable, str(REPO_ROOT / "scripts" / "sync_writing_kernel.py"), "--check"],
        capture_output=True,
        text=True,
    )
    if sync.returncode != 0:
        violations.append(sync.stdout.strip() or "kernel 同步检查失败")

    for violation in violations:
        print(violation)
    print(f"共 {len(violations)} 条违规" if violations else "全部通过")
    return 1 if violations else 0


if __name__ == "__main__":
    sys.exit(main())
