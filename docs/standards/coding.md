# 编码规范

## 1. 类型提示（强制）

- 所有函数/方法的参数与返回值必须标注类型，`__init__` 返回 `None`
- 内置泛型：`list[str]`、`dict[str, torch.Tensor]`（不用 `typing.List/Dict`）
- 可空：`X | None`（不用 `Optional[X]`）
- **Tensor 用 jaxtyping 标注形状与 dtype**（见 §3），普通张量兜底 `torch.Tensor`

## 2. Docstring（强制，Google 风格，中文书写）

```python
def normalize_frames(
    frames: Float[Tensor, "b c h w"],
    gain: float = 1.0,
) -> Float[Tensor, "b c h w"]:
    """按增益归一化一批帧的亮度。

    Args:
        frames: 待处理的帧，取值范围 [0, 1]。
        gain: 亮度增益，取值范围 [0.2, 1.0]。

    Returns:
        归一化后的帧，取值范围 [0, 1]。

    Raises:
        ValueError: gain 超出取值范围时抛出。
    """
```

- 三引号 `"""`，首行一句话总结（中文）
- `Args:` / `Returns:` / `Raises:` 块，缩进 4 空格
- shape/dtype 由 jaxtyping 签名表达；docstring 里写**取值范围、坐标约定、单位**等类型系统表达不了的信息
- 公开函数/类必写完整块；私有辅助函数一行总结即可

## 3. 形状类型：jaxtyping + beartype（强制）

```python
from jaxtyping import Float, Int, jaxtyped
from beartype import beartype
from torch import Tensor

@jaxtyped(typechecker=beartype)
def resize_frames(frame: Float[Tensor, "b c h w"], scale: float) -> Float[Tensor, "b c h w"]:
    ...
```

- 维度名全项目统一：`b`（batch）`c`（channel）`t`（time/frame）`h w`（空间）
- 公开的张量处理函数必须 `@jaxtyped(typechecker=beartype)` 装饰——运行时校验形状，预实验阶段抓 bug
- 性能敏感的内层循环函数可豁免装饰器，但签名仍写 jaxtyping 类型（当文档用）

## 4. PyTorch 专项

- **device/dtype 不硬编码**：禁止 `.cuda()`；`.to(device)`，device 从配置/参数传入
- **nn.Module**：`__init__` 只建层和存超参；`forward` 无副作用、无 `print`
- **随机性**：实验入口显式 `seed` 参数，统一一处设置 `torch.manual_seed` / `numpy` / `random`
- **就地操作**：默认禁用 `_` 后缀就地算子（`add_` 等），确需省显存时加注释说明
- **形状变换**：优先 `einops.rearrange`（自带形状文档），避免裸 `view/permute` 连锁
- 评估/推理函数用 `@torch.no_grad()` 装饰，不在函数体内手动开关

## 5. 通用

- 行宽 **120**（与 nvim/stylua 一致）
- 工具链：`ruff format` + `ruff check` + `mypy`（宽松起步），配置入仓
- 命名：`snake_case` 函数/变量，`PascalCase` 类，`UPPER_CASE` 常量；张量名带语义（`frame_orig`、`flow_fwd`），禁 `x2`/`tmp` 出现在实验脚本
- 实验参数走 `argparse` 或 config 文件，禁止改源码魔法数字跑实验
- 每个实验产出目录含：config 快照 + git commit hash + 结果文件
- **不做向后兼容**：接口、参数、数据格式、文件布局被新版本替代时，在同一次修改中直接删除旧实现，并同步更新所有调用方。禁止保留兼容分支、旧名别名、双格式读写、废弃警告过渡期（deprecation shim）。本仓库是单团队研究代码，没有外部使用者，兼容层只会累积死代码。过期文档同理：压归档存根或删除，不保留并行旧版。

## 6. 注释语言与 review 问题标记

- **注释只写代码本身表达不了的约束**（取值范围、坐标约定、单位、外部系统的行为限制）。
  **不写历史叙事**：日期、决策出处（"XX 用户决策"）、事故经过、修复过程、
  "已删除 / 不再 / 不是 XX"式的否定说明——这些属于 `progress.md` 与 git 历史。
  判据：注释对**从未见过旧版代码的读者**是否仍有信息量；没有就删。

- **所有代码注释与 docstring 用中文书写**。例外保持原样：Google 结构关键字（`Args:` / `Returns:` / `Raises:`）、标识符与参数名、文献/作者名、单位（deg、ms）、issue 编号（I7、B10）。
- **用户可见消息也用中文**：异常消息、`warnings.warn`、print/日志输出。f-string 插值、字面选项值（如 `'relative'`）、单位保持原样。
- **图表文字（matplotlib title/label/legend）保持英文**：默认字体无中文字形会渲染成方块，且图表可能直接用于英文论文。
- **`# ?` 问题标记**：以 `# ?` 开头的注释（`#` 与 `?` 之间空格数量不限，如 `# ? ...`、`#? ...`、`#  ? ...`）是作者 review 代码时留下的问题。
  - 收到处理指令时，用 `#\s*\?` 全局搜索所有此类注释，**回答直接写在问题注释的下一行**，以 `# 答：` 开头，不另写文档。
  - 保留原问题注释不删除，问题与回答成对出现，由作者确认后自行清理。

## 7. 工具配置（pyproject.toml）

```toml
[tool.ruff]
line-length = 120
target-version = "py310"

[tool.ruff.lint]
select = ["E", "F", "W", "I", "N", "UP", "B"]
ignore = ["F722"]  # jaxtyping 的形状字符串会误报 forward-annotation 语法错误

[tool.mypy]
python_version = "3.10"
check_untyped_defs = true      # 宽松起步：检查已标注代码，不强制全标注
ignore_missing_imports = true  # torch/第三方库 stub 缺失不报错
# 后期收紧路径: disallow_untyped_defs = true -> strict = true
```

## 下一步

- 写代码前读本文件 + `workflow.md`；收尾按 `review.md` 五问自查。
