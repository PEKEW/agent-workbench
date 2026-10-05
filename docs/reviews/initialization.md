# 项目初始化记录

日期：2026-10-05。

使用 working-template-semi-harness 提交 `b599fb0b0b0de887aeb802174171eeafb9f24b09` 的原创建器建立项目。HTML 模板提交为 `2388bf94232bef4ea6c2d21b7344908d2783d105`。完整讨论和用户导出按原字节保存，摘要见 discussion/sources.json。

本项目适配：使用本机执行；通用规则进入 AGENTS，CLAUDE 保留专用入口；progress 与工作流统一为精简记录；检查器说明按实际能力修正；保留原检查逻辑和规则生成脚本。

初始化只建立目录、规范、规格和历史证据，没有服务、CLI 产品入口或前端实现。模板创建器默认不复制自身；未来产品复用创建能力的交付位置在首期实现规格中确定。

## 验证

- `python3 scripts/check_markdown.py`：全部通过。
- 三个讨论快照文件的 SHA-256 与来源清单一致。
- project.json 列出的当前态文档与目录均存在。
- PLAN 与 progress 均在模板行数限制内。
- 产品测试未运行：当前为空项目，没有产品代码。
