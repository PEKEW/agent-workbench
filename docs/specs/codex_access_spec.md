# 阶段二 Codex 原生会话接入

## 目标与范围

在现有 HTML 阅读、编辑与讨论上接入 Codex 原生执行。支持专用命令启动新会话、网页创建多个新会话、消息与任务输入、选定文档及讨论转发、原生事件查看、审批、停止回合与关闭会话。工作台不执行模型的 shell 或文件工具；这些操作由 Codex app-server 执行。

此阶段不接管运行中 TUI，不提供终端输入取回、自动广播、独立任务系统、自动合并、默认 worktree 或图自动生成。专用命令启动时没有并行 TUI 输入入口，因此该新会话的输入只来自工作台；不能声称锁住其他终端的现有会话。

## 原生接口

验证基线为本机 codex-cli 0.160.0，使用 CLI 生成的 JSON Schema。每个工作台会话拥有独立的 `codex app-server --listen stdio://` 子进程，标准输入输出为 JSONL；先 initialize 再 initialized。thread/start 创建原生会话，turn/start 发送输入，turn/interrupt 中断。模型参数留空时不指定模型，沿用 Codex 配置；工作目录固定为项目根目录。

默认 sandbox 为 workspace-write，approvalPolicy 为 on-request，approvalsReviewer 为 user。网页可明确选择 read-only／workspace-write 及 on-request／never；此阶段不提供跳过沙箱入口。展示 thread/start 返回的实际模型与权限，而非只显示请求值。登录使用现有 Codex 认证；不读取或转存认证文件。

协议说明：[官方 Codex App Server](https://learn.chatgpt.com/docs/app-server)。

## 状态与资源

状态包括 starting、idle、running、waiting、disconnected 和 closed。按原生 turn/completed 的 completed／failed／interrupted 展示回合结果；失败不能当作完成。回合开始前占用输入入口，多个网页不能同时启动两个回合。忙碌时输入进入有界队列，正常完成后逐条发送；失败或中断时队列暂停，人工决定继续或清空。队列与发送状态明确记录，服务重启不自动重放。

网页断线不结束运行，轮询按事件序号补齐。关闭会话或服务终止自有子进程并释放 pipe；不会结束其他 Codex 进程。恢复服务后读取历史和原生 thread ID，标记 disconnected，用户明确恢复后调用 thread/resume；本阶段只恢复本工作台记录的会话，不自动搜索全部 Codex 历史。

每项目最多 8 个运行时会话，每会话最多 20 条排队输入，单次输入及选定上下文总计不超过 2 MiB。原始事件持久化在 `.agent-state/sessions/`，网页默认显示最近 100 条，完整日志可按序读取；此阶段不删除或轮换原始日志。状态目录不跟随符号链接。只读打开项目不创建状态；明确创建或恢复会话才启动进程。

## 审批与待响应请求

原生 commandExecution、fileChange 的审批展示命令、目录、原因及原始参数，提供本次允许、拒绝与取消；不增加永久允许。permissions 请求展示完整权限，允许只授予请求权限且范围为本回合，拒绝授予空权限。tool/requestUserInput 按原生问题 ID 回答；MCP elicitation 显示原生请求并允许取消。无法支持的请求保留为待响应并明确显示，用户可中断或关闭，不自动同意或伪造工具结果。

请求 ID 与会话绑定；请求已解决、中断或进程退出后不能重复批准。普通消息不能回答审批请求。未知通知保留为原始事件。stderr 属于运行时诊断，独立标注，不伪装成操作事件。

## 网页与协作

文档与讨论三栏保留，新增 Agent 会话视图，支持列表、状态、当前权限、队列、待响应请求和按事件查看消息及执行信息。模型文本与日志通过 textContent 展示，不执行 HTML。网页刷新后重取会话及事件。

发送前展示准确输入：用户消息、选定文档及其内容版本、选定讨论及原始署名和消息意图。只发送这些材料；不包含网页 DOM、CSS 或全部项目历史。讨论上的“转发给 Agent”只预填预览，明确发送后才调用原生回合。原生回复可由用户加入指定讨论，记录来源会话和 item ID；不会自动广播或覆盖正文。

## 命令和 HTTP

`agent-workbench connect [--agent-name 名称] [--model 模型] [--sandbox 模式] [--approval 策略] [--no-browser] <项目目录>` 打开项目并创建一个 Codex 新会话。已有 open/new 命令仍有效，网页可主动创建会话。二进制通过 PATH 查找 codex，命令路径仅由本地启动配置指定，不接受网页任意可执行路径或 shell 参数。

Agent 接口沿用工作台令牌与 Origin 检查。GET /api/agents 读取会话；POST /api/agents 创建；GET /api/agents/{id}/events 按 after 序号读取；POST /api/agents/{id}/input 发送／排队；POST /api/agents/{id}/interrupt 中断；POST /api/agents/{id}/close 关闭；POST /api/agents/{id}/resume 显式恢复；POST /api/agents/{id}/queue 控制暂停队列；POST /api/agents/{id}/requests/{requestId} 回答已记录请求。接口不提供任意原生方法透传。

## 验收与中止条件

使用真实子进程的协议测试验证握手、回合流、忙碌排队、错误及中断、请求路由、退出清理、日志补读、重启标记与显式恢复。真实 CLI 验证握手与会话创建，并用隔离临时项目运行最小只读回合；真实模型回合若受登录、配额或网络阻塞则报告实际结果，不冒充成功。浏览器验证新视图及原 HTML 交互不回退。

若实际协议缺少必要生命周期或无法可靠保留原生权限，则暂停该能力并报告证据，不以终端文字抓取替代结构化协议，也不接管用户现有进程。
