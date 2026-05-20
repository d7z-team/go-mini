# Go-Mini Agent 约束

本文档只保留本仓库开发时必须遵守的约束；架构状态与任务进度以 `TODO.md` 为准。

## 核心约束

- 非调试执行主路径不得重新引入 AST 节点依赖。
- 新能力必须先落到 lowering / compiler / bytecode payload，再由 runtime 消费。
- 对外 JSON / 持久化 / CLI 装载保持 bytecode-first，`go-mini-bytecode` / `PreparedProgram` 是唯一执行装载工件。
- 不要扩展 AST-only 执行装载入口。
- FFI 只走 schema-only，不引入旧 spec/registrar 双轨。
- `ffigen` 只保留 `-pkg` / `-out` 参数模型。
- VM 可见类型名保持短路径 / 模块路径语义，不把完整 Go import path 写回 schema 文本。
- 闭包运行时结构只保留执行必要信息，不重新引入 AST 函数字段作为执行依赖。
- VM 内部始终按单线程协作式 VM 执行上下文调度，不新增 host goroutine 执行 VM task。
- 新增并发能力必须证明不会破坏单线程 VM 调度器语义。
- Mini AST / lowering / compiler / runtime 只允许 canonical type。
- Go 风格类型只允许存在于 Go 前端输入层，必须在 `core/ffigo/converter.go` 中立即规范化。
- 手写 AST / JSON AST 若出现非 canonical type，必须直接编译错误，不做兼容修复。

## 协作规则

- 改动前先读 `TODO.md`。
- 涉及执行链路的能力，优先修改 lowering / compiler / bytecode，再修改 runtime。
- 涉及 CLI、序列化或持久化时，默认接入 bytecode-first 主链。
- 涉及 FFI 时，先确认改动属于 `ffigen`、runtime schema 注册还是标准库模块测试。
- 每完成一块能力立刻补测试。

## 提交前检查

- [ ] 非 debugger 主路径未重新引入 AST 依赖
- [ ] 新能力已在 lowering / compiler / runtime 闭环
- [ ] 相关测试已补齐
- [ ] 已先执行 `make lint test`
- [ ] `TODO.md` 状态已同步

## 文档同步

- 架构约束或协作方式变化时，同步更新 `AGENTS.md`、`README.md`、`DOCS.md`、`LSP.md`
