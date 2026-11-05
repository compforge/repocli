# Component 的 Depot

Component 的 `language`、`tools` 和可选 `depot` 均复用 common。
RepoKit 的 `ComponentBinding` 另保存 root、manifests 和 `toolEvidence`；发现证据不进入 Tool。
`Tool` 表示项目声明的开发工具，不代表该工具已经安装或可执行。

一个 Component 在当前 checkout 对应零个或一个逻辑 Depot，Depot 包含多个 Bin。
Bin 的 `path` 是规范化的 POSIX 仓库相对路径，`kind` 为 dependencies、output 或 cache。
不同 Component 可以引用同一物理 Bin；目录尚未创建、已经回收，不改变逻辑声明。
依赖安装的一次操作可以准备多个 Component 共享的 Bin。

## 静态发现

Go、Python、TypeScript 使用同一组 conformance 用例，inspect、diff、view 解释各自捕获版本的材料。
发现阶段不扫描忽略目录、不安装依赖、不执行构建工具。

当前原生规则：

- Node package.json 对应安装根下的 node_modules。
- uv 项目的 pyproject.toml 与安装根 uv.lock 对应 .venv。普通 Python manifest 不猜测虚拟环境。
- npm/Yarn package.json workspaces、pnpm-workspace.yaml packages、tool.uv.workspace members/exclude
  决定安装根。仅有父级 lockfile 不构成 workspace 成员关系。
- 成员模式支持相对目录、分段 `*` 和跨层级 `**`，支持排除；未支持的模式、畸形配置不猜测 Depot。
  Yarn PnP 不声明 node_modules Bin。
- 同一目录包含 Node 和 uv manifest 时，两类 Bin 放在同一个 Depot，不受主要 language 限制。

未知位置保持未声明。Go 全局 module cache、conda 全局环境、动态构建配置中的输出位置不自动发现。
common 支持 output/cache Bin，但 RepoKit 不凭 dist、vendor 等目录名推定可删除资源；当前只自动
发现上述 dependency Bin。显式 worktree outputs 保留原有项目授权语义。

## 准备与回收

现有 Python `inspect_dependencies` / `prepare_dependencies` 提供依赖运行态观察与显式锁定安装，
详见 [validation](validation.md)。安装的回执属于 RepoKit，common 不保存命令、回执、占用或就绪状态。

现有 worktree retention 负责显式的环境回收，见 [worktrees](worktrees.md)。Depot 声明本身不授权删除：
仍需回执或项目 outputs 策略、Git ignore/tracked 检查、checkout 保护及删除恢复 journal。
共享目录按物理位置处理；tracked 文件、符号链接、嵌套 checkout 不因为存在 Bin 声明而变得可删除。

## 验证输入与复用

Go/CLI validate 自动为输入范围内的 dependency Bin 记录 `depotWitnesses`，调用方无需维护第二份缓存。
证据绑定物理目录身份与准备回执内容；对应安装根的源码子树保守加入输入范围，防止子组件显式声明
遗漏 workspace 根锁文件和其它成员的 manifest。

缺失、无回执、回执无效或不支持的物理位置仍可执行真实检查，但不能复用历史成功。
开始执行前和整轮结束后复核这些证据；回收、替换目录或修改回执会使旧计划失效。
回执不是对所有依赖文件逐一校验的完整性证明，任意手工修改目录内部内容不在这一保证内。
outputs/cache 不作为 dependency witness，不因构建写入忽略的 dist 而使本轮自我失效。

未发现的外部依赖或动态资源仍应由调用方通过显式 input witnesses 或 DisableReuse 声明。
验证不隐式安装依赖，真实检查失败保留原始命令和输出。
