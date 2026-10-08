# 对话与讨论同步验证

## 结果

2026-10-07 按 docs/conversation_sync_plan.md 完成改造，保留接续时已有后端修改。未自动提交 Git。

- 文档、对话和讨论使用安全 Markdown；原文编辑、复制、交接 JSON 保留原文。
- 对话与执行各保留最近 100 条记录，独立滚动并可定位同回合；原始日志完整保留。
- 每个工作会话与讨论组合使用独立原生会话；首次完整输入，随后根据已确认送达记录生成增量。
- 回合结束后服务后台幂等写回最终回复；网页关闭不影响同步，重启仅恢复待回写，不自动提交模型任务。
- 人工发言保存与模型执行分开；送回工作会话需要选择、预览和明确发送。

## 验证

| 检查 | 结果与范围 |
|---|---|
| go test -race ./... | 全部包通过，包含完整日志恢复、队列送达不明、重复与乱序事件、回写重试、删除及人工修改 |
| go vet ./... | 通过 |
| go build -o bin/agent-workbench ./cmd/agent-workbench | 通过 |
| python3 scripts/check_markdown.py | 全部通过 |
| python3 scripts/sync_writing_kernel.py --check | kernel 同步一致 |
| git diff --check | 通过 |
| node scripts/agent_ui_test.mjs | 5 项通过，包含会话切换、完整日志切换与保存讨论草稿并发 |
| node scripts/conversation_ui_test.mjs | 双时间线、输入和流式去重、各自条数上限、外来 thread 过滤通过 |
| node scripts/browser_smoke.mjs | Chrome 集成通过，runtimeErrors 为空；包含 3100 条执行事件、独立讨论、自动回写、增量、原 HTML 交互、原文编辑、审批、队列及窄屏 |
| 真实 Codex 讨论检查 | codex-cli 0.160.0，临时项目与只读沙箱；固定回复 AWB_DISCUSSION_OK 写入一次，第二轮预览为增量 |

真实 Codex 检查命令：

```sh
AWB_REAL_CODEX_DISCUSSION=1 go test -v ./internal/server -run TestInstalledCodexDiscussionWriteback -count=1
```

最终运行耗时 7.16 秒。原生父 thread 为 01a1151c-3082-7950-a3d3-592f685a71d8，子 thread 为 01a1151c-312f-7eb0-9084-881dceb3fbbd，模型 gpt-6.1-sol。测试未提交代码、shell 或联网任务。

浏览器使用明确的协议测试进程，真实 CLI 单独验证。机器可读结果见 [conversation-sync-browser.json](conversation-sync-browser.json)。已实际查看[桌面截图](conversation-sync-discussion.png)和[窄屏截图](conversation-sync-mobile.png)。

## 原有数据

升级前确认现有工作会话原始日志包含 3362 条事件、9 条用户输入及 19 条完整 Agent 消息；collaboration.json 为 generation 10，1 条讨论、7 条发言。完整状态备份保留在本地忽略目录 .superpowers/sdd/conversation_sync_plan/state-backup-before-work。测试使用临时项目，未向原工作会话发送任务。升级后恢复到原 native thread 01a10c2e-d0f3-7790-a277-bd3d2150e62e，状态为空闲；原日志作为前缀完整保留，讨论文件与备份逐字节一致。新服务接口与前端投影重建结果为 9 条用户消息、19 条 Agent 消息、最近 100 条执行记录，原对话均可读取。

## 评审五问

1. Correctness：送达仅根据匹配原生确认推进；提交标记与完整日志扫描支持恢复；已完成 turn 的迟到 started 不得绑定后续排队输入。
2. Design：继续使用 Go 服务与 JSON；父子关系由工作台管理，每个参与者使用独立 thread/start。
3. Complexity：沿用现有事件、队列及 Markdown 依赖，无数据库或调度框架；跨文件更新采用持久化待回写与幂等追加。
4. Tests：除成功路径，验证 1000 条缓存外的恢复、重复完成、队列晋升窗口、送达不明、删除消息和人工修改后的重试。
5. Maintainability：新增共用渲染和时间线投影模块；README、规格和计划同步现状。

独立评审发现的送达不明队列、删除顺序不稳定、人工修改基线、切换草稿及迟到事件问题均修复；各相关回归先确认失败，再验证通过。最后评审无剩余 Critical 或 Important。

## 边界

最多 8 个运行时会话，队列最多 20 条；现有 TUI 移交、共享任务调度与其他运行时适配继续列在 PLAN.md。父子关系不依赖原生 subagent 调度。输入送达结果不明时需检查历史、清空旧队列并完整重新预览，禁止自动重放。跨文件 JSON 更新不提供数据库事务保证。
