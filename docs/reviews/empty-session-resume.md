# 空会话恢复修复

## Correctness

用户报告 thread 01a10be6-753c-7c10-a58c-938efc2e468a 恢复返回 Codex -32600 no rollout found。本地完整日志只有启动、连接与退出，无输入／回合。真实 CLI 0.160.0 在临时项目创建后直接关闭，再恢复，稳定复现同一错误；此前真实恢复测试完成过回合，遗漏这个场景。

仅匹配原生错误代码及完整目标 thread ID，并检查完整日志、活动输入、回合状态和队列。完整连续日志确认从未输入才调用 thread/start 重连；保留工作台 ID 与选项，记录旧／新 thread ID。已有输入、缺失日志、序号缺口、其他错误均不自动替换，不重发任何任务。

## Design

原生 RPC 错误保留类型和代码；错误显示格式不变。恢复入口复用原初始化与 thread/start，只有已证实为空的 missing-rollout 分支重建。网页用普通事件说明变化，恢复状态不伪称原 thread 已找回。

## Complexity

无新接口、状态迁移、数据库或登录操作；完整日志分批读取，不能仅根据最近 1000 条判断空会话。生产修改限定在协议错误、恢复逻辑、日志确认与事件说明。

## Tests

修复前：真实无回合会话恢复失败；空会话重连回归失败。修复后：真实空会话恢复成功，新 thread 明确不同；真实只读 shell 回合执行成功，关闭后仍恢复原 thread 01a10c28-3b15-77d3-bd36-925af2c5265f。已有输入不得替换、旧输入超过最近缓存不得替换、其他原生错误不得替换均通过。

Go race 全量、vet 与前端四个回归通过。实际用户工作台会话已重连为空闲状态，原 thread 映射到新 thread 01a10c29-46c5-74d3-847c-45689cea8166，日志记录 workbench/emptySessionRecreated，lastError 清空；未提交任何模型任务到用户项目。

## Maintainability

保留历史与原错误；不通过伪造消息或注入历史来强制生成 rollout。官方 thread/resume 定义为继续已存储会话；空会话尚未落盘的行为依据本机真实复现，不把观察扩大到所有版本。协议参考：[Codex App Server](https://learn.chatgpt.com/docs/app-server)。
