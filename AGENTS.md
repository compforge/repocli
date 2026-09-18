# repocli

## 项目定位与边界

repocli 是独立的仓库命令行工具。`diff` 命令只描述变更源码、变更符号和可能受影响的测试文件，
不关心调用方如何消费结果，也不运行项目命令。`snapshot` 只报告观察到的仓库内容身份与完整性，不创建备份或判断检查结果是否可复用。

## 代码地图与核心模块

```text
main.go           # 进程入口、信号与退出码
cmd/              # Cobra 根命令、子命令、参数与输出适配
VERSION           # 版本号，由入口嵌入二进制
internal/
  analysis/       # 仓库快照、变更分析与组件归属的编排
  git/            # 只读 Git 基线与工作区快照
  diff/           # patch 解析、变更行与内存中的 postimage 重建
  syntax/         # gotreesitter 适配，提取独立于语法树生命周期的事实
  codegraph/      # 通用局部构图、本地关系解析与带证据的关系查询
  impact/         # diff 起点、测试候选、影响筛选与缺口归属
  project/        # common 身份、组件布局与语言发现、产品关联和文件归属
docs/codegraph.md # 通用图概念、构图流程与边界
docs/diff.md      # diff 消费图的流程和关键取舍
```

## 关键约定

1. `diff` 和 `snapshot` 对目标仓库只读：不修改 index、工作区、配置或目标仓库依赖，不执行目标仓库代码。所有命令的执行日志由统一 CLI 入口写入用户目录 `~/.repocli/logs/`，覆盖输出及参数解析错误，不进入分析结果或领域逻辑，子命令无需自行接入。
2. `sourceFiles` 描述变更事实，`testFiles` 描述静态影响估计；不要把结果变成 lint/test 执行策略或通过证明。
3. 无法解析、扫描不完整和依赖不明确必须出现在结果里；不能用空列表掩盖分析失败。
4. Cobra 命令位于 `cmd/`，仅负责参数、调用与输出；`internal/analysis` 及其下游不依赖 Cobra。语法树、Git 命令和外部库对象留在适配边界内，Go 包不是对外兼容接口。
5. 变更前后源码与 diff 必须对应。删除和重命名不能只依赖当前源码推断。
6. 公共仓内容保持平台中立，不包含内部地址、个人机器路径、凭据或公司专属逻辑。
7. Forge / Repository / Product / Component 直接使用 quality-harness 的 Go common 类型；本仓拥有目录与语言发现，不能把 checkout 路径或执行策略塞入共享身份。Product 关联只读取显式声明。

8. CodeGraph 只理解起点、候选与代码关系；测试发现和影响策略属于 impact。构图按需展开，关系种类与确信度独立；推断边不能当作确定证据，目标未知时保留缺口。

9. 修改代码文件时，必须在同一 PR 中同步 bump 根目录 `VERSION`；默认升 patch，新增功能或不兼容变更按 SemVer 选择 minor/major。仅文档改动无需升级。`VERSION` 是二进制版本的唯一来源，发布 tag 必须为 `v<VERSION>`。

开发验证入口：`make fix`、`make lint`、`make test`、`make build`。关键行为用隔离的临时仓库和
多语言源码样例验证，不能执行被分析项目的测试来替代分析器自身的回归。

## References

- [README.md](README.md)：使用方式、输出语义、语言范围和限制。
- [docs/codegraph.md](docs/codegraph.md)：局部图的概念、流程、关系证据与支持边界。
- [docs/diff.md](docs/diff.md)：diff 分析模型、快照、影响传播和缺口边界。

- [docs/snapshot.md](docs/snapshot.md)：内容摘要、输入范围与完整性契约。
