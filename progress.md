# 项目进度

## 2026-10-05 项目初始化

- 通过 semi-harness 原创建器建立 Agent Workbench。
- 写入需求基线、任务计划、本机规则，并保存讨论证据。
- 当前没有产品实现；下一步细化项目新建／恢复的实现规格。
- 初始化检查结果见 docs/reviews/initialization.md。

## 阶段一进行中

Go 实现项目入口与阅读器。若必须改变现有项目文件或执行其中脚本才能打开，停止该操作并调整实现；不扩展到 agent 接入。

## 阶段一交付

Go 项目入口、文档阅读与 Archify iframe 已实现。关键验证与截图见 docs/reviews/stage-one.md；下一步是 Codex 新会话接入。

## 阅读器完善

工程文件默认折叠；恢复 Markdown 原文编辑与保存，增加冲突保护和未保存提示。Archify 当前外部生成，刷新载入，未来以明确更新请求触发。

- 恢复 HTML 引用批注、备忘、回复与 Rebuttal、解决／重开、删除和 JSON 交接；协作状态保存到项目并支持服务重启恢复。

- 开始阶段二 Codex 接入：若原生协议无法可靠保留生命周期或权限则暂停该能力并报告，不接管现有用户进程。

- 阶段二接入 Codex 新会话、输入队列、中断、原生响应、完整事件日志与显式恢复；HTML 讨论与会话可双向手动转发。
- 真实 CLI 0.160.0 握手及只读 shell 回合通过；浏览器协议路由、原 HTML 回归与前端并发测试通过。详细证据见 `docs/reviews/codex-access.md`。

- 修复空会话恢复：真实 CLI 已复现并修复缺失 rollout；空会话重建、有输入时禁止替换与完整日志回归通过，用户会话已重连。详细证据见 docs/reviews/empty-session-resume.md。若无法可靠判定是否提交过输入则保留原错误，不重建。

- 补充发送按钮禁用原因：提示输入、预览或恢复会话，保留已有的预览后发送规则。

## 对话与讨论同步设计

- 已按书面计划完成 Markdown、双时间线、独立讨论会话、增量预览、后台幂等回写与显式返回工作会话。
- Go race、vet、build、前端和 Chrome 回归通过；真实 Codex 父子独立 thread、一次回写与第二轮增量通过。评审发现的事件乱序、送达不明、草稿切换和人工修改问题已修复。
- 保留原有讨论与未提交修改，未自动提交 Git。详细证据和下一步见 docs/reviews/conversation-sync.md 与 PLAN.md。
- 中止条件：无法确认原生输入送达与回复归属，或不能保留冲突及待回写状态时暂停相关同步，不自动重放。

- 按使用反馈移除 JSON 导出与外部回复导入；扩大对话／执行主区域，预览默认折叠。讨论默认只附引用与发言，正文改为明确选择并保留增量去重。验证见 docs/reviews/discussion-context-layout.md。

- 批注多选与逐条独立交付完成：统一选择 Agent、逐条预览、明确批量发送，各回复写回原讨论；部分失败和重复提交验证通过。证据见 docs/reviews/batch-discussion.md。

- 补充跨页面执行状态、批注参与会话状态及日志读取失败时的状态刷新；修复固定 Rebuttal 指令覆盖“实现此功能”的问题。验证见 docs/reviews/session-status.md。

- 会话清理、持续等待提示和交互提醒、项目／会话授权列表完成；撤销与审批写入竞态、复杂 shell 和异常输入保护经 race 与浏览器验证。证据见 docs/reviews/session-controls.md。
- Claude-Mem 查看器、原生 hooks 查询和显式信任接入；固定版本已安装，本地 Codex provider 服务运行。隔离项目验证 1 条观察、1 条摘要和上下文取回；固定版本五个 hooks 已按自动记忆要求核对并信任。证据见 docs/reviews/claude-mem.md。

## GitHub 源码交付

- 整理 1.0 预览版 README 和当前实现，发布目标为 PEKEW/agent-workbench；源码交付不包含本地会话、讨论状态、配置备份或构建产物。正式版本标签、发行包及全新环境验收仍待完成。
