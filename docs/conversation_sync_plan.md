# 对话与讨论同步实现计划

> 执行方式：使用 superpowers:executing-plans 在当前会话执行。用户已明确要求开始改造；遵循本项目在根目录开发、无默认 worktree 的约定，不自动提交 Git。

**目标：** 隔离工作会话与讨论上下文，提供 Markdown、两条时间线和可恢复的 JSON 增量同步。

**架构：** 复用独立 Codex 原生会话实现讨论参与者，保存父子关系。Manager 记录输入上下文和待回写结果；本地服务后台幂等写入讨论，网页只负责展示与明确发送。

**技术：** Go 标准库、JSON、marked、DOMPurify、现有浏览器协议测试。

**规格：** docs/specs/conversation_sync_spec.md。

## 约束与重点

- 一个工作台绑定一个项目；运行时会话最多 8 个，队列最多 20 条，输入及材料总计最多 2 MiB。
- 独立 thread/start 实现工作台父子关系，不依赖未经验证的原生 subagent 调度。
- 仅已确认送达材料参与增量计算；排队快照允许保守重复，禁止依赖未送达前序。
- 完整日志用于恢复待回写，结构化来源去重；不覆盖人工消息、不重建已删除讨论。
- 旧 JSON 可读；符号链接、越界、并发预览失效与写入失败保留明确错误。

## 任务一：讨论运行状态

文件：internal/agent/discussion.go、manager.go、storage.go、discussion_test.go。

- [x] 核对已有独立父子会话、复用、送达时点、排队清空与最终回复测试；新增完整日志恢复和乱序回归，确认修复前失败。
- [x] 实现 CreateDiscussion、SendDiscussion 与上下文快照结构；Session 保存关系、已送达快照及待回写结果。
- [x] 完成事件与响应乱序归属检查，按回合收集最终回复，完成后登记待回写记录。
- [x] 运行 internal/agent 测试，确认普通会话、审批和恢复回归通过。

## 任务二：增量输入与后台回写

文件：internal/server/discussions.go、agents.go、server.go、discussions_test.go；internal/project/collaboration.go。

- [x] 核对并补充 HTTP 测试，覆盖首次完整、第二次增量、正文变化、过期预览、父子隔离、关闭网页回写和重复结果。
- [x] 增加讨论专用会话入口、带基线摘要的预览与发送校验；增加可选结构化消息来源。
- [x] 本地后台任务读取 Manager 待回写、按 generation 合并和来源去重，重启恢复且不触发模型任务。
- [x] 运行相关 HTTP 与持久化测试。

## 任务三：阅读与交互

文件：internal/server/web/markdown.js、agent-timeline.js、agents.js、collaboration.js、index.html、style.css；scripts/agent_ui_test.mjs、browser_smoke.mjs。

- [x] 先加入时间线分类、流式去重、会话切换与 Markdown 安全浏览器断言，确认旧实现失败。
- [x] 共用安全 Markdown 渲染；对话和执行各自保存最近 100 条记录并提供回合定位。
- [x] 讨论入口选择父会话后创建或打开子会话，持续加载关联讨论，提供回复讨论及送回工作会话入口。
- [x] 运行前端和浏览器验证，检查桌面与窄屏截图。

## 任务四：交付与评审

- [x] 运行 go test -race ./...、go vet ./...、go build ./cmd/agent-workbench、Markdown 检查及 git diff --check。
- [x] 对照规格逐项检查，进行代码评审；修正重要问题并验证。
- [x] 更新 README、PLAN、progress 和规格现状，详细结果写入 docs/reviews/conversation-sync.md。

## 执行记录

- 规格已修订为独立讨论专用会话。初次 go 调用因 PATH 缺失失败，使用 README 指定的本机 Go 工具链继续。

- 保留接续时已存在的后端修改，前端时间线测试最初因文件缺失失败；恢复、去重、预览稳定性及草稿回归均验证修复前失败。
- 2026-10-07：所有实现任务完成，Go race、vet、build、前端与 Chrome 检查通过，真实 Codex 独立讨论回合通过，评审无剩余 Critical 或 Important。证据见 docs/reviews/conversation-sync.md。
- 原有状态完整备份保留在本地忽略目录，未自动提交 Git。
