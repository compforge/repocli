# repocli 内核

repocli 是 Repository 理解与操作工具包：结合仓库布局、代码、Git 版本和工具链解释仓库，并提供可组合的操作。
公共 API 提供结果与诊断，CLI 将这些能力映射为命令；调用方拥有业务流程与决策。

## 职责范围

| 能力域 | 理解与处理的对象 |
|---|---|
| 仓库布局 | 目录、文件角色、Component 与文件归属 |
| 代码理解 | 源码结构、实体与依赖关系；CodeGraph 提供图事实 |
| 变更分析 | 组合 Git、代码和布局事实，形成 Fragment / Unit 并分析影响 |
| Git | branch、worktree、index、commit 与远端状态，以及 commit、pull、push 等显式操作 |
| 构建与包管理 | build/package tool、manifest、依赖与产物约定，以及调用方请求的工具操作 |

这是能力归属，不表示所有语言工具包已实现全部操作；具体支持以各公共 API 和专题文档为准。
工具识别和操作机制由 repocli 持有，是否构建、选哪些验证、何时提交或发布由调用方决定。
静态分析保持只读；构建、包管理等执行能力通过显式操作接入，不作为补齐分析缺口的隐式步骤。

这些能力按任务组合。`diff` 是贯穿 Git、布局识别和代码理解的变更分析流程：Git 比较提供前后版本、
文件状态、patch 和内容，CodeGraph 提供各版本中的源码归属与关系，repocli 组合这些证据解释变更。
Git 比较与语义变更分析各自拥有其结果，不按命令名称各维护一套重复的 diff 事实。

Fragment / Unit 是 repocli 的变更概念；按关系强度和数量目标聚合的契约见 [Fragment 与 Unit](units.md)。

## 理念与核心概念

### Repository、Checkout、Component 与 Product

Repository 是稳定的仓库身份，由 Forge 和仓库路径组成；Checkout 是本次操作所在的本地工作区。
同一个 Repository 可以有多个 worktree，目录位置不参与仓库身份。无法确定远端身份时保持未知，
不能用个人机器路径充当身份。

Component 属于一个 Repository，表达仓库内的组成单元。组件根目录、语言及文件归属描述当前布局；
语言是元数据，不参与身份，关联的 Ecosystem 也不等同于某个包管理器。包工具从清单与锁文件中
识别并保留依据，不改变共享身份。组件边界用于归属和报告，
不证明组件之间没有依赖。

