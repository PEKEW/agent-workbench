# Agent Workbench

以 semi-harness 项目结构为基础的本地 Agent 网页工作台。一个工作台绑定一个项目，在浏览器中打开计划、设计、证据和交互图。

当前版本提供项目新建、已有项目打开、文档阅读和 iframe 图展示。Agent 会话、任务分配、批注编辑和消息转发尚未接入。

## 使用

本机已构建的程序：

```bash
./bin/agent-workbench
./bin/agent-workbench open /Users/peke/agent-workbench
./bin/agent-workbench new --name my-project /已有父目录/my-project
```

默认自动打开浏览器。使用 `--no-browser` 只打印地址，`--port 端口` 指定端口；选项须放在目录参数之前。Ctrl+C 关闭服务。打开另一个项目时启动另一个进程。

新建目标目录必须不存在；打开已有项目只读加载文件，不执行项目脚本。缺少规范或计划时页面显示提示，文档刷新可读取外部编辑的最新内容。访问地址含本次服务凭证，请使用终端给出的完整地址。

## 交互图

把 Archify 生成的独立 HTML 放在项目的 `docs/diagrams/`，刷新后在左侧“交互图”中打开。HTML 在隔离 iframe 中运行，可使用节点查看、主题和路径等本地交互。嵌入模式不支持网络请求、弹窗和下载；需要完整导出功能时可在浏览器单独打开原 HTML 文件。

本项目附带阶段一架构设计图及 JSON 源。工作台只读取成品，不需要 Archify 或 Node.js 运行环境，也不做双向编辑或自动生成。

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
