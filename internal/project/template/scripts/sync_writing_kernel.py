"""把写作规范的 kernel 区同步到会话加载的短规则文件。

唯一权威源与目标文件由仓库根目录 `project.json` 的 `kernel` 节指定，默认
`docs/standards/writing.md` 的标记区 -> `.claude/rules/markdown-style.md`。
标记之间的内容被逐字抄写，保证「规则只维护一份」：改规范只改权威源，再跑本脚本。

用法：
    python scripts/sync_writing_kernel.py          # 同步（覆盖目标文件）
    python scripts/sync_writing_kernel.py --check  # 只校验一致性，不写盘
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
CONFIG_PATH = REPO_ROOT / "project.json"

DEFAULT_KERNEL: dict[str, str] = {
    "source": "docs/standards/writing.md",
    "target": ".claude/rules/markdown-style.md",
    "start_marker": "<!-- CLAUDE-WRITING-KERNEL:START -->",
    "end_marker": "<!-- CLAUDE-WRITING-KERNEL:END -->",
    "frontmatter": '---\npaths:\n  - "*.md"\n  - "**/*.md"\n---\n',
}


def load_kernel_config() -> dict[str, str]:
    """读取 `project.json` 的 `kernel` 节，缺项由默认值补齐。

    Returns:
        含 `source`/`target`/`start_marker`/`end_marker`/`frontmatter` 的配置字典。
    """
    kernel = dict(DEFAULT_KERNEL)
    if CONFIG_PATH.exists():
        config = json.loads(CONFIG_PATH.read_text(encoding="utf-8"))
        kernel.update(config.get("kernel", {}))
    return kernel


def extract_kernel(text: str, start_marker: str, end_marker: str, source: Path) -> str:
    """取出权威源中标记之间的内容（不含标记行）。

    Args:
        text: 权威源全文。
        start_marker: kernel 区起始标记。
        end_marker: kernel 区结束标记。
        source: 权威源路径，仅用于报错信息。

    Returns:
        kernel 区正文，首尾空白已去除。

    Raises:
        ValueError: 标记缺失或顺序错误时抛出。
    """
    match = re.search(re.escape(start_marker) + r"(.*?)" + re.escape(end_marker), text, re.DOTALL)
    if match is None:
        raise ValueError(f"{source} 中未找到成对的 kernel 标记")
    return match.group(1).strip()


def render_target(kernel: str, frontmatter: str) -> str:
    """拼出目标规则文件的完整内容。"""
    return f"{frontmatter}\n<!-- GENERATED. Do not edit directly. -->\n\n{kernel}\n"


def main() -> int:
    parser = argparse.ArgumentParser(description="同步写作规范 kernel 区到会话加载的短规则文件")
    parser.add_argument("--check", action="store_true", help="只校验目标文件与 kernel 区一致，不写盘")
    args = parser.parse_args()

    config = load_kernel_config()
    source = REPO_ROOT / config["source"]
    target = REPO_ROOT / config["target"]

    expected = render_target(
        extract_kernel(source.read_text(encoding="utf-8"), config["start_marker"], config["end_marker"], source),
        config["frontmatter"],
    )

    if args.check:
        actual = target.read_text(encoding="utf-8") if target.exists() else ""
        if actual != expected:
            print(f"不一致：{target} 落后于 {source} 的 kernel 区，运行 python scripts/sync_writing_kernel.py")
            return 1
        print("kernel 同步一致")
        return 0

    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(expected, encoding="utf-8")
    print(f"已同步 {source.relative_to(REPO_ROOT)} -> {target.relative_to(REPO_ROOT)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
