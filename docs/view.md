# Repository 浏览与布局设计

`repocli view` 是帮助 reader 理解当前 Repository 的本地浏览工具。读者先通过 Component
理解项目如何组织，再通过 graph 找到值得阅读的节点、沿关系阅读源码，通过 diagnostics
判断当前分析有哪些缺口。布局围绕这三个阅读任务组织。

本文汇总当前布局、交互与设计理由，是 viewer 的统一设计说明。

```sh
repocli view
repocli view --repo /path/to/repo --addr 127.0.0.1:5484
repocli view --max-documents 4000 --timeout 3m
```

命令打印浏览器 URL，Ctrl+C 退出。地址限于 loopback；端口设为 0 时自动分配。
`--timeout` 限制每次捕获和构图，不限制浏览会话时长。`--json` 不适用于该命令。

## 模型与流程

```text
工作区 → Git 内容捕获 → Document → CodeGraph
                                 → 节点、关系、诊断 + 同版本源码
                                 → Go HTTP server → Cytoscape.js
```

CodeGraph 组合图引擎、语言提取和异步执行，提供代码图事实；repocli 拥有仓库输入与浏览体验。
viewer 直接读取共享图，保留原始节点/关系 ID、具体类别、源码位置、confidence、basis 和 marker。
它不经过 diff 的影响路径筛选，也不把 repocli 的配置范围或测试推荐混成共享图事实。

## 页面布局

页面提供三个独立 tab，共用同一份快照。首次打开进入 Component；切换 tab 保留当前图的位置与详情。
仓库身份、当前视图的统计、Refresh snapshot 和快照标识位于公共区域，帮助读者确认正在阅读哪个仓库、哪个版本。

| Tab | 回答的问题 | 主要布局 | 设计思路 |
|---|---|---|---|
| Component | 项目由哪些部分组成，它们如何嵌套？ | 按组件根目录组织的树与信息卡片 | 先建立项目组织结构，再进入代码细节 |
| graph | 哪些节点值得读，它们如何协作？ | 左侧阅读入口、中间图与顶部工具栏、右侧详情 | 从整体关系进入局部，再用源码和证据验证理解 |
| diagnostics | 哪些材料或关系没有被完整分析？ | 独立搜索框与诊断列表 | 集中解释图的缺口，让读者能判断结论的边界 |

### Component：组件嵌套树

Component 按最近父组件的根目录包含关系嵌套，缩进与连线表达层级。一个组件下可以包含多个子组件；
没有父组件的条目并列展示。根目录 `.` 显示为 Repository root。

每张卡片展示组件名称、语言、根目录、manifest、包工具及识别证据。目录归属决定这里的层级，
组件之间的调用或依赖由 graph 表达。将 Component 独立成页，可以先读项目布局，避免组件卡片与代码节点争夺画布。

### graph：围绕画布组织阅读流程

桌面布局如下；窄窗口下，详情区移动到图的下方，顶部工具栏允许换行。

```text
Repository identity                         Statistics / Refresh snapshot
Component | graph | diagnostics                         Snapshot identity

Explore          | Search / Node kinds / Relation kinds / Reset | Details
Top 10 / Fit     | View title / Shape and color legends          | Node or edge
Ranked results   |                                              | Source
or search hits   |                 Graph canvas                 | Evidence
                 | Visible counts / Scope / Display limits      |
```

| 区域 | 当前内容与交互 | 设计思路 |
|---|---|---|
| 左侧 Explore | 无搜索词时列出 Top 10；有搜索词时列出匹配节点，附类别、邻居数、入向/出向计数及位置 | 为不熟悉项目的读者提供起点，也为已有目标的读者提供定位入口；侧栏 Top 10 不决定默认画布范围 |
| 顶部工具栏 | 名称/路径搜索、Node kinds、Relation kinds、最右侧 Reset；筛选以浮层展开 | 将影响图阅读范围的操作集中放在画布上方，给结果列表留出高度 |
| 中间画布 | 全局图、Top 10 或选中节点的一跳邻域；配合形状、颜色、线型和大小 | 先看关系结构，再逐步聚焦具体符号；各视觉通道表达不同信息 |
| 右侧 Details | 节点身份、所属 Component、源码、marker，或关系原始证据 | 图负责导航，详情负责解释，读者可沿证据核对图上的判断 |
| 画布底部 | 当前绘制数量、候选总数、范围说明和显示上限提示 | 明确当前看到的只是哪个范围，避免把未绘制误读为不存在 |

#### 全局图、Top 10 与局部邻域

