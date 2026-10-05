# Claude 项目入口

项目目标、共同规则和本机执行要求见 `AGENTS.md`；目录地图见 `README.md`，当前工作见 `PLAN.md`。

Claude 的 Markdown 短规则位于 `.claude/rules/markdown-style.md`，由 `docs/standards/writing.md` 生成。修改通用源后运行 `python3 scripts/sync_writing_kernel.py`，不手改生成物。

本项目当前未配置 Claude 专用 skills。其他运行时读取通用规范，无需使用 Claude 专用加载机制。
