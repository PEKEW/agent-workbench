# Agent Workbench

Agent Workbench 是单项目本地网页工作台，把 Markdown 文档、批注讨论、Codex 会话和执行日志放在同一个界面中。网页负责阅读、交互与任务交付，Codex 原生运行时负责文件、代码、Shell 和工具操作，Go 本地服务负责转发、状态持久化与回复回写。

当前交付定位为 **1.0 预览版**，适合单机、单用户、单项目的本地试用。核心工作流程已有自动化检查和真实 Codex 验证记录；源码仓库为 [PEKEW/agent-workbench](https://github.com/PEKEW/agent-workbench)。正式版本标签、发行包和全新环境安装验收尚未完成。

## 已实现的功能

| 模块 | 当前能力 |
| --- | --- |
| 项目 | 新建模板项目、打开已有项目；一个进程绑定一个项目 |
| 文档 | Markdown 阅读、LaTeX 公式渲染、原文编辑保存、新建、重命名与删除、外部刷新、版本冲突保护与冲突差异对比；目录树导航，工程文件默认折叠；文件栏与批注栏可收起 |
| 代码文件 | 目录树中只读查看代码与其他文本文件，通过 `--editor` 指定的外部编辑器打开 |
| 批注 | 引用批注、整篇意见、备忘、回复、Rebuttal、结论、解决与重开；批注编辑框和展开的讨论显示在正文所引段落下方 |
| Agent | Codex 新会话、任务预览与发送、排队、中断、清空上下文、关闭、显式恢复和删除 |
| 讨论交付 | 文档页直接请 Agent 回复并显示处理状态、每份文档一个讨论会话、增量材料、按文档批量交付、最终回复按讨论分段自动写回、结论送回工作会话 |
| 执行反馈 | 对话与执行双时间线、文件修改逐行差异、完整原始日志、执行状态与交互提醒 |
| 权限 | 原生审批与问题响应、项目或会话范围的命令前缀授权及撤销 |
| 交互图 | iframe 展示 Archify 等工具生成的独立 HTML |
| 可选记忆 | Claude-Mem 本地查看器、Codex 原生记忆插件的 hooks 查看与显式信任 |

## 环境要求

- 源码构建：Go 1.25 或更高版本；当前验证工具链为 Go 1.27.1。
- 使用界面：现代浏览器；当前浏览器集成验证使用 Chrome。
- 使用 Agent：本机已安装并登录 Codex CLI，且 `codex` 在 PATH 中；已验证版本为 0.160.0。其他版本的原生协议兼容性尚未验证。
- 开发检查：Python 3 用于文档检查，Node.js 与 Chrome 用于前端和浏览器检查。

已构建的工作台程序内嵌网页与项目模板，基础功能不需要 Go、Python 或 Node.js。Codex 及可选 Claude-Mem 按各自要求安装。仅阅读、编辑和批注文档时可以不启动 Agent。

## 快速开始

取得源码并构建：

```sh
git clone https://github.com/PEKEW/agent-workbench.git
cd agent-workbench
go build -o bin/agent-workbench ./cmd/agent-workbench
```

打开已有项目，路径替换为自己的项目目录：

```sh
./bin/agent-workbench open /path/to/project
```

新建模板项目，目标目录必须不存在，父目录必须已存在：

```sh
./bin/agent-workbench new --name my-project /path/to/my-project
```

也可以不带子命令启动，在网页选择新建或打开：

```sh
./bin/agent-workbench
```

默认自动打开浏览器。打开已有项目只读取文件，不执行项目脚本；保存原文或启动 Agent 后才执行相应操作。服务仅监听 `127.0.0.1`，终端输出的完整 URL 包含本次访问凭证。关闭服务后旧 URL 失效，重新启动时使用新的完整地址。

需要固定端口或手动打开浏览器时：

```sh
./bin/agent-workbench open --port 60834 --no-browser /path/to/project
```

**选项必须放在目录参数前。** 按 Ctrl+C 关闭服务；打开另一个项目时启动另一个进程。

代码文件在网页中只读显示。需要编辑时，用 `--editor` 指定外部编辑器，网页中的“文件 → 外部编辑器打开”会把文件交给它；未指定时 macOS 使用默认文本编辑器：

```sh
./bin/agent-workbench open --editor code /path/to/project
```

## 接入 Codex

在网页的“Agent 会话”中启动 Codex，或通过专用命令同时打开项目与新会话；也可以接入本机已有的原生 thread（新建会话对话框填写 thread ID，或点「载入本机原生会话」从列表选择），让工作台会话与 CLI 会话共享同一原生历史：

```sh
./bin/agent-workbench connect --agent-name Codex /path/to/project
```

可指定模型、沙箱和审批策略：

```sh
./bin/agent-workbench connect --agent-name Reviewer --sandbox read-only --approval on-request /path/to/project
```

`--model` 指定模型，默认沿用原生配置；`--codex-bin` 指定本机可执行文件路径。默认沙箱为 `workspace-write`，审批策略为 `on-request`。支持的沙箱为 `read-only`、`workspace-write`，审批策略为 `on-request`、`never`。

此命令启动新的 `codex app-server` 会话，日常输入由网页接收。当前最多同时连接 8 个原生会话，讨论参与会话也计入此限制。

1. 输入任务，预览实际发送材料，再明确发送。
2. 在对话区查看回复，在执行区查看命令、工具事件与审批。
3. 需要批准或回答原生问题时，通过对应交互控件响应。
4. 忙碌时输入进入队列；失败或中断后队列暂停，可人工继续或清空。
5. “停止回合”中断当前任务，“清空上下文”在空闲时重建 Codex thread 以丢弃原生上下文（保留工作台日志与选项），“关闭会话”结束该会话运行时。

网页关闭或断线后，只要本地服务仍运行，任务与讨论回写继续执行。服务重启后会话标为已断开，点击“恢复会话”尝试恢复原生 thread；结果不明的输入不会自动重发。空会话缺少原生 rollout 时可重建；主动清空过上下文的会话同样按清空点为界判断，有已提交输入的会话保留恢复错误，避免替换已有上下文。

对话和执行过程各默认显示最近 100 条，可展开完整原始日志。顶部、会话列表、讨论参与入口和浏览器标题显示状态；回合结束仍需要核对回复、文件修改和验证结果。

## 文档编辑与讨论

左侧默认展示计划、设计、证据和交互图，`AGENTS.md`、`CLAUDE.md` 等可在“工程文件”中展开。Markdown 使用“编辑原文”修改，再明确保存。外部修改造成版本冲突时保留草稿，核对最新内容后再保存；冻结讨论证据及只读区域不能保存。保存不自动提交 Git。

选中文字后，批注编辑框显示在该段落下方；整篇意见和备忘在批注栏顶部填写。带批注的段落下方显示批注标记，点击标记或批注栏中的引用，在正文内展开完整讨论。讨论支持回复、Rebuttal、结论、解决与重开，刷新或重启后继续保留。

在批注栏顶部选择回复所用的工作会话后，讨论上的“请 Agent 回复”或“保存并请 Agent 回复”在文档页直接发送本条讨论，不切换页面，也不经过预览；讨论显示 Agent 正在回复、排队或写回状态，回复写回后显示在原讨论中。实际发送内容和执行细节在 Agent 页对应会话中查看。

讨论上的“在 Agent 页处理”选择工作会话后进入 Agent 页，工作台创建或打开该文档的讨论会话；同一文档的全部批注共用这个会话和原生 thread，不同文档使用不同会话。批注可跨文档多选，通过“批量交给 Agent”统一指定任务要求；所选讨论按文档合并，每份文档预览一次、发送一个回合。Agent 按「### 讨论 <ID>」分段回应，服务把各段写回对应讨论；回复缺少可识别的分段时不猜测归属，保留在会话中供手动加入。部分失败不会自动重发已交付项。

材料发送规则：

- 首次发送所选讨论的引用和全部发言。
- 后续发送新增、修改或删除的发言及变化的引用。
- 文档正文默认不附带；主动勾选后才发送，未变化且已送达的正文跳过。
- 保存发言不触发模型；“请 Agent 回复”或 Agent 页发送后才启动回合。Agent 页可直接发送，也可先预览。
- 最终回复由服务自动写回；结论“送回工作会话”仍需预览和发送。
- 任务指令末尾附带统一的回复可读性要求（结论先行、结构分块、默认每条讨论 300 字以内、代码块带语言、实施说明改动与验证），发送预览中可见。

网页 DOM、CSS 和整份项目文档不会默认进入模型上下文。Codex 自身加载的项目规范、插件 hooks 和记忆材料由原生运行时处理，仍可能占用上下文，不包含在工作台任务材料预览中。

## 授权与会话清理

顶部或 Agent 工具栏的“命令授权”可保存参数前缀，例如 `go test`、`git status`，范围可选当前项目或单个会话。匹配的原生命令审批可自动响应；复杂 Shell 表达式、文件审批和额外权限仍按原生机制处理。撤销规则影响后续匹配，不撤销已执行操作，也不覆盖 Codex 已有的允许规则或沙箱。

“删除会话”清理所选工作台记录与日志，保留项目文件、批注、其他会话、Codex 原生历史和 Claude-Mem 记忆。执行中、有队列或待回写结果时禁止删除；异常断开遗留的输入需要显式确认放弃。

## Archify 交互图

把生成的独立 HTML 放在项目的 `docs/diagrams/`，刷新后从“交互图”打开。本项目附有架构示例和 JSON 源。

工作台只展示成品。与图配对的源文件（同目录、主名相同或以「<主名>.」为前缀的 `.json` 等源文件）会在图上方列出，可直接点「编辑原文」修改；保存只改源文件，图由外部 Agent 或生成工具重新生成 HTML 后刷新载入，普通 Markdown 保存不会自动更新图。隔离 iframe 支持本地节点、主题等交互，禁止网络请求、弹窗和下载；完整导出功能可单独打开原 HTML 文件使用。

## 可选：Claude-Mem

“记忆”入口嵌入本机 Claude-Mem 查看器，可读取 worker 端口或手动填写本地 URL，地址按项目保存在浏览器中。查看器自身的项目筛选可能包含同一用户的其他项目记忆。

已验证集成为 `claude-mem@13.34.2`。上游安装入口：

```sh
npx claude-mem@13.34.2 install --ide codex-cli --provider codex
npx claude-mem start
```

该安装器会修改用户级 Codex 插件配置，并可能要求第三方登录。本机验证中安装器在注册插件后要求 OAuth，该登录步骤被中止，随后通过本地 Codex provider 配置启动 worker。上述命令尚未通过全新环境端到端安装验收；配置细节、验证过程和限制见 [Claude-Mem 接入记录](docs/reviews/claude-mem.md)。

插件和 worker 就绪后，在“记忆 → 自动记忆设置与 hooks 信任”选择已连接会话，展开并检查五个 hook 定义，明确点击信任，再关闭并恢复空闲会话。信任保存到 Codex 用户配置；定义变化后需要重新检查。观察、摘要及后续检索复用原生插件，数据保存在 Claude-Mem 自有目录。

记忆会产生额外模型调用并占用上下文。工作台讨论增量发送仍按自己的规则执行。安装和配置第三方组件前备份用户配置；若不需要自动生成目录 `CLAUDE.md`，在 Claude-Mem 配置中关闭该行为。

## 数据保存与备份

| 内容 | 保存位置 |
| --- | --- |
| 文档与图 | 项目中原有 Markdown、`docs/diagrams/` 等文件 |
| 批注与讨论 | 项目 `.agent-state/collaboration.json` |
| 工作台会话、事件日志与命令授权 | 项目 `.agent-state/sessions/` |
| 原生会话历史 | Codex 自身的数据目录 |
| 自动记忆 | Claude-Mem 自身的数据目录 |

`.agent-state/` 默认不进入本项目 Git。**只提交 Markdown 不能备份网页讨论和会话记录。** 完整项目备份需包含此目录，建议在停止服务后复制；原生历史、记忆和用户配置需要分别备份。日志包含任务输入、工具事件和回复，应按本地工作数据管理。

## 1.0 范围与交付状态

当前范围是单机文档协作与 Codex 会话工作台。以下能力尚未实现：其他 Agent 运行时适配、已有 TUI 接管及控制权取回、统一共享任务管理、多 Agent 自动讨论调度、可视化 Git 合并、代码编辑器或 VS Code／nvim 接入、Archify 自动生成、模板升级与迁移。

多个会话共享项目目录，版本保存和合并仍通过 Git 操作并人工审阅。当前不默认建立 worktree；没有远程多人账户或多项目进程管理。

正式稳定版发布前还需完成：

- 将当前实现整理为可复现的版本提交与标签，并准备对应发行包。
- 在全新环境验收构建、首次启动、Codex 接入、服务重启恢复与数据备份；可选记忆单独验收。
- 明确支持的操作系统与版本范围；当前证据主要来自 macOS，Linux 和 Windows 尚未完成同等验收。
- 确定项目自身许可证，核对第三方组件许可与分发清单。

当前源码通过 GitHub 仓库交付，尚未配置自动化 CI 或项目自身许可证，也未发布预编译程序。交付范围以仓库中的已提交源码为准，本地讨论、会话日志和用户配置不随源码上传。

## 开发与验证

```sh
go test -race ./...
go vet ./...
python3 scripts/check_markdown.py
node scripts/agent_ui_test.mjs
node scripts/batch_discussion_ui_test.mjs
node scripts/conversation_ui_test.mjs
```

构建后运行 `node scripts/browser_smoke.mjs`，使用临时项目和独立 Chrome 配置，结果写入 `docs/reviews/`。Chrome 路径可通过 `CHROME_BIN` 指定。该检查使用测试协议进程；真实 Codex 和记忆链路另有验证，不能相互替代。真实模型检查会产生模型调用，运行方法见对应验证记录。

源码布局：`cmd/agent-workbench` 为命令入口，`internal/project` 管理项目与内嵌模板，`internal/agent` 适配原生运行时，`internal/server` 提供 HTTP 和内嵌网页，`scripts` 保存检查工具。

- [当前计划](PLAN.md)与[任务计划](docs/task_plan.md)
- [产品规格](docs/specs/workbench_spec.md)与[Codex 接入规格](docs/specs/codex_access_spec.md)
- [阶段一验证](docs/reviews/stage-one.md)、[Codex 验证](docs/reviews/codex-access.md)、[讨论同步验证](docs/reviews/conversation-sync.md)
- [批量交付验证](docs/reviews/batch-discussion.md)、[会话控制验证](docs/reviews/session-controls.md)、[记忆验证](docs/reviews/claude-mem.md)
- [Agent 共同规则](AGENTS.md)、[完整讨论快照](docs/reviews/discussion/index.html)、[第三方组件](THIRD_PARTY.md)
