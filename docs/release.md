# 构建与发布

仓库按可复用工具包和应用划分发布单元。Go toolkit、TypeScript toolkit 和 CLI 各自拥有依赖清单；
根 Makefile 汇总检查，不承担包依赖。go.work 只连接本仓源码，不进入外部调用方的依赖解析。

## 开发与验证

在 `toolkit/typescript` 安装 npm 依赖后，根 `make lint`、`make test`、`make check` 和 `make build`
覆盖全部 Component。也可进入单个 Component 使用其 Makefile。`make install` 只构建并安装 CLI；
`make -C apps/cli build` 生成根 `bin/repocli`。共享语料保存在根 conformance，各实现从同一份语料验证契约。

Go 工具包可用 `GOWORK=off go build ./...` 单独构建；应用通过 go.mod 固定工具包版本。
发布前的联合开发由 go.work 将该版本指向本地源码，不在应用 go.mod 中保留目录 replace。
CLI 版本通过构建参数注入；`go install ...@version` 则读取 Go 模块构建信息，不依赖运行时文件。

## 版本与发布顺序

- 根 `VERSION` 定义仓库和 CLI 版本，仓库 Release/tag 为 `v<VERSION>`。
- Go 工具包 module 为 `github.com/compforge/repocli/toolkit/go`，对应 tag `toolkit/go/v<VERSION>`。
- CLI module 为 `github.com/compforge/repocli/apps/cli`，对应 tag `apps/cli/v<VERSION>`。
  go.mod 声明的工具包版本与本次仓库版本保持一致，go.work 同步绑定该版本到本地源码。
- `@compforge/repocli` npm 包独立使用 `toolkit/typescript/package.json` 的版本。
  它依赖的 harness-common 版本必须先在 npm 可用；npm 发布遵循独立的包发布流程。

发布时先通过受控 Git 发布流程为同一提交创建 Go 工具包及 CLI 子模块 tag，再创建仓库 tag。
仓库 tag 触发 release workflow：检查版本与既有子模块 tag 的目标、验证全部 Component、构建各平台 CLI，
并在关闭 go.work 后独立安装 CLI，最后上传附件。工作流不创建或移动 Go 子模块 tag，也不自动发布 npm 包。
缺少子模块 tag、目标提交不一致或依赖不可用时停止发布。

子模块 tag 可用后，外部用户可通过以下入口安装或引用：

```sh
go get github.com/compforge/repocli/toolkit/go@v0.14.0
go install github.com/compforge/repocli/apps/cli/cmd/repocli@v0.14.0
```

发布时独立验证 CLI 的模块依赖与版本输出，避免仅有 go.work 的本地替换掩盖缺失的发布依赖。
