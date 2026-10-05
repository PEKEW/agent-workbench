# 阶段一实现计划

**Goal:** 交付 Go 编译的本地项目入口与文档阅读器。

**Architecture:** 一个进程管理一个项目，内嵌模板与网页，原生 HTTP 接口只监听回环地址。

**Tech Stack:** Go 标准库、HTML/CSS/JavaScript；沿用现有文档渲染库。

**Spec:** [项目接入规格](specs/project_access_spec.md)。

## 约束与评审重点

保留文件与 Git 状态；拒绝覆盖；相对路径区分同名文档；项目名正确编码；缺项仍可阅读；符号链接与跨站请求不得暴露任意文件。无需数据库、会话框架或默认 worktree。

## 任务一 项目目录与模板

文件：internal/project/project.go、project_test.go、template/。
接口：`Create(path, name string)` 与 `Open(path string)` 返回 Project；`Snapshot()` 返回 Info；`ReadDocument(path string)` 返回正文。

- [x] 先用真实临时目录测试创建、拒绝覆盖、只读打开、缺项与越界读取，运行测试观察失败。
- [x] 内嵌固定模板，实现复制、JSON 编码、文件索引与受限读取。
- [x] 运行项目测试，确认已有文件和不同路径 README 保留。

## 任务二 HTTP 与 CLI

文件：internal/server/server.go、server_test.go；cmd/agent-workbench/main.go、main_test.go。
接口：New(token string, project *project.Project) 返回 http.Handler；POST 绑定项目，GET 读取状态或正文。

- [x] 先测试令牌、外部来源、绑定冲突、错误输入与文档读取，观察失败。
- [x] 实现 HTTP、命令、监听、浏览器启动与退出。
- [x] 运行测试及真实命令，验证退出码和单项目边界。

## 任务三 网页与交付

文件：internal/server/web/ 下页面、样式、脚本与渲染依赖；README、PLAN、进度与评审报告。

- [x] 实现创建／打开表单、文档列表、Markdown 阅读、缺项提示和刷新。
- [x] 浏览器验证新建、打开、同名文档、脚本隔离及窄屏布局。
- [x] 运行 go test -race ./...、go vet ./...、go build、文档检查，记录结果。
- [x] 保存本地提交，启动工作台并提供访问入口。

## 范围补充

按用户要求增加隔离 iframe，读取 docs/diagrams 的独立 HTML；附 Archify 生成的架构设计示例。验证包括隔离父页面、阻止网络请求和保留主题交互。
