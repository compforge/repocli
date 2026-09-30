# repocli 内核

repocli 面向 Git Repository 捕获内容、识别仓库结构，再由各命令报告内容身份或变更及其关系证据。
它提供可组合的仓库工具；调用方决定如何使用结果。

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

### Snapshot 与 Comparison

Snapshot 表示从一个指定输入观察到的仓库内容及捕获缺口。输入可以来自工作区、index 或 commit；
内容摘要用于比较观测结果，不创建备份，也不保证文件系统原子性。可变输入前后的复核用于发现并发变化。

Comparison 将指定的前后内容版本配对，产生文件状态、变更行和前后源码。patch 输入由匹配的基线在内存中
重建另一版本。删除、重命名和旧依赖必须依据对应版本解释，不能仅以当前磁盘内容代替。

内容身份、源码目录和解析资源有不同用途。子模块的内容可以参与摘要，显式引用的配置可以成为解析资源，
但这些都不意味着子模块源码、组件和测试进入父仓分析范围。Git 跟踪及忽略规则决定输入，嵌套的独立仓库
保持自己的边界。具体捕获和摘要契约见 [snapshot.md](snapshot.md)。

### Document、File、Symbol 与 CodeGraph

共享 CodeGraph 组合 GoGraph 的属性图与 Cypher、gotreesitter 的语法与事实提取、pond 的异步调度，
让调用方通过图访问源码信息。它拥有代码身份、关系绑定、来源位置、证据强度与局部诊断。
repocli 负责仓库定位、Git 内容捕获及消费方式。依赖探索消费 Extractor 产出的 Facts，
结束后将材料与仓库模块上下文交给 Builder，一次构图。比较两侧可复用单文件提取，绑定仍按版本隔离。

Document 是共享 CodeGraph 的输入材料及对应图节点；Function、Class 等具体声明通过 contains
归属到 Document。File 是 repocli 对仓库路径的称呼，symbol 是声明的统称。影响分析中的
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
成功退出表示命令完成，不表示目标项目正确。是否扩大测试、重试或接受报告由调用方决定，
repocli 不执行目标项目命令。

## 主流程

```text
CLI 请求
  → 定位 Checkout，确定输入版本和范围
  → 捕获内容与缺口
  → prepare：按内容版本准备 Repository / Component / 语言 / 包工具证据
  → 命令能力消费同版本内容与上下文
      snapshot：报告内容身份、捕获完整性与仓库上下文
      view：源码材料 → 共享 CodeGraph → 本地 HTTP API → 图与快照源码页面
      diff：Comparison → changeset + testset → 有界 workset → 按版本构图
                            → 变更 File / Symbol 查询 → 与 testset 相交
                            → 已知测试关联、证据与缺口
             并附加 Repository / Component / Product 上下文
  → 复核可变输入，报告观察到的并发变化
  → 输出命令报告与退出状态；统一入口记录重试参数与耗时
```

仓库命令共用 analysis 的 prepare，将内容与 project 发现的上下文绑定到同一版本。
`snapshot` 输出准备结果，`diff` 对前后版本分别准备后协调比较与归属，`view` 消费准备结果构图。
prepare 复用已捕获内容，不用工作区元数据替换 index、commit 或 patch 的上下文。
help/version 不进入仓库准备。具体识别规则见 [仓库上下文](repository.md)。

## 分层与关键边界

### 适配层与分析能力分开

CLI 和带版本的 JSON 报告是兼容边界，Go 包是内部实现。Cobra 负责参数、输出和退出码，
分析能力通过普通 Go 请求与结果协作，不依赖 Cobra 或某个 agent/plugin 运行时。

`git` 封装 Git 和内容读取，`diff` 解释 patch 与变更行，`project` 负责结构发现与归属。
`viewer` 承载本地 HTTP 与内嵌静态页面，仅展示 analysis 提供的图和捕获源码。
`analysis` 协调这些能力，`impact` 负责变更起点、测试候选和结果筛选，`codegraph` 适配共享 CodeGraph 的
声明与调用事实，并负责仓库上下文解析、局部构图及关系查询。共享图库对象和语法树都不穿透到业务消费者。

新增命令按其需要组合已有能力，不把命令名称、验证策略或调用方状态塞入通用图、快照或共享身份。

### 目标内容只读且不执行

分析命令读取目标仓库，不修改 index、工作区、配置和依赖，不执行仓库代码、构建脚本或测试。
配置只在声明的静态模型中解释；遇到缺失依赖和不支持的行为，报告边界，不通过运行目标代码补齐知识。
自动下载依赖、初始化子模块和向远端写入也不属于分析过程。

源码读取、语法解析和图扩展受时间与容量约束，缓存仅复用适用上下文中的事实。达到边界时保留已取得的证据，
并报告缺口或失败，不能把截断结果标为完整。具体限制随所属能力维护。

### 执行观测属于统一入口

执行记录用于定位耗时最久的 case 并按原参数重试，统一入口保留工作目录、参数、版本、耗时与退出状态。
成功的 help/version 调用不初始化日志，避免查询工具用法依赖可写的用户目录。
子命令自动继承记录机制，领域能力不负责日志文件。日志与结构化报告分离，
记录失败不改变命令结果；原始输出位置保持不变。存储、保留和数据范围见 [logging.md](logging.md)。

## 专题文档

- [repository.md](repository.md)：命令共享准备、组件归属、语言与包工具证据。
- [diff.md](diff.md)：比较如何生成查询起点、筛选测试并归属缺口。
- [codegraph.md](codegraph.md)：局部关系模型、构图、语言解析与查询证据。
- [snapshot.md](snapshot.md)：输入捕获、摘要格式和内容完整性。
- [diff-usage.md](diff-usage.md)：diff 参数、输出契约、组件发现与语言范围。
- [logging.md](logging.md)：命令执行日志的格式、位置与保留规则。

- [代码图浏览](view.md)：本地 viewer 的快照、展示和刷新契约。