**默认 graph 是当前筛选范围的全局视图。** 全局表示没有聚焦到某个节点的一跳邻域；具体范围仍由
node kinds、relation kinds、confidence 与材料选择共同决定，并受绘制预算限制。

Top 10 是独立的阅读视图。点击 Top 10 按钮进入，再次点击返回当前筛选范围的全局图。
侧栏无搜索词时持续提供 Top 10 阅读入口，画布可以同时保持全局视图。

| 操作 | 结果 |
|---|---|
| 单击画布节点 | 在详情区查看节点、源码与排名依据 |
| 双击画布节点，或点击搜索/排名结果 | 聚焦该节点的一跳邻域 |
| 点击详情中的 Explore this neighborhood | 聚焦该节点的一跳邻域 |
| 单击关系边 | 查看聚合关系包含的原始关系与证据 |
| Fit graph | 调整缩放与居中，让当前已绘制的节点适应画布；筛选与视图模式保持原样 |
| Reset | 清空搜索、恢复默认筛选、关闭筛选浮层、清除节点详情并回到全局图；使用当前快照 |
| Refresh snapshot | 重新捕获工作区并构图，更新三个 tab 共用的数据 |

全局视图达到绘制预算时，优先展示中心性靠前的节点，并提示数量限制。Top 10 只绘制榜上节点之间
已有的关系；局部视图展示筛选范围内的一跳关系，达到上限时保留选中的节点。

#### 搜索与筛选

搜索在选中的 node kinds 内匹配名称与路径，更新左侧结果列表；选择结果后进入节点邻域。
输入搜索词本身不裁剪画布。筛选决定候选图，改变关系类型或 confidence 会重算中心性。

默认筛选为：

- Node kinds：Struct、Class、Interface；没有这些类型时选择 Function。
- Relation kinds：calls、references、extends、implements。
- Evidence：所有 confidence 等级。
- 材料范围：排除已标记的依赖、生成物、构建产物与测试夹具，以及 Go 测试文件和 test/tests/__tests__ 目录；可勾选包含这些材料。

筛选仍按具体 kind 选择。形状与颜色只是帮助识别的大类，不改变底层节点或关系类别。

#### 中心性：帮助选择阅读起点

当前按**不同语义邻接节点数**排序，可视为 degree centrality 的直接计数形式；入向与出向分别展示，
同一邻居在总数中只计一次，重复引用不会抬高排名。调用、引用、类型与模块等语义关系参与计分，
目录归属、声明与包含边不参与。

方法、字段和引用通过 CodeGraph 已有的 membership/enclosure 归入选中的类型，形成可读的聚合关系。
类型级视图因此可以表达一个 struct 与哪些其他类型发生联系，而不必先展开其每个方法和每次引用。

节点大小表达连接程度，Top 10 给出可解释的阅读顺序。连接多也可能来自日志、工具或基础设施类型，
因此中心性只是结构上的阅读提示；业务核心仍需结合 Component、名称和源码判断。

#### 视觉编码：形状、颜色、线型与大小

节点用形状区分大类，保留大小来表达连接程度：

| 节点大类 | Node kinds | 形状 |
|---|---|---|
| 类型 | Struct、Class、Interface | 菱形 |
| 可调用单元 | Function、Method | 圆形 |
| 字段 | Field | 矩形 |
| 其他 | 未归入以上类别的 node kinds | 六边形 |

关系用颜色区分大类，边与箭头使用同一颜色；选中关系时仍保留类别颜色：

| 关系大类 | Relation kinds | 颜色 |
|---|---|---|
| 调用 | calls | 蓝 |
| 引用 | references、aliases、decorates | 橙 |
| 类型 | extends、implements | 紫 |
| 模块 | imports、exports | 青绿 |
| 结构归属 | contains、encloses、declares、occurs_in、in_directory、in_namespace | 玫红 |
| 其他 | 未归入以上类别的 relation kinds | 灰 |

线型独立表达静态证据强度：exact 用实线，scoped、name_only、heuristic 等非 exact 等级用虚线。
聚合边只要包含非 exact 关系就使用虚线，完整依据保留在详情中。颜色回答“是什么关系”，线型回答
“证据确定到什么程度”，两者可以同时读取。图上分别提供节点形状、关系颜色和线型图例。

#### 节点排布

当前按可见节点数量选择排布：不超过 100 个节点使用力导向布局，将有关联的节点拉近；更大视图使用
网格布局，控制布局成本。Fit graph 在当前排布上调整视角，节点筛选或视图切换会重新排布。

几何位置用于浏览，不承载中心性排名或组件嵌套语义。读者通过节点大小和排名判断连接程度，通过
Component 树判断目录层级；当前没有手动选择排布算法的控件。

