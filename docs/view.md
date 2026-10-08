# 代码图浏览

`repocli view` 为当前 Git 工作区提供本地代码图浏览器。用户从 Document 概览进入符号，
沿调用、导入、引用和类型关系阅读源码，并查看关系依据和解析缺口。

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

## 页面

- Components 展示当前快照的完整组件目录、语言与包工具证据；节点详情显示文件或目录归属。
- 共享图保留 Directory、in_directory 与节点 Tags；目录详情列出捕获到的直接子节点，文件和目录均展示直接标签。目录没有源码文本，也不表示其所有内容都已捕获。
- 左侧列出 Document；输入名称或路径可搜索所有已构建的节点。
- Document 概览将跨文件关系按端点 Document 和关系类别分组；点击边查看原始证据。
- 点击 Document 展开其声明与相邻节点；搜索或双击符号可切换到该符号的一跳邻域。
- 关系类型和 confidence 可筛选。实线表示 exact；含非 exact 关系的组使用虚线；筛选支持 exact、scoped、name_only、heuristic，详情展示每条关系的完整 Evidence。
- 没有单一源码位置的 Package/Module 通过 declares 展示贡献文档，不假造文件位置。
- 详情展示 marker、入边/出边及快照源码。诊断保留类别、位置和语义缺口。

confidence 表达静态证据强度，不是改动影响概率。没有诊断不证明分析完备；没有边也不证明无依赖。

## 快照与边界

捕获复用 snapshot/diff 的 Git 跟踪和忽略规则，并继承其文件数、字节、符号链接及子模块边界。
纳入捕获范围的 UTF-8 文本作为 Document；二进制不参与构图。无 grammar 的文本仍可作为
Document 展示，并保留不支持语言的诊断。

默认最多选择按路径排序的 2,000 份文本材料；省略计数进入图诊断。共享图的节点、关系及
解析预算仍生效。捕获完整性与图的解析诊断分别展示，不能将捕获成功解释为关系完整。

页面最多绘制 250 个节点和 800 条边，搜索结果最多显示 100 条，诊断列表最多显示 200 条；
达到显示上限会提示继续缩小范围。展示限制不删除后端图事实。源码每次显示最多 400 行。

页面只读内存中的捕获内容，不按请求路径重新访问磁盘。点击 Refresh snapshot 才重新捕获和构图，
成功后原子替换上下文、图与源码的整个版本；失败仍可浏览旧图。来源请求携带 snapshot ID，版本过期返回冲突，
防止另一个页面刷新后将旧位置配上新源码。构图仍是 best effort，捕获期间的变化单独报告。

## 交付与验证

HTML、CSS、JavaScript、固定版本 Cytoscape.js 及其 MIT 许可证随 Go 二进制内嵌，
运行时无需 Node.js、外部 CDN 或数据库。第三方版本和完整性摘要在
`internal/viewer/static/vendor/README.md`。

项目标准检查仍为 `make fix lint test build`。viewer 回归测试覆盖完整关系投影、快照源码一致性、
刷新失败和并发、捕获忽略规则、展示资源、loopback 边界及取消退出。修改前端后额外运行
`node --check internal/viewer/static/app.js`，并用实际浏览器验证搜索、图绘制、过滤、源码与刷新。

组件上下文位于图 API 的顶层 `repository`、`components`；嵌套的 `snapshot` 使用内容报告 schema 2，
不再承载结构元数据。两部分从同一捕获材料产生，刷新时与图、源码一同替换。
