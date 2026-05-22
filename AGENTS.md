# Go-Mini Agent 约束

本文档只保留本仓库开发时必须遵守的约束；架构状态与任务进度以 `TODO.md` 为准。

## 核心约束

- 非调试执行主路径不得重新引入 AST 节点依赖。
- runtime 包及其依赖图不得引入 `core/ast`；Go source 到 Mini AST 的转换只允许在 `core/gofrontend`，AST 到执行计划的转换只允许在 `core/lowering`。
- 新能力必须先落到 lowering / compiler / bytecode payload，再由 runtime 消费。
- 对外 JSON / 持久化 / CLI 装载保持 bytecode-first，`go-mini-bytecode` / `PreparedProgram` 是唯一执行装载工件。
- 不要扩展 AST-only 执行装载入口。
- 调用模板只允许在 compiler 首次语义检查后、优化前展开为真实 AST；runtime、bytecode、FFI bridge 不得保留模板执行逻辑或模板节点。
- FFI 只走 schema-only，不引入旧 spec/registrar 双轨。
- `core/ffigo` 只承载 FFI wire / bridge / helper 类型，不得 import `core/ast` 或 Go parser/AST 包。
- `core` 不得 import 或调用顶层 `ffilib`；`core/ffilib` 只承载纯原生类型标准库 FFI 子集，并由 `engine.NewMiniExecutor()` 默认注册；完整标准库 FFI 只能由顶层 `ffilib.RegisterAll` 装配。
- 非 `core/ffilib` 或顶层 `ffilib` 测试不得依赖标准库 FFI；`core/e2e` 只保留核心语言、runtime、module、FFI 机制测试。
- `ffigen` 只保留 `-pkg` / `-out` 参数模型。
- VM 可见类型名保持短路径 / 模块路径语义，不把完整 Go import path 写回 schema 文本。
- canonical type 文本格式只允许由 `core/typespec` 实现；前端使用 `core/ast/ast_types.go` 门面，VM/runtime 使用 `runtime.TypeSpec` / schema 门面。
- 除上述类型格式化层外，不得手动拼接 `Array<T>`、`Map<K, V>`、`Ptr<T>`、`HostRef<T>`、`tuple(...)`、`function(...)`、`interface{...}`、`struct {...}`。
- 闭包运行时结构只保留执行必要信息，不重新引入 AST 函数字段作为执行依赖。
- VM 内部始终按单线程协作式 VM 执行上下文调度，不新增 host goroutine 执行 VM task。
- 新增并发能力必须证明不会破坏单线程 VM 调度器语义。
- Mini AST / lowering / compiler / runtime 只允许 canonical type。
- Go 风格类型只允许存在于 Go 前端输入层，必须在 `core/gofrontend` 中立即规范化。
- 手写 AST / JSON AST 若出现非 canonical type，必须直接编译错误，不做兼容修复。

## 多模块规则

- 仓库采用 `core` / `ffilib` / `examples` 多模块布局，root 只保留 `go.work`、文档和仓库级脚本。
- `core` module path 为 `gopkg.d7z.net/go-mini/core`。
- `ffilib` module path 为 `gopkg.d7z.net/go-mini/ffilib`。
- `examples` module path 为 `gopkg.d7z.net/go-mini/examples`。
- `ffilib` 可以依赖 `core`，但 `core` 不得依赖 `ffilib`。
- 开发使用 `go.work` 解析本地模块；各模块 `go.mod` 中的依赖版本表示发布兼容基线，不通过本地 `replace` 固化仓库内模块关系。
- `ffilib` 中声明的 `core` 版本只表示最低兼容版本，不要求与 `ffilib` 版本一致。
- 多模块结构调整时，各模块改动应拆成独立提交；不要把 core、ffilib、examples 和文档全部混进一个提交。
- 不手写 `go.sum`，只通过 `go mod tidy`、`go test`、`go generate` 自然维护。
- 正确性以本地 `go generate` / `go test` 通过和边界扫描为准。

## 协作规则

- 改动前先读 `TODO.md`。
- 涉及执行链路的能力，优先修改 lowering / compiler / bytecode，再修改 runtime。
- 涉及 CLI、序列化或持久化时，默认接入 bytecode-first 主链。
- 涉及 FFI 时，先确认改动属于 `ffigen`、runtime schema 注册还是标准库模块测试。
- 所有 `ffilib` FFI 模块测试（含 `core/ffilib` 与顶层 `ffilib`）统一使用表达式/代码块测试框架，通过 `test.Out*` 与 `test.Done()` 校验执行完成和输出，并覆盖对应 schema 方法。
- 每完成一块能力立刻补测试。
- 架构约束的外部命令检查（如 `go list -deps`、`rg`、`make`）属于提交前人工/代理检查，不要塞进普通包单元测试；单元测试只保留不依赖外部命令的轻量源码或行为检查。

## 提交前检查

- [ ] 非 debugger 主路径未重新引入 AST 依赖
- [ ] 新能力已在 lowering / compiler / runtime 闭环
- [ ] 相关测试已补齐
- [ ] 已先执行 `make lint test examples`
- [ ] `TODO.md` 状态已同步

## 文档同步

- 架构约束或协作方式变化时，同步更新 `AGENTS.md`、`README.md`、`DOCS.md`、`LSP.md`
- `README.md` 必须保持类似常规 GitHub 项目首页的简洁结构，只放项目简介、安装、快速使用、开发入口和文档链接；不要堆积架构细节、内部边界、长期约束或任务状态，这些内容分别放到 `DOCS.md`、`LSP.md`、`TODO.md` 或本文件。
