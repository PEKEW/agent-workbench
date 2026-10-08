# Claude-Mem 接入验证

2026-10-07 接入查看器及 Codex 自动记忆。复用上游原生 Codex 插件，无额外观察数据库或 HTTP 采集适配器。

## 版本与安装

核对上游 main `71ddd11735d6dc38a6356fe376921fc216f2aa38` 和 npm `claude-mem@13.34.2`。安装本机原生 Codex marketplace、插件与 Bun，Codex 0.160.0 可发现五个 hooks：SessionStart、UserPromptSubmit、PreToolUse、PostToolUse、Stop。插件提供 MCP 搜索。

原 Codex 配置已备份到忽略目录 `.superpowers/sdd/claude_mem_plan/codex-config-before.toml`。安装器在注册插件后要求 cmem.ai OAuth，已中止该步骤，未完成第三方登录；本地 settings 改用已登录的 Codex provider、gpt-6.1-sol、low、一个观察器。关闭目录 CLAUDE.md 生成和额外 transcript 摄取。worker 在 `127.0.0.1:37701`，实际健康检查返回 version 13.34.2、initialized、mcpReady、Codex CLI subscription。

## 页面与信任

顶部“记忆”提供本机 HTTP iframe，端口自动发现，按项目记住地址，支持刷新和另开。独立跨源 iframe 不获得工作台 token；架构图 iframe 的无网络隔离保持原规则。本地 health 不跟随重定向，拒绝远程主机、凭证、路径及查询参数。

网页只展示 claude-mem 插件原生 hooks/list；展开可检查命令、事件、源文件和摘要。信任时后端再核验 plugin/key/hash/启用状态，并使用 config/batchWrite expectedVersion 写用户级 hooks.state；不透传任意方法。本次用户明确授权查看器与自动记忆均接入，实施时核对固定版本五个定义，并通过同一固定 API 信任这些当前摘要。空闲会话已重新连接；后续新或变更定义仍需在网页检查。

## 实际证据

- Chrome 协议夹具验证查看器入口、hooks 展示和信任、拒绝远程地址、页面切换及手机无溢出；旧编辑和讨论回归通过。截图 claude-mem-embedded-viewer.png 使用查看器夹具；claude-mem-live.png 已另验证真实本机查看器及五个已信任原生 hooks。
- `AWB_REAL_CODEX_MEMORY_HOOKS=1 go test ./internal/agent -run TestInstalledCodexMemoryHooksTrust -count=1` 通过：信任只写临时 CODEX_HOME，原生五个定义从 untrusted 变为 trusted。
- 独立原生集成探测使用临时 CODEX_HOME、记忆数据目录和项目，信任测试定义后启动真实 Codex，执行一次 `pwd`，不修改项目文件。五次 hook/completed 均 completed，worker 使用本机 Codex 生成 1 条 discovery 观察和 1 条会话摘要；`/api/context/inject?cwd=...&platformSource=codex` 取回包含 AWB_MEMORY_PIPELINE_OK 的索引和摘要。临时 worker 已停止。
- Go race、vet、build、前端与 Chrome 回归通过。最终只读评审未发现 Important/Critical。

## 范围与限制

观察模型调用和检索会消耗 token；原生 hook 注入不会出现在工作台“精确任务预览”，原生钩子执行和完整原始事件可在执行日志查看。默认全文观察数量为 0，按索引与 MCP 取回细节；仍会附加摘要和 claude-mem 的 work_state 工具指引。此指引是上游行为，尚未与工作台任务模块整合。

查看器含用户级跨项目记忆，使用其项目筛选器；工作台会话清理保留这些记忆。安装或信任成功不能单独证明某个实际任务已入库，应在查看器检查观察与摘要。已有真实讨论未用于集成探测；测试仅信任临时 CODEX_HOME；本机固定版本激活在测试之后按用户自动记忆要求单独完成。

来源：[上游源码与中文 README](https://github.com/thedotmack/claude-mem/blob/main/docs/i18n/README.zh.md)、[类似工作台的原生接入](https://github.com/thedotmack/claude-mem/blob/main/docs/public/t3code-integration.mdx)、[OpenAI hooks 信任机制](https://learn.chatgpt.com/docs/hooks)。

## 本地服务更新

升级前确认四个用户会话无运行或排队任务，备份实际 .agent-state 后更新端口 60834 的服务。保持两个已关闭会话关闭，恢复两个原空闲会话；有任务的工作会话 native thread 保持不变，尚无 rollout 的空讨论会话按既有规则重新连接。collaboration.json 字节不变，四份原事件日志完整保留为前缀。未向实际用户会话提交新模型任务。
