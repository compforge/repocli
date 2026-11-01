# Makefile 验证

repocli 按 Component 规划和执行项目声明的 lint/test。前提是项目遵守下面的 Makefile 契约；
工具包不根据语言猜测试命令，也不通过 `make -n` 探测能力，因为 Make 在读取文件时就可能执行函数。
Go 复用已有 gotreesitter 的 Make grammar 静态读取规则和变量引用，不新增解析依赖；Python 保持
该约定子集的原生识别，双方共用契约语料。语法解析不等于求值，条件规则和动态目标不推断为可用入口。
分析与规划保持只读，执行是调用方明确发起的操作。

## 项目契约

每个需要验证的 Component 在其根目录提供普通文件 `Makefile`（不接受符号链接），直接声明目标：

- lint：优先 `lint-ci`，其次 `lint`。
- test：优先 `test`，其次 `test-ci`、`test-local`。
- 目标返回 0 表示检查通过，非 0 表示检查失败；目标不修改参与验证的源码。
- 可选的 `LINT_FILES` / `TEST_FILES` 为 Component 相对路径的空格分隔列表。
  非空时检查这些文件，空值必须执行该目标的完整检查。
- 只有当前 Makefile 显式引用 `$(LINT_FILES)` / `${LINT_FILES}` 或相应 TEST_FILES 变量，
  才视为声明文件过滤支持。主 Makefile 需直接声明公开目标；不展开 include、变量目标或动态生成的规则。

例如 Python Component 的测试入口：

```make
.PHONY: test
# 空值时 pytest 发现全量测试；非空时按给出的测试文件运行。
test:
	uv run pytest $(TEST_FILES)
```

项目负责实现这些语义。repocli 不解析测试框架输出，也不能从一个返回 0 的空 recipe 证明实际检查发生。
依赖安装、代码生成和 `make fix` 应在规划前完成；Python 调用方可使用已有的
`inspect_dependencies` / `prepare_dependencies`。验证不隐式安装依赖或运行 formatter。

## 模型与流程

`ValidationPlan` 按 Component 分组，每组包含若干 `CheckSpec`。每项固定检查种类、命令、文件、
范围、选择原因和执行环境摘要；整个计划绑定工作区内容快照。Go 计划可输出 JSON 供审阅，
但序列化输出不包含环境值，也不能反序列化后直接执行。计划只在创建它的进程中执行。

`CheckResult` 记录具体命令的状态、退出码、耗时、stdout、stderr 和诊断。
`ValidationReport` 汇总各 Component 的结果、整轮完整性及内容一致性：

- `complete`：选中的检查均结束，且验证输入保持一致；正常的失败检查也可以是 complete。
- `passed`：本轮 complete，所有实际执行的检查都通过；skipped 不当作执行过。
- 缺目标、无法启动、取消或超时会使本轮 incomplete；检查失败保留 failed。
- 检查返回 0，但源码在过程中变化时，单项命令仍是 passed，整轮报告为 incomplete/passed=false。
  调用方不能把该轮的单项成功保存为可复用通过证据。
- 没有选中任何检查时报告空结果；它不证明整个仓库已经验证。

Go 提供 `PlanValidation`、`ExecuteValidation`、`Validate`、`Lint`、`Test`。
Python 原生 API 为 `plan_validation`、`execute_validation`、`validate`、`lint`、`test`，
不包装 repocli CLI。CLI 组合 Go 公共 API；lint/test 只选择单类检查，validate 选择两类。
TypeScript 当前不提供验证执行 API。

## 选择检查范围

未提供 impact 时，对选中的 Component 做全量检查；未指定 Component 时选择全部已发现 Component。
显式空 Component 列表表示不选任何 Component，未知 Component 是参数错误。

调用方可提供当前工作区的 `ImpactReport`。规划器核对 checkout、input 和 snapshot，拒绝把旧报告、
index 或 commit 报告套在当前工作区。默认在 affectedFileCounts 中出现 source/test/config/manifest/lockfile
时选择该 Component；调用方可以提供自己的 tags。lint 使用该 Component 的 affectedFiles，test 使用 testFiles。
分析缺口不单独触发检查，原始 ImpactReport 继续保留 before/unowned 等分析证据。

删除、重命名前侧的路径仍按真实归属检查：候选已经不存在或当前属于别的 Component 时，当前检查
回退全量并记录原因。没有返回候选文件、项目不支持文件过滤、路径无法安全传给 Make 时，也回退全量。
不为已不存在的 Component 或 unowned 路径伪造执行目标。

手工 `files` 与 impact 是两种互斥的选择来源。文件必须是仓库相对路径；进入 Make 前转换为
Component 相对路径并去重排序。包含空格、shell/Make 元字符或穿过符号链接的路径不进入文件参数。
这类候选会导致全量回退，不会被静默丢掉。

输入摘要不决定检查范围。目前验证执行绑定完整仓库快照；已有 scoped snapshot API 仍可用于调用方
的复用策略。执行 API 本身不读取旧 stamp 或缓存，不拥有缓存目录、Session、branch 或提交门禁。

## 执行与预算

执行器只消费冻结计划，不重新查询目标、分析 affected 或推导 scope。开始和结束各捕获一次工作区，
扫描次数不随 Component/check 数量增加。规划之后内容已变时不启动检查；执行途中变化则整轮失效。

环境在规划时复制，执行时显式传给子进程；显式环境需提供用于查找 make 的 PATH。报告只保留其摘要，绝不输出环境值。
每条命令用 `make -f Makefile` 固定读取规划时的文件，避免 GNUmakefile/makefile 抢占默认入口。
Make 控制变量 MAKEFILES/MAKEFLAGS/MFLAGS/MAKEOVERRIDES/GNUMAKEFLAGS/MAKELEVEL 与两类文件变量不从父环境继承；
每条命令显式设置 LINT_FILES 和 TEST_FILES，防止继承的 dry-run 或文件选择改变计划语义。
环境摘要描述实际传入的环境，不要求 Python/Node 宿主进程的环境相等。

检查当前顺序执行。Go 调用方通过 context 传入整轮 deadline；Python 使用 timeout 和已有 operation
取消作用域。CLI 沿用全局 --timeout。macOS/Linux 上每次 Make 都运行在独立会话中，取消、超时、
输出超限或退出时清理其进程组。启动后中断标为 uncertain，未启动时取消标为 cancelled；不自动重试。
stdout 上限 16 MiB，stderr 上限 64 KiB。输出超过预算会终止检查并报告 uncertain。

## CLI

```sh
repocli validate --component toolkit/go --timeout 10m
repocli lint --component toolkit/python --file toolkit/python/repocli/model.py
repocli test --component toolkit/python --file toolkit/python/tests/test_inspect.py
repocli validate --base HEAD --plan
repocli validate --base origin/main --json --timeout 10m
```

`--plan` 只输出 JSON 计划，不执行 Makefile。`--base` 显式请求先分析 working-tree impact；
不传该参数时，不隐式分析 diff。`--file` 可重复，不能与 --base 混用。
JSON 报告与目标 stdout/stderr 分离，目标输出作为单项结果字段保存。
退出码为 0（完整通过）、1（检查失败或验证不完整）、2（CLI 参数错误）。

Go/Python 共用 `conformance/validation/cases.json`，并分别通过真实 Make 进程验证内容变动、
环境冻结、失败、超时和子进程清理。CLI 测试通过公共 API 验证入口组合与退出码。
