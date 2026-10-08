# 会话控制实现计划

> 执行方式：superpowers:executing-plans，在当前项目根目录执行。用户已要求实施三项功能；保留既有修改，无默认 worktree，不自动提交 Git。

**目标：** 会话可清理、执行有交互提醒、网页授权配置实际生效。

**架构：** Manager 保存带版本的项目授权配置，严格匹配后回应已有原生审批；清理仅删除所选本地会话记录。前端使用持续状态与页面提醒。

**技术：** Go 标准库、JSON、现有 HTML/JavaScript 与 Chrome 协议测试。

**规格：** docs/specs/session_controls_spec.md。

- [x] 先加入清理、简单命令匹配、自动授权／撤销、持久化与范围隔离测试，确认旧实现失败。
- [x] 实现 Manager 清理及授权状态，并接入固定 HTTP 接口；无任意原生 RPC 透传。
- [x] 加入删除入口、授权配置弹窗、持续等待指示和页面提醒，保留草稿与焦点。
- [x] 运行 race、vet、build、前端与 Chrome 回归，检查截图与评审结果。
- [x] 更新 README、PLAN、progress 与详细证据，空闲时备份并更新本地服务。
