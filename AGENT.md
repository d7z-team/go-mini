# Go-Mini 项目架构与开发引导（Agent 版）

本文档是本仓库 AI Agent / 开发者的统一入口，目标是确保改动始终符合当前架构方向：**编译与执行分层，执行主路径脱 AST，data/bytecode 驱动**。

## 1. 当前架构总览

### 1.1 分层模型

1. 前端层（语法与语义）
- `core/ffigo/converter.go`: Go AST -> Mini AST
- `core/ast/*`: AST 节点、类型系统、语义检查、查询能力

2. 编译层（Lowering / Bytecode）
- `core/runtime/task_lowering.go`: AST -> Task（数据任务）lowering
- `core/compiler/*`: 编译为 bytecode（用于反汇编、后续执行路径统一）
- 目标：编译阶段完成内联、结构化展开、执行期不依赖高级 AST

3. 执行层（Runtime VM）
- `core/runtime/executor.go`: 调度循环与 opcode 执行
- `core/runtime/executor_eval.go`: 调用、内置函数、FFI 路由
- `core/runtime/task.go`: opcode、payload 以及 `SourceRef` 数据结构
- `core/runtime/scope.go`: 栈、作用域、Var、闭包数据结构

### 1.2 运行时现状（2026-03-31）

- 非调试模式（主路径）已实现完全脱 AST：
  - `InitializeSession/ExecExpr/ExecuteStmts/ImportModule` 仅消费 lowered tasks。
  - `dispatch` 调度器不再依赖 `task.Node`（非调试模式下强制要求 `Data` payload）。
  - `Disassemble` 已改为基于 `SourceRef` 和 `Data` 的纯数据化展开。
  - 任务项包含 `SourceRef` 用于记录行号、ID 等元信息。

- 调试模式：
  - 已移除 `Task.Node` 字段，调试完全基于 `SourceRef` 和 `OpLineStep` 驱动。
  - 断点、单步、暂停功能均已适配数据化执行路径。

## 2. 目录速查

```text
go-mini/
├── cmd/
│   └── ffigen/                  # FFI 包装生成器
├── core/
│   ├── ast/                     # AST、语义、查询
│   ├── compiler/                # Bytecode 编译
│   ├── debugger/                # 调试会话模型
│   ├── e2e/                     # 端到端测试
│   ├── ffigo/                   # Go->Mini AST 转换与桥接协议
│   ├── ffilib/                  # 标准库 FFI
│   └── runtime/
│       ├── task.go              # OpCode + payload 定义
│       ├── task_lowering.go     # AST -> Task lowering
│       ├── executor.go          # VM 调度与执行
│       ├── executor_eval.go     # 调用与内置函数
│       └── scope.go             # Stack/Var/Closure
├── TODO.md                      # 当前重构总任务（唯一清单）
└── AGENT.md                     # 本文档
```

## 3. 关键设计约束（必须遵守）

### 3.1 执行主路径禁止 AST 依赖

- 非 debugger 模式下，新增逻辑不得把 AST 节点塞进运行时任务栈作为主依赖。
- 新增语义必须先在 lowering 阶段落成 payload，再由 `dispatch` 消费 payload。
- 禁止新增“为了兼容先用 `task.Node.(*ast.X)`”的常驻逻辑。

### 3.2 新语法/新节点的标准落地顺序

1. `core/ast`：节点定义与 `Check/Optimize`。
2. `core/ffigo/converter.go`：Go AST 到 Mini AST 映射。
3. `core/runtime/task_lowering.go`：lower 为数据任务。
4. `core/runtime/executor.go`：只加 payload 执行逻辑。
5. 测试：`core/runtime/*_test.go` + `core/e2e/*_test.go`。

### 3.3 闭包与调用约束

- 闭包运行时结构只保留执行必要信息：
  - `FunctionType`
  - `BodyTasks`
  - `Upvalues`
  - `Context`
- 不要重新引入 AST 函数字段作为执行依赖。

### 3.4 FFI 与隔离约束

- 保持 VM 与宿主内存隔离：脚本端仅持有可控值/句柄。
- 执行路径禁止反射依赖。
- FFI 参数和返回值保持已定义的 VM 原语约束。

## 4. 开发流程（建议）

### 4.1 改动前

- 先读 `TODO.md`，确认该任务属于哪一阶段。
- 用 `rg` 检索是否已有 payload 结构可复用，避免重复建模。

### 4.2 改动中

- 优先改 lowering，再改 dispatch。
- 改动涉及调试器时，显式区分：
  - 主路径（非 debugger）
  - 兼容路径（debugger）
- 每完成一块能力，立刻补测试。

### 4.3 改动后验证

至少执行：

```bash
GOCACHE=/tmp/go-build-cache go test ./core/runtime
GOCACHE=/tmp/go-build-cache go test ./core/e2e
GOCACHE=/tmp/go-build-cache go test ./...
```

## 5. 提交前检查清单

- [ ] 非 debugger 主路径未引入新的 AST 任务依赖。
- [ ] 新能力已在 lowering 和 dispatch 两端闭环。
- [ ] 相关 runtime 单测和 e2e 测试已补齐或更新。
- [ ] `go test ./...` 通过。
- [ ] `TODO.md` 状态已同步（完成项/剩余项）。

## 6. 文档协作规则

- 架构与任务状态以 `TODO.md` 为准。
- 本文档聚焦“怎么做（约束与流程）”，不记录临时实验结论。
- 若架构方向变更（例如 debugger 完全脱 AST），需要同步更新本文件与 `TODO.md`。

## 其他说明

所有的对话均使用 **简体中文** ，专业术语使用  **简体中文（英语缩写/全称）** 