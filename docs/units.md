# Fragment 与 Unit

## 理念与边界

Unit 把尽量相关的变更放在一起，让调用方能够共同理解和处理一项变化。减少 Unit 数量是控制
重复理解成本的手段，不能以丢失编辑、猜测依赖或无限扩大单个 Unit 为代价。

`diff → Fragment → Unit` 属于 repocli 的变更分析能力：Git 提供前后版本、文件状态、patch 和源码，
repocli 切分和分类 Fragment，再依据关系组织 Unit。CodeGraph 用 Node + Relation 表达代码，
在 Fragment 组装成 Unit 时提供关系与 namespace 证据。

| 对象 | 语义 |
|---|---|
| Change | 一份文件变更，包含前后源码、版本与 patch |
| Fragment | 可独立定位的一段编辑，包含两侧源码元素、范围、类型与解析缺口 |
| Unit | 一组 Fragment 引用，以及组装依据、跨 Unit 关系和大小预算状态 |

Fragment 和 Unit 由 repocli 定义，不携带评审任务、模型消息或执行状态。调用方可以把 Unit 用于
评审或其它变更处理流程，业务上下文和运行结果由调用方持有。

## 从 diff 到 Fragment

拆分只读取已捕获的源码和 patch，不要求构建 CodeGraph。增加行在新侧源码定位，删除行在旧侧定位，
重命名保留两侧路径。Fragment 的源码元素使用 repocli 自己的类型，包含 function、method、class、
struct、interface、import 等；无法识别的内容保留为 unknown。

同一归属下的编辑可以合成一个 Fragment。连续替换保留为一个补丁，即使涉及多个声明或两侧类型不同，
也不按同名猜测跨版本身份。`Counts()` 返回前后两侧的元素数量 map，例如 `after["class"]=1`、`after["method"]=2`；不存在的类型为 0。
一个 Fragment 可以同时涉及多类元素，无需选出单一 kind 或 mixed 标签。Unit 使用同一统计模型，
对共享 Fragment 和同侧源码元素去重；`Counts.Only(import)` 可判断整个目标是否只涉及 import。
源码元素范围用于描述归属，实际变更行范围由 patch 决定。

每条编辑恰好属于一个 Fragment。拆分后比较编辑坐标与内容，校验没有遗漏或重复。
Fragment 身份由路径、版本、源码范围和 patch 派生；没有图也能保持身份稳定。

## Change 的路径标签

Diff 的每个 Change 通过 Tags 保存直接路径分类，JSON 字段为 tags。
类型复用 CodeGraph 的开放式 Tag，默认规则来自 BuiltinTagRules。
分类使用 Change.Path()：新增、修改和重命名使用新路径，删除使用旧路径。
同一路径的内容变化不改变标签；二进制或捕获内容缺失不影响分类，解析失败也不会把标签变成缺口。

Diff 通过 CodeGraph 的 TagMatcher 编译并匹配路径规则，不为分类解析源码或构图。调用方可通过 DiffRequest.TagRules 替换规则；
nil 使用内置集合，显式空集合关闭标签，追加自定义规则可使用：

```go
rules := append(codegraph.BuiltinTagRules(), codegraph.TagRule{
    Name: "test", Pattern: `(^|/)tests?(/|$)`,
})
changes, err := repocli.Diff(ctx, repocli.DiffRequest{
    Repository: root, TagRules: rules,
})
```

规则在捕获时固定，多条规则累加、标签去重排序，不继承祖先标签。FormUnits 自行构图时使用同一规则集；
WithGraphs 传入的图则保留调用方自己的标签，分组只消费版本一致的语义关系。
Select 与 FormUnits 保留捕获的 Change 标签，UnitReport 的 changes 同时向 JSON 消费方公开。
CLI 的 `diff --units` 文本在文件行下显示标签。

标签不自动排除变化；调用方可用 Select 指定路径，或通过 UnitOptions.ExcludeChange 提供排除策略：

```go
report, err := repocli.FormUnits(ctx, changes, repocli.UnitOptions{
    ExcludeChange: func(ch repocli.Change) bool {
        return slices.Contains(ch.Tags, codegraph.GeneratedTag)
    },
})
```

回调返回 true 的 Change 在构图和 Fragment 拆分之前移出本次输入，不产生 Fragment 或 Unit；
UnitReport.Changes 只包含保留项。原始 DiffReport 与捕获的源码保持不变，依然可用于上下文查询。
回调为空时保留全部 Change；具体排除哪些标签或路径由调用方决定，repocli 不内置评审排除策略。
一个 Unit 可能含多种性质的文件，
因此标签保留在 Change 上，不对整个 Unit 猜测统一类别。Directory 的 in_directory 关系不参与
语义关联或 namespace 分组，相邻路径本身不能合并独立 module。