#### Details：从图追溯到证据

节点详情展示具体 kind、位置、标签、所属 Component、语义邻居计数、文档注释与 marker，以及原始
入边/出边和快照源码。邻居计数与原始关系条数分开展示：一个邻居可能对应多条调用或引用证据。

关系详情展示具体 kind、confidence、两端节点、位置，以及每条 Evidence 的 basis 与 confidence。
聚合用于减少画布噪声，详情保留原始证据供读者核对。

Directory 详情列出捕获到的直接子节点及标签；目录没有源码文本，其存在不代表所有内容都已捕获。
没有单一源码位置的 Package/Module 通过 declares 展示贡献文档。节点关联的文档诊断入口跳转到
diagnostics，并填入对应路径。

### diagnostics：独立查看分析缺口

diagnostics 汇总捕获诊断和图解析诊断，支持按路径、代码或消息搜索。条目保留位置、相关对象、关系
及可用的 outline，帮助读者解释某个文件为何缺失、某种关系为何无法确认。

该页与 graph 共用快照，诊断数量不表示代码缺陷数量。confidence 表达静态证据强度，不是改动影响
概率；没有诊断不证明分析完备，没有边也不证明无依赖。

## 快照与边界

捕获复用 snapshot/diff 的 Git 跟踪和忽略规则，并继承其文件数、字节、符号链接及子模块边界。
纳入捕获范围的 UTF-8 文本作为 Document；二进制不参与构图。仅 source 标签的材料参与符号解析，
已识别的清单仍提供模块元数据。配置、文档、资源和未知格式文本保留为 Document 上下文，
不因未进行符号解析而产生 unsupported_language 诊断。

物料分类统一消费 CodeGraph 的 TagMatcher；tooling 默认覆盖
`.idea`、`.vscode`、`.codebase`、`.devloop`、`.codex`、`.claude`、`.agents`、`.cursor` 等路径，
以及任意目录下的 `AGENTS.md`、`CLAUDE.md`、`GEMINI.md`。
repocli 不维护目录名列表，通过 ExcludedBy 检查材料和祖先目录标签，在文档预算前排除 tooling 类别。
同一 AdmissionPolicy 同时传给 Builder，保持选材与构图准入一致；被排除材料不占用文档预算；
构图还按所选快照中的 `.gitignore` 排除匹配文件，即使文件已被 Git 跟踪；嵌套规则和 `!` 例外由 Git 解释。
Git 内容快照仍按原有规则捕获这些文件。其他隐藏目录与普通说明文件仍按普通材料处理。
`.dockerignore`、`.gitignore`、`.env` / `.env.*` 等常见配置由共享 config 标签标注。

默认最多选择按路径排序的 2,000 份文本材料；省略计数进入图诊断。每张图最多包含 250,000 个节点和
1,000,000 条关系；共享图的其他解析预算仍生效。捕获完整性与图的解析诊断分别展示，不能将捕获成功解释为关系完整。

图中 Document、Directory 仍保留 NodeKind 和既有关系，物料角色通过 Tags 表达。

页面最多绘制 250 个节点和 800 条边，搜索结果最多显示 100 条，诊断列表最多显示 200 条；
达到显示上限会提示继续缩小范围。展示限制不删除后端图事实。源码每次显示最多 400 行。

页面只读内存中的捕获内容，不按请求路径重新访问磁盘。点击 Refresh snapshot 才重新捕获和构图，
成功后原子替换上下文、图与源码的整个版本；失败仍可浏览旧图。来源请求携带 snapshot ID，版本过期返回冲突，
防止另一个页面刷新后将旧位置配上新源码。构图仍是 best effort，捕获期间的变化单独报告。

## 交付与验证

HTML、CSS、JavaScript、固定版本 Cytoscape.js 及其 MIT 许可证随 Go 二进制内嵌，
运行时无需 Node.js、外部 CDN 或数据库。第三方版本和完整性摘要在
`internal/viewer/static/vendor/README.md`。

viewer 的标准检查入口为 `make -C apps/cli fix lint test build`。viewer 回归测试覆盖完整关系投影、快照源码一致性、
刷新失败和并发、捕获忽略规则、展示资源、loopback 边界及取消退出。修改前端后额外运行
`node --check internal/viewer/static/app.js`，并用实际浏览器验证搜索、图绘制、过滤、源码与刷新。

组件上下文位于图 API 的顶层 `repository`、`components`；嵌套的 `snapshot` 使用内容报告 schema 2，
不再承载结构元数据。两部分从同一捕获材料产生，刷新时与图、源码一同替换。
