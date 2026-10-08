# 批注批量逐条交付验证

## 实现

用户选择逐条独立回应。当前支持跨文档选择、当前筛选列表全选和清空选择，统一选择父工作会话及任务要求后，逐条调用已有独立讨论创建、预览和发送接口。默认只附引用与讨论增量，正文不自动附带。预览不会发送任务。

明确发送后逐条显示交付、排队或错误状态；本批次不重发已交付或发送报错项目。结果不明时需查看对应会话；原有 Manager 约束仍适用，最多同时运行 8 个会话。关闭窗口后服务继续执行，回复分别写回原讨论。批量列表是网页本次操作状态；刷新后通过持久化会话和讨论查看结果，不另建批次数据库。

## 验证

- node scripts/batch_discussion_ui_test.mjs：独立预览、默认无正文、发送归属、部分预览失败、部分发送失败与禁止自动重放通过。
- node scripts/agent_ui_test.mjs 与 node scripts/conversation_ui_test.mjs：既有前端回归通过。
- node scripts/browser_smoke.mjs：两条独立会话预览、各原目标自动回写一次、重复提交不追加、窄屏布局通过；原编辑、批注、审批和队列回归通过，无运行时 JavaScript 异常。
- go test -race ./...、go vet ./... 与构建通过；Markdown 与 git diff 检查通过。
- 实际查看 batch-discussion-preview.png 和 batch-discussion-mobile.png；机器可读结果见 conversation-sync-browser.json。

浏览器第一次运行发现协议模拟器恢复后复用了 turn-1，导致 Manager 正确过滤已完成回合。模拟器改用唯一回合 ID 后完整流程通过，未修改真实协议处理来规避该检查。

未发送真实批量模型任务，未自动提交 Git。写作规范的上下文优化仅作建议，本次未修改 AGENTS.md、规范正文或运行时规则配置。

本地服务仅在全部会话空闲、无队列时更新，完整备份最新 .agent-state 后恢复连接。原工作会话保持同一 native thread；讨论文件与备份逐字节一致，各事件日志前缀保留，未提交新模型任务。
