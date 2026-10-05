# Agent Workbench

以 semi-harness 项目结构为基础的本地 Agent 网页工作台。一个工作台绑定一个项目，通过网页管理文档、任务、多个 agent 和讨论，原生运行时负责文件、代码、shell 与 Git 操作。

当前状态：空项目骨架与需求基线已建立，产品代码尚未实现。首先完成新建／恢复项目，再接入 Codex；后续支持更多运行时。

## 从这里开始

- 当前工作：[PLAN.md](PLAN.md)。
- 产品需求：[工作台规格](docs/specs/workbench_spec.md)。
- 阶段安排：[任务计划](docs/task_plan.md)。
- Agent 共同规则：[AGENTS.md](AGENTS.md)。
- 完整讨论与批注：[讨论快照](docs/reviews/discussion/index.html)。
- 初始化来源与验证：[初始化记录](docs/reviews/initialization.md)。

## 目录

| 路径 | 用途 |
| --- | --- |
| docs/specs/ | 设计规格 |
| docs/standards/ | 工作流、评审、代码与写作规范 |
| docs/reviews/ | 证据与冻结讨论快照 |
| docs/findings.md | 调研发现 |
| PLAN.md、progress.md | 当前状态与精简里程碑 |
| scripts/ | 模板自带的文档检查与规则同步脚本 |
| .claude/rules/ | 从通用写作规范生成的 Claude 专用加载文件 |

## 本机检查

在项目根目录执行：

```bash
python3 scripts/check_markdown.py
```

更新写作 kernel 后执行 `python3 scripts/sync_writing_kernel.py`。应用启动命令和产品测试命令将在首个可运行版本中提供。
