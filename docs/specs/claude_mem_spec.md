# Claude-Mem 接入

用户明确要求本地查看器和 Codex 自动记忆。沿用 iframe 嵌入，自动采集和检索复用 claude-mem 原生 Codex 插件，不增加第二套观察数据库或事件采集适配器。

## 页面

顶部“记忆”入口打开独立工作区，填写本地 worker URL，嵌入查看器、刷新和另开页面。仅允许本机 HTTP 地址，不传递工作台 token，不读取 iframe DOM。地址保存在当前浏览器、按项目区分。服务未运行或禁止嵌入时给出可操作说明，不把 iframe load 事件当作记忆可用证据。查看器可包含其他项目记忆，用户通过其项目筛选器选择。

## 原生自动记忆

安装官方 npm claude-mem 13.34.2 的 codex-cli 集成，复用 SessionStart、UserPromptSubmit、PreToolUse、PostToolUse、Stop hooks 与插件 MCP 检索。观察和摘要保存到 claude-mem 自有数据目录；会话删除不删除记忆。记忆摘要使用本机已登录 Codex 和当前配置模型，不新增 API key。

原生插件安装会注册用户级 Codex marketplace、插件和 features.hooks，先备份原配置。工作台不改写 AGENTS.md 或 CLAUDE.md，不绕过原生 hooks 信任机制。网页通过固定 hooks/list 显示 claude-mem 的具体定义、事件和信任状态；用户查看后显式“信任这些记忆 hooks”，后端再次核验当前 key/hash/plugin 归属再调用 config/batchWrite。信任保存到 Codex 用户级 hooks.state，适用于同一原生定义，需重新连接已有空闲会话；不自动批准未展示或其他插件的 hooks。

这类记忆由原生运行时附加，不混入工作台讨论增量预览。原生 hooks 和 MCP 检索仍占用上下文；不承诺零 token。按 claude-mem 的分层检索取回所需历史，不默认附加全部文档或日志。网络离线、未信任、插件缺失分别显示，禁止把安装成功写作采集已验证。

## 验证

浏览器验证本地 URL 限制、跨源 iframe、工作区切换、服务异常说明、hooks 信任展示和固定 API。原生 CLI 检查插件、hooks/list 与 MCP；隔离临时项目验证采集和后续会话取回。已有真实讨论不提交新任务，服务升级前备份并保持历史。