## 从 Fragment 到 Unit

关系强度按以下顺序递减：

```text
包含 > caller/callee 等直接依赖 > import > 同文件 > 同 namespace
```

包含关系让类、结构体与发生变更的子成员优先共同处理；调用与引用让协作变更共享上下文。
import 只跟随图证据指向的使用者，不能因为两个函数使用同一个 import 就把两个函数合并。
同文件和同 namespace 是更弱的组织关系，只在数量压力下扩大 Unit。

1. **文件内形成 Unit**：先按包含，再按调用与引用聚合，随后附加相关 import。无法关联的声明保持
   独立；同文件剩余 unknown 合为一个 Unit，未附着的 import 按同类合并。
2. **按跨文件关系合并 Unit**：数量超目标时，继续按包含、调用与引用等强关系合并完整 Unit。
3. **按文件合并 Unit**：仍超目标时，合并有共同文件的 Unit。此前跨文件 Unit 保持完整，孤立 import
   和 unknown 因而可以回到包含同文件其它变更的 Unit。
4. **按 namespace 合并 Unit**：仍超目标时，从叶到根选择共同语义容器，逐步归拢完整 Unit。

文件内形成 Unit 后，每次成功合并都检查数量，达到目标立即停止。数量目标为零表示不设数量目标：
依然组织强关系，但不触发文件和 namespace 的弱关系归拢。文件模式显式选择一文件一组。

共享 import 允许被多个 Unit 引用，例如 `U1={import,A}`、`U2={import,B}`。两者合并后成为
`{import,A,B}`，共享 Fragment 只计算一次。后续阶段只合并完整 Unit，不拆散已经形成的强关系组。
最终报告保留每次合并的关系类别、数量变化和 namespace 证明路径，CLI 可解释每个 Unit 的形成过程。

## 证据、预算与缺口

CodeGraph 决定关系是否有依据，repocli 决定如何消费关系。分组使用 Exact/Scoped 关系；同一级关系
优先考虑触及改动行的证据，再按置信度和稳定身份排序。共同依赖一个未变更目标不构成两个变更
之间的合并依据。图缺失或未能绑定时保留目标，不通过名称相同或文本扫描补造关系。

Namespace 使用 CodeGraph 的共同祖先查询，保留版本身份与原始证明路径。Go package/module、
Python 包以及 JS/TS module 的具体语义由 CodeGraph 持有，目录相邻不自动表示同 namespace。
局部图可能有多个根或没有共同根，查询失败与取消也不等于“没有共同 namespace”。
候选对按 namespace 深度和稳定顺序排队；合并后只重算涉及新 Unit 的候选，证明路径仅在选中时组装。
namespace 阶段的 `budget_blocked` 统计被预算拒绝的候选版本，不重复计算未变化的候选对。

数量是软目标，文件数、变更行数和 diff 大小约束合并。预算不允许、或没有共同 namespace 时，
保留 Unit 并报告 `limit_exceeded`；不伪造一个语言 namespace 来满足数量。不可再拆的 Fragment
或同类残余 Unit 本身超预算时仍保留，并标为 `budget_exceeded`。被预算切断的关系作为边界证据输出。

`complete` 描述捕获、解析和图分析是否报告执行缺口，不承诺静态分析覆盖所有运行时关系。
例如当前图未提供 Go 限定调用到 import 的绑定时，import 会留作文件内兜底目标。

## 使用

```sh
repocli diff --repo . --base main --units --max-units 8
repocli diff --repo . --base main --units --max-units 8 --json
```

Go 调用方先用 `Diff(ctx, request)` 获取变更，再按需调用 `FormUnits(ctx, diff, options)`。
`diff.Select(paths...)` 筛选变更并保留完整捕获源码；空选择返回零变更。FormUnits 不重新读取工作区。
`diff.WithGraphs(before, after)` 可复用相同版本的调用方图；未提供图时按所选变更构图。
已有源码也可使用 `SplitChange`，再将已有图交给 `GroupFragments`。这些方法面向任意调用方，
不持有评审状态、提示词或验证门禁。图与源码必须属于相应的同一版本。
库不执行被分析项目的代码，也不调用 LLM；CLI 负责参数和展示。
