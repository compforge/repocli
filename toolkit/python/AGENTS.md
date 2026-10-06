# Python 仓库工具包

## 项目定位与边界

`compforge-repocli`（导入名 `repocli`）提供原生同步 inspect / owner，通过 Git CLI 读取版本控制事实，
不包装 repocli CLI。身份复用 harness-common；布局与工具证据归本包，消费方拥有执行策略。

## 代码地图与核心模块

```text
repocli/
├── __init__.py / model.py  # 公共 API、common 派生类型和 owner 查询
├── _inspect.py            # 输入版本、整次调用预算与一致性观察
├── _git.py / _process.py  # 文件目录、批量对象读取与有界 Git 子进程
├── _layout.py             # 纯布局投影、配置和包工具证据
└── _language.py / languages.json # 固定依赖生成的文件名识别目录
```

## 关键约定

- 公共入口使用 Python 命名与 frozen dataclass；未知身份不伪造，完整性保留诊断。
- Git 子进程与元数据读取共享 deadline；超时、取消或超出输出容量时终止并回收子进程。
  按目录和对象批次读取，不为每个源文件启动 Git。
- 配置只作静态数据；不读取源码内容，不执行目标项目，不保存消费方状态。
- 生成目录由根 `make generate-languages` 更新，不能独立修改；三种语言共用 conformance。
- 使用 `uv sync --locked` 准备依赖；改动后运行 make fix/lint/test/build。uv.lock 由 uv 生成。
  Python 包版本由 pyproject.toml 管理，同时遵守根 VERSION 约定。

## References

- [共享识别契约](../../docs/repository.md)
- [调用方式](README.md)
