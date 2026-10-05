# 第三方组件

## Markdown 阅读

internal/server/web/vendor.js 复用 HTML-for-LLM 提交 2388bf94232bef4ea6c2d21b7344908d2783d105 的离线打包资源，保留原版权头。包含 Marked 18.0.14（MIT）与 DOMPurify 3.4.16（Apache-2.0 或 MPL-2.0，本项目保留 Apache-2.0 文本）。许可文本位于 internal/server/web/licenses/。

上游：https://github.com/markedjs/marked 与 https://github.com/cure53/DOMPurify。

## Archify

示例架构图由 Archify 提交 a21ed05c9e9c910b66cae912e642ad044302720d 生成。源 JSON 与独立 HTML 保存在 docs/diagrams，许可为 MIT，完整文本见 docs/diagrams/LICENSE-archify.txt。工作台仅显示独立 HTML，运行时不加载生成器。

上游：https://github.com/tt-a1i/archify。
