# Agent 共同规则

本项目是 Agent Workbench。先读 `PLAN.md` 和任务对应的 `docs/specs/`，工程地图见 `README.md`。

1. 设计以 `docs/specs/` 为准；变更设计时先更新规格。
2. 一个工作台只绑定一个项目；遵循奥卡姆剃刀原则，保持流程简单。
3. 只修改任务范围内的内容；无关问题记录到 PLAN。
4. 开发与检查在本机、项目根目录执行，当前没有 SSH 远端。
5. `docs/reviews/discussion/` 是冻结讨论证据，建立后不修改；后续结论写入规格或新报告。此为工程约定，不等同于操作系统权限限制。
6. Markdown 遵守 `docs/standards/writing.md`；共用规则不依赖 Claude 专用目录。
7. progress 只保存精简进度和里程碑；详细验证写入 reviews，运行日志由未来本地服务保存。
8. 完成时给出实际验证结果，不能将初始化完成表述为产品已可运行。

按需阅读 `docs/standards/workflow.md`、`review.md`、`coding.md`。Claude 专用入口见 `CLAUDE.md`。
