# 当前计划

## 当前状态

- Go 阶段一已实现项目新建、已有项目打开、文档阅读、原文编辑保存与刷新。
- 网页可用 iframe 显示 docs/diagrams 下的独立 HTML，附有 Archify 架构设计示例。
- 一个进程只绑定一个项目，打开只读加载，明确保存后才写回；工程文件默认折叠。
- HTML 的引用批注、备忘、回复／Rebuttal／结论、解决／重开、删除已恢复，讨论保存到项目；手动 JSON 交接入口已移除。
- Codex 新会话、任务输入、排队、中断、原生审批、事件日志及显式恢复已接入；讨论采用独立参与会话、增量预览和服务端自动回写，结论可显式送回工作会话；多条批注可统一预览、逐条独立交付。
- 独立任务管理、多个 agent 讨论的统一调度、现有 TUI 移交及终端控制权取回尚未实现。

## 当前目标

阶段二 Codex 新会话接入已实现，规格见 `docs/specs/codex_access_spec.md`，使用和证据见 `docs/reviews/codex-access.md`。

对话展示与讨论增量同步已按 `docs/conversation_sync_plan.md` 实现：两条时间线、Markdown、独立讨论会话及可恢复 JSON 同步。真实 Codex 和浏览器验证见 `docs/reviews/conversation-sync.md`。

## 下一步

1. 在现有项目中使用独立讨论、增量发送与显式返回工作会话，收集实际使用反馈。
2. 细化“拉起讨论”、参与 agent、共享任务与人工消息转发的统一入口。
3. 按原生能力继续验证 TUI 移交及终端控制权取回，遵守共享目录与人工 diff 约定。

## 边界

首期没有注册中心、模板升级、迁移、自动修复或默认 worktree。Archify 仅嵌入独立 HTML，生成在工作台之外完成。

## 本轮已实施

会话删除、对话等待指示与页面提醒、项目／会话命令授权配置已实现，规格和验证见 `docs/specs/session_controls_spec.md` 与 `docs/reviews/session-controls.md`。

Claude-Mem 查看器和 Codex 原生自动记忆已接入：五个 hooks、MCP 检索与网页显式信任。临时项目已验证观察、摘要和上下文取回；本机固定版本五个 hooks 已核对并信任，空闲会话已重新连接；后续定义变化仍需重新检查。见 `docs/specs/claude_mem_spec.md` 和 `docs/reviews/claude-mem.md`。
