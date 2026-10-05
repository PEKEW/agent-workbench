# Agent Workbench

以 semi-harness 项目结构为基础的本地 Agent 网页工作台。一个工作台绑定一个项目，在浏览器中打开计划、设计、证据和交互图。

当前版本提供项目新建、已有项目打开、文档阅读与编辑保存、HTML 批注讨论和 iframe 图展示。已接入 Codex 新会话、任务输入、原生审批、执行事件、排队和手动讨论转发；独立任务管理与自动广播尚未实现。

## 使用

本机已构建的程序：

```bash
./bin/agent-workbench
./bin/agent-workbench open /Users/peke/agent-workbench
./bin/agent-workbench new --name my-project /已有父目录/my-project
```

默认自动打开浏览器。使用 `--no-browser` 只打印地址，`--port 端口` 指定端口；选项须放在目录参数之前。Ctrl+C 关闭服务。打开另一个项目时启动另一个进程。

新建目标目录必须不存在；打开已有项目只读加载文件，不执行项目脚本。缺少规范或计划时页面显示提示，文档刷新可读取外部编辑的最新内容。工程文件默认折叠，可在左侧展开查看。Markdown 可编辑原文并保存到对应文件；外部改动会提示版本冲突并保留草稿，冻结记录及只读区域不能保存。保存不会自动提交 Git。

访问地址含本次服务凭证，请使用终端给出的完整地址。

## 交互图

把 Archify 生成的独立 HTML 放在项目的 `docs/diagrams/`，刷新后在左侧“交互图”中打开。HTML 在隔离 iframe 中运行，可使用节点查看、主题和路径等本地交互。嵌入模式不支持网络请求、弹窗和下载；需要完整导出功能时可在浏览器单独打开原 HTML 文件。

本项目附带阶段一架构设计图及 JSON 源。工作台只读取成品，不需要 Archify 或 Node.js 运行环境，也不做双向编辑或自动生成。当前由外部 agent 修改 JSON 并生成 HTML，网页刷新载入。后续接入 agent 后建议用明确“更新图”请求触发，并在架构或阶段计划变更后提示；普通 Markdown 保存不自动生成图。

## 构建与检查

Go 1.25 或更高版本：

```bash
go build -o bin/agent-workbench ./cmd/agent-workbench
go test -race ./...
go vet ./...
python3 scripts/check_markdown.py
```

本机工具链安装在 `/Users/peke/.local/share/go-toolchains/go1.27.1/go/bin`，需要源码开发时可将此目录加入 PATH。运行已构建程序不依赖 Go 或 Python。

可选浏览器检查：先构建，再运行 `node scripts/browser_smoke.mjs`。检查使用临时项目、独立 Chrome 配置和临时服务，结果写入 docs/reviews；其他 Chrome 安装路径可用 CHROME_BIN 指定。开发验证使用 Node.js 24 与 Chrome，最终程序不依赖它们。

## 项目入口

- [当前计划](PLAN.md)与[任务计划](docs/task_plan.md)。
- [产品需求](docs/specs/workbench_spec.md)与[阶段一规格](docs/specs/project_access_spec.md)。
- [Agent 共同规则](AGENTS.md)；Claude 专用入口见 CLAUDE.md。
- [阶段一验证记录](docs/reviews/stage-one.md)。
- [完整讨论快照](docs/reviews/discussion/index.html)。
- [第三方组件](THIRD_PARTY.md)。

## 目录

cmd/agent-workbench 是命令入口；internal/project 管理项目与内嵌模板；internal/server 提供 HTTP 和内嵌网页；docs/specs 保存设计；docs/reviews 保存证据；docs/diagrams 保存交互图；scripts 保存开发检查工具。

## 文档协作

工作台保留 HTML 的三栏阅读与讨论：选中文字批注、整篇意见、备忘日期、回复／Rebuttal／结论、解决与重开及删除。讨论保存到 `.agent-state/collaboration.json`，刷新或重启服务后继续；该目录默认不进入 Git。多标签页保存发生冲突时保留输入、载入最新讨论，核对后再提交。

“导出交接 JSON”包含项目最新正文与完整讨论，兼容 agent-docs-workspace v1；“载入回复”合并同一工作区的回复及讨论，相同 ID 的重复内容跳过。正文修改建议会作为待处理讨论展示，由用户核对后通过原文编辑器采纳。讨论中的署名用于记录发言者。通过 Agent 会话视图可连接真实 Codex，并将选定讨论转发给该会话；回复可人工加入讨论，来源会话和 item ID 随消息保留。

## Codex 会话

本机需要已安装并登录 Codex CLI。首个适配版本核对于 codex-cli 0.160.0，运行时通过 PATH 查找 codex；Go 产品不依赖 Node.js。通过网页的“Agent 会话 → 启动 Codex”创建，或使用专用命令：

```sh
./bin/agent-workbench connect --agent-name Codex /完整/项目目录
```

可在目录前指定 `--model`、`--sandbox read-only`、`--approval on-request`；默认沿用模型配置，使用 workspace-write 与 on-request。`--codex-bin` 仅用于本机明确指定可执行路径。专用命令启动新的 app-server 会话并将日常输入交给网页，当前不接管已有 TUI。

发送任务前先预览，选择是否附上当前文档或一条讨论。忙碌会话排队，失败或中断后队列暂停；可人工继续或清空。原生审批和问题在网页响应，普通聊天不替代审批。“停止回合”中断当前任务；“关闭会话”结束本服务创建的运行时。

原始事件保存在 `.agent-state/sessions/`。网页默认显示最近 100 条可读事件，勾选原始事件或展开“查看完整原始日志”按序补读历史。网页重连不停止任务；服务重启后会话标为已断开，点击“恢复会话”尝试恢复记录的原生 thread，不自动重发结果不明的输入。Native isSecret 回答在工作台日志中遮蔽。

验证见 [阶段二检查](docs/reviews/codex-access.md)。运行 Go 测试用 `go test -race ./...`；前端并发检查为 `node scripts/agent_ui_test.mjs`，真实 Chrome 集成检查为 `node scripts/browser_smoke.mjs`。浏览器检查使用明确标注的测试协议进程；真实 CLI／模型另行验证。
