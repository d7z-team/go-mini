# Go-Mini 项目架构与开发引导（Agent 版）

本文档是本仓库 AI Agent / 开发者的统一入口。

- 编译与执行分层
- 执行主路径脱 AST
- bytecode / prepared program 作为唯一装载工件
- FFI 统一为 schema-only

## 1. 当前架构

### 1.1 分层模型

1. 前端层
- `core/ffigo/converter.go`: Go AST -> Mini AST
- `core/ast/*`: 语义检查、类型系统、LSP 查询

2. 编译层
- `core/runtime/task_lowering.go`: AST -> lowered task plan
- `core/compiler/*`: 构建 `go-mini-bytecode`、`PreparedProgram`
- `core/bytecode/*`: 稳定 bytecode 工件格式、重建 blueprint

3. 执行层
- `core/runtime/executor.go`: 只执行 prepared task plan
- `core/runtime/executor_eval.go`: 调用、内置函数、FFI 路由
- `core/runtime/task.go`: opcode、payload、`SourceRef`
- `core/runtime/scope.go`: 栈、slot、闭包、upvalue

4. 工具层
- `cmd/exec`: bytecode-first CLI，负责编译、反汇编、执行
- `cmd/ffigen`: FFI 生成器

### 1.2 架构状态

- 非调试执行主路径脱离 AST
  - runtime 默认装载 `PreparedProgram` / bytecode executable
  - `cmd/exec` 使用 bytecode-first CLI
  - JSON 默认只接受 `go-mini-bytecode`
- 调试器保留前端/LSP 所需的 AST 蓝图，但不依赖 `Task.Node`
- `ffigen` 使用精简后的生成模型
  - 只保留 `-pkg` / `-out`
  - `ffigen:module` 是 VM 命名唯一来源
  - VM 中不暴露完整 Go import path

## 2. 关键约束

### 2.1 执行主路径禁止回退 AST

- 非 debugger 模式下，不得新增基于 AST 节点的运行时主逻辑
- 新能力必须先落在 lowering / bytecode / payload，再由 runtime 消费
- 禁止引入新的 `task.Node.(*ast.X)` 常驻分支

### 2.2 bytecode 是唯一装载工件

- 对外 JSON / 持久化 / CLI 装载都应以 `go-mini-bytecode` 为准
- 新功能若涉及执行入口，应优先接入 `Compile*` / `NewRuntimeByCompiled` / `NewRuntimeByBytecodeJSON`
- 不要扩展 AST-only 装载口

### 2.3 FFI 只走 schema-only

- 不要引入旧 spec/registrar 双轨
- `ffigen` 新参数模型只有 `-pkg` 和 `-out`
- VM 可见类型名必须保持短路径 / 模块路径语义，不要把完整 Go import path 写回 schema 文本

### 2.4 闭包与调用约束

- 闭包运行时结构只保留执行必要信息：
  - `FunctionType`
  - `BodyTasks`
  - `UpvalueSlots`
  - `Context`
- 不要重新引入 AST 函数字段作为执行依赖

## 3. 标准落地顺序

新增语法 / 新节点按这个顺序落地：

1. `core/ast`
2. `core/ffigo/converter.go`
3. `core/runtime/task_lowering.go`
4. `core/compiler/*` / `core/bytecode/*`
5. `core/runtime/executor.go`
6. 测试：`core/runtime` + 对应 `core/e2e` / 模块测试

## 4. 开发流程

### 4.1 改动前

- 先读 `TODO.md`
- 用 `rg` 检索已有 payload、schema、bytecode 结构
- 确认改动属于前端、编译、执行还是工具层

### 4.2 改动中

- 优先改 lowering / compiler，再改 runtime
- 涉及 CLI 或序列化时，默认接入 bytecode-first 主链
- 涉及 FFI 时，先确认是否属于 `ffigen`、runtime schema 注册或标准库模块测试
- 每完成一块能力立刻补测试

### 4.3 改动后验证

至少执行：

```bash
GOCACHE=/tmp/go-build-cache go test ./core/runtime
GOCACHE=/tmp/go-build-cache go test ./core/e2e/...
GOCACHE=/tmp/go-build-cache go test ./...
```

涉及 CLI / 生成器时附加：

```bash
GOCACHE=/tmp/go-build-cache go test ./cmd/exec ./cmd/ffigen/...
GOCACHE=/tmp/go-build-cache go generate ./...
GOCACHE=/tmp/go-build-cache make test
```

## 5. 提交前检查

- [ ] 非 debugger 主路径未重新引入 AST 依赖
- [ ] 新能力已在 lowering / compiler / runtime 闭环
- [ ] 相关 runtime / e2e / 模块测试已补齐
- [ ] `go test ./...` 与 `make test` 通过
- [ ] `TODO.md` 状态已同步

## 6. 文档协作规则

- 架构与任务状态以 `TODO.md` 为准
- 本文档只描述当前约束和推荐流程
- 若架构方向变化，需要同步更新 `AGENTS.md`、`README.md`、`DOCS.md`、`LSP.md`