Product 通过显式声明关联 Component：一个 Product 可以使用多个组件，一个组件也可以服务多个 Product。
关系不从目录或仓库名猜测。Forge、Repository、Component、Product 复用
[quality-harness Go common](https://github.com/compforge/quality-harness/tree/main/sdks/go/common)
的中立类型；repocli 拥有布局发现和文件归属，不向共享身份加入 checkout 路径或调用方执行策略。

### Directory、File、Project Manifest

Repository 的内容由目录和文件组织，Component 是对某个目录的工程解释，不是仓库的全部内容。
普通目录可归属于 Component，也可无归属；仓库可以没有 Component，根目录也可以就是一个 Component。
目录路径使用仓相对路径，根为 `.`。Git 不记录空目录，tree 只表达版本条目隐含的目录。

File 的 Git entry kind（regular、symlink、gitlink）与语义 role 分开：Project Manifest 是描述项目的
普通文件，如 go.mod、pyproject.toml、package.json、Cargo.toml。它同时是工程边界候选、工具输入、
语言与包工具识别的证据；它的存在不证明该目录可独立构建。Makefile 是构建脚本，锁文件是解析结果，
均不独立触发组件发现。未知普通文件不需要强制归类。gitlink 是父仓的一条引用，保留 path、mode、OID。

`tree` 提供不依赖组件配置的基础内容视图；`inspect` 按显式声明或发现规则提供组件及 Manifest 证据。
Manifest 在 testdata 中仍可被 tree 识别，组件发现则可排除该候选。文件/目录归属取最深组件根；
无归属保持为空。归属不表示影响，根构建脚本可能影响多个组件，影响分析另行报告证据。
这些仓库事实不要求先构造 CodeGraph，也不强制把所有辅助文件与物理目录变成图节点。

### Snapshot 与 Comparison

Snapshot 表示从一个指定输入观察到的仓库内容及捕获缺口。输入可以来自工作区、index 或 commit；
内容摘要用于比较观测结果，不创建备份，也不保证文件系统原子性。可变输入前后的复核用于发现并发变化。

Comparison 将指定的前后内容版本配对，产生文件状态、变更行和前后源码。patch 输入由匹配的基线在内存中
重建另一版本。删除、重命名和旧依赖必须依据对应版本解释，不能仅以当前磁盘内容代替。

gitlink 仅以引用参与父仓快照。工作区可读取已初始化子仓的 HEAD，但不读取其文件；未初始化时保留
index OID。index/commit 仅使用对应版本的 OID，不要求子仓对象存在。跨 gitlink 引用的配置不在父仓
捕获边界内，分析报告缺口。独立仓库保持自己的边界，详见 [snapshot.md](snapshot.md)。

### Document、File、Symbol 与 CodeGraph

共享 CodeGraph 组合 GoGraph 的属性图与 Cypher、gotreesitter 的语法与事实提取、pond 的异步调度，
让调用方通过图访问源码信息。它拥有代码身份、关系绑定、来源位置、证据强度与局部诊断。
repocli 负责仓库定位、Git 内容捕获及消费方式。依赖探索消费 Extractor 产出的 Facts，
结束后将材料与仓库模块上下文交给 Builder，一次构图。比较两侧可复用单文件提取，绑定仍按版本隔离。

Document 是共享 CodeGraph 的输入材料及对应图节点，DocumentKind 区分源码、manifest、gitlink 等材料。
具体声明通过 declares 关联 Document，词法嵌套使用 encloses。消费侧从 node、relation 及其属性读取事实。
File 是 repocli 对仓库路径的称呼，symbol 是声明的统称。影响分析中的
file/symbol 投影服务于候选筛选，不替代共享图的具体节点类别。

view 从一个捕获版本构建完整共享图，保留节点、关系和诊断，供用户查看代码结构；它在页面上
按 Document 聚合或展开邻接关系。diff 由变更和候选范围驱动有界 workset，再补充仓库级 import、
语言配置和 package 成员关系。两者复用内容捕获和共享 CodeGraph；view 不经过影响分析的裁剪。

每张图属于一个内容版本。图、关系位置和显示源码来自相同捕获内容；刷新完整替换版本。
比较前后分别构图和查询，再合并结果，不能把两个版本中的边拼成一条证据路径。
关系聚合、距离衰减、测试推荐以及 Web 服务和布局由消费者决定。

### Changeset、Testset 与 Workset

这些集合由 repocli 拥有，不进入共享 CodeGraph 的业务模型：

- Changeset 是本次变更在对应版本中存在的 Document；变更 Symbol 是查询起点，不是材料集合。
- Testset 是依据调用方测试范围和受支持测试命名发现的候选材料；只有其中的文件可以成为测试结果。
- Workset 是 changeset 与 testset 加上仓库依赖展开所得的实际分析材料，包括连接两者的中间文件。
  它受深度、文件数和源码边界约束，不承诺依赖闭包完整；未完成的展开保留缺口。

每个版本从空图开始，先从捕获目录选出 workset，再批量加入共享 CodeGraph，复用图中的声明定位变更 Symbol。
repocli 补充仓库解析关系，沿变更起点反向查询，最后与 testset 相交。依赖文件不因入图而成为测试候选。
前后版本分别执行此流程；删除和重命名使用各自版本的材料与路径。源码目录可以大于 workset，
目录中的无关文件不因为被捕获而解析。未请求测试影响时只处理 changeset，不展开测试及其依赖。

### 事实、推断与完整性

变更文件和内容摘要描述观察到的事实；测试影响是基于静态代码关系的估计。输出应保留事实与推断的区别，
可报告的已知结果不因存在缺口而消失，未知部分也不能被空列表掩盖。

测试推荐采用 best-effort 语义：有目标的确定关系和推测关系都参与查询，路径保留关系的确信度与依据。
无法确定目标的依赖不入图；共享图报告的局部缺口保留为 observation。空测试列表不证明没有受影响的测试。

repocli 决定影响策略：自动选择查询起点，按路径最弱边的置信度优先、依赖距离其次选择解释。
路径距离用于排序，不转换成概率；共享 CodeGraph 的原始 confidence、basis 和位置不被改写。

完整性描述快照、工作集和仓库解析是否存在影响当前范围的执行缺口。
声明或关系提取缺口保留其 subject、位置和覆盖计数，不自动扩散为所有候选的缺口；
与变更行相交的声明缺口使起点扩大到文件。配置读取失败、不可用边界和预算耗尽按消费者范围判定；
`complete: true` 不承诺静态依赖或运行时覆盖完整。快照捕获完整性保持独立。
成功退出表示命令完成，不表示目标项目正确。是否扩大测试、重试或接受报告由调用方决定；
分析过程不执行目标项目命令。

## 主流程

| 命令 | 用户问题 | 必要工作 | 输出边界 |
|---|---|---|---|
| tree | 仓库包含什么？ | 枚举同版本路径、类型和已知角色 | Directory、File、Manifest；不依赖组件识别 |
| inspect | 仓库如何组织？ | 枚举文件路径，读取必要配置 | Repository、Component、语言、包工具证据 |
| snapshot | 是否为同一份仓库内容？ | 捕获内容，计算摘要，报告捕获缺口 | 内容身份及完整性；不解释项目配置 |
| diff | 改了什么，可能影响什么？ | 比较前后版本，识别布局，按 workset 构图与查询 | 变更、组件影响、测试候选、证据与缺口 |
| view | 如何理解当前代码结构？ | 捕获内容与布局，构建共享代码图 | 同版本图、源码、结构上下文的交互视图 |

各命令共用输入选择、Git 边界和适用的能力，不强制经过一次包含所有分析的 prepare。
inspect 共享 project 的识别规则；snapshot/view 共享内容捕获；diff 的基线和 postimage 使用同一套
识别规则。source capture、结构识别和代码静态分析分别按命令需求组合。

quality-harness common 提供 Repository / Component / Product 的中立身份；repocli 拥有路径布局、
组件发现、文件归属、工具证据、依赖探索和影响策略。CodeGraph 负责代码实体与关系的静态事实。
一个 manifest 的存在可以作为工程组件候选，无需先得到其中的模块声明；manifest 不等于独立 Component。

三个完整性问题分别回答：inspect 的目录与必要配置是否观察完成；snapshot 的内容是否捕获完整；
diff 的影响分析是否存在缺口。它们不互相替代。view 在同一报告内分别保留内容捕获诊断与图诊断。
`snapshot` schema 2 不保留组件输出；结构查询统一使用 `inspect`，全仓内容指纹仍使用 `snapshot`。
help/version 不访问目标仓库。具体识别规则见 [仓库结构识别](repository.md)。

## 分层与关键边界

### 适配层与分析能力分开

Go 工具包 `github.com/compforge/repocli/toolkit/go` 是进程内的公共入口，提供 `Tree`、`Inspect`、`Snapshot`、`Diff`、
`Graph` 及其请求和结果类型。CLI 和 viewer 位于 `apps/cli`，消费这个入口；应用的 `cmd/repocli` 管理进程生命周期，
`internal/cli` 负责 Cobra 参数、输出和退出码。工具包负责验证程序调用的输入约束，不依赖命令行预校验。
公共 Go API 与带版本的 JSON 报告分别承担兼容责任；结果类型复用内部结构，避免额外复制和语义转换。
Repository、Component、Product 身份直接使用 quality-harness common；`ComponentBinding` 组合
`common.Component` 与本次解析的目录、产品关联和工具证据，不另造同名身份类型。
公开类型的可见字段与方法同样属于 API，不能借内部文件移动改变其语义。

Go 调用方通过 context，TypeScript 调用方通过 AbortSignal 与 timeoutMs、Python 调用方通过 Event 与 timeout 控制取消和超时，并负责结果持久化、日志及执行策略。工具包可调用 Git 读取材料，
直接在当前进程完成组织和分析，不启动 repocli CLI。CLI 日志和 JSONL 历史由适配层写入；库调用不创建
这些状态。静态分析可以沿调用方传入的 timeline context 记录阶段。

可复用能力按语言归入 `toolkit/go`、`toolkit/typescript` 和 `toolkit/python`，应用归入 `apps/cli`。
Go 工具包与应用分别拥有 go.mod 和 internal，编译器约束应用只能访问工具包公开的能力；
根 go.work 连接本地源码，各模块的依赖和发布身份保持独立。语言实现按实际能力独立演进，
共享输入版本、组件归属、完整性和诊断契约；相同用例的结果应可对照验证。TypeScript 与 Python 库直接完成所支持的
解析工作，公共入口包括 `tree`、`inspect`、`owner`、工作区 `snapshot` 和 Git 查询。Python 的 Git/Forge 操作和 Go 的 worktree 操作直接由库提供，CLI 按自身需求选择暴露哪些能力。身份类型分别使用 `@compforge/harness-common` 与 `harness-common`；
布局和输入版本通过 `conformance/inspect` 共享语料校验。文件名识别元数据由固定版本的 Go 依赖生成，
TS/Python 运行时依赖 Git，不依赖 Go 或语法解析器。消费者的环境准备和验证门禁不进入工具包。

Go 工具包内部的 `git` 封装 Git 和内容读取，`diff` 解释 patch 与变更行，`project` 负责结构发现与归属。
`viewer` 承载本地 HTTP 与内嵌静态页面，仅展示 analysis 提供的图和捕获源码。
`analysis` 协调这些能力，`impact` 负责变更起点、测试候选和结果筛选，`codegraph` 适配共享 CodeGraph 的
声明与调用事实，并负责仓库上下文解析、局部构图及关系查询。共享图库对象和语法树都不穿透到业务消费者。

新增命令按其需要组合已有能力，不把命令名称、验证策略或调用方状态塞入通用图、快照或共享身份。

### 分析只读，操作显式

分析命令读取目标仓库，不修改 index、工作区、配置和依赖，不执行仓库代码、构建脚本或测试。
配置只在声明的静态模型中解释；遇到缺失依赖和不支持的行为，报告边界，不通过运行目标代码补齐知识。
自动下载依赖、初始化子模块和向远端写入也不属于分析过程。

源码读取、语法解析和图扩展受时间与容量约束，缓存仅复用适用上下文中的事实。达到边界时保留已取得的证据，
并报告缺口或失败，不能把截断结果标为完整。具体限制随所属能力维护。

CLI 安装维护由应用层的 `upgrade` 命令拥有：显式查询和安装稳定 Release，不访问目标仓库，
不改变库依赖，分析调用不会触发它。详见 [CLI 更新](upgrade.md)。

### 执行观测属于统一入口

执行记录用于定位耗时最久的 case 并按原参数重试，统一入口保留工作目录、参数、版本、耗时与退出状态。
成功的 help/version 调用不初始化日志，避免查询工具用法依赖可写的用户目录。
子命令自动继承记录机制，领域能力不负责日志文件。日志与结构化报告分离，
记录失败不改变命令结果；原始输出位置保持不变。存储、保留和数据范围见 [logging.md](logging.md)。

## 专题文档

- [repository.md](repository.md)：inspect、共享组件归属、语言与包工具证据。
- [diff.md](diff.md)：比较如何生成查询起点、筛选测试并归属缺口。
- [codegraph.md](codegraph.md)：局部关系模型、构图、语言解析与查询证据。
- [snapshot.md](snapshot.md)：输入捕获、摘要格式和内容完整性。
- [diff-usage.md](diff-usage.md)：diff 参数、输出契约、组件发现与语言范围。
- [logging.md](logging.md)：命令执行日志的格式、位置与保留规则。

- [代码图浏览](view.md)：本地 viewer 的快照、展示和刷新契约。

### 仓库操作与开发流程

Git 操作提供明确路径的 staging、commit、push、rebase 和 worktree 创建/删除；Forge 客户端提供
PR/MR、评论与 release 的平台适配。调用方提供目标、凭据与操作参数，拥有任务归属、验证门禁、
操作顺序、合并授权与清理时机。库不读取调用方配置或持久化其 session、验证结果。

Git index 查询保留路径、mode 与对象 OID；gitlink 也是文件条目，注册与提交准入由消费方判断。
验证式暂存提供候选 index 的一致视图，跨文件判断依据该视图中的内容。

Git 默认保留脏工作区；强制删除和 lease push 必须显式指定。操作超时可能发生在副作用之后，
GitResult 的 uncertain 要求调用方先核对状态再重试。分析过程不会隐式触发操作。
具体能力与返回契约见 [operations.md](operations.md)。
