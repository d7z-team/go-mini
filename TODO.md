# TODO: Runtime 全面脱 AST（编译/执行彻底分离）

更新时间: 2026-03-31
范围: `core/runtime` 主执行路径（非编译器前端）

## 目标

- 编译阶段: AST -> Lowering -> Task/Bytecode（数据化）
- 执行阶段: 仅消费数据任务/字节码，不依赖高级 AST 节点
- `eval`/共享状态/FFI/panic-unwind 在执行器内部统一处理

## 全量任务清单

### A. 架构分层与任务模型

- [x] 建立 `Task` 数据负载模型（`BranchData`/`ForData`/`RangeData`/`SwitchData`/`CallData`/`LHSData` 等）。
- [x] 建立 lowering 入口 `tasksForStmt/tasksForExpr/tasksForLHS`。
- [x] 增加 `OpMakeClosure`，闭包创建走 payload。
- [x] 增加 `DoCallData`，函数调用入口不再依赖 `*ast.FunctionStmt`。
- [ ] 删除 `Task.Node ast.Node`（改成仅调试元信息，例如 `SourceRef`）。

### B. Lowering 覆盖

- [x] `if/for/range/try/defer/switch/type-switch` lower 到数据任务。
- [x] `call/import/composite/index/slice/assert` lower 到数据任务。
- [x] `func literal` lower 到 `OpMakeClosure` + `ClosureData`。
- [x] 支持 type-switch `Assign` 的 block 形态（兼容 converter 输出）。
- [ ] 对所有 AST 节点建立“要么 lower，要么显式拒绝”的覆盖矩阵文档。

### C. 执行器主路径脱 AST

- [x] `InitializeSession` 主入口改为压入 lowered tasks（非 debugger 模式）。
- [x] `ExecExpr` 改为优先走 lowered expr tasks（非 debugger 模式）。
- [x] `ExecuteStmts` 改为优先走 lowered stmt tasks（非 debugger 模式）。
- [x] `ImportModule` 改为 payload `ImportInitData`。
- [x] `invokeCall/setupFuncCall` 改为 `FunctionType + BodyTasks`。
- [x] `VMClosure` 去掉 `FuncDef` 字段，仅保留运行时所需数据。
- [ ] 去除 `dispatch` 中所有 `task.Node.(*ast.X)` fallback 分支。
- [ ] 去除 `OpJumpIf/OpEvalLHS/OpIndex/OpComposite/OpAssert` 的 AST 回退读取。
- [ ] 去除 `OpForCond/OpSwitch*` 的 AST 回退读取（全部只读 `ForData/SwitchData`）。
- [ ] 彻底停用 `OpExec/OpEval`（或仅在调试适配层内部可见，不进入主运行队列）。

### D. 调试与可观测性适配

- [x] 当前通过 `session.Debugger != nil` 保留 AST 执行路径，确保现有调试测试通过。
- [ ] 设计“无 AST 的调试协议”: 断点/单步改基于 lowered task 的 source 映射。
- [ ] `Disassemble` 改为只依赖 lowered tasks/bytecode，不调用 `handleExec/handleEval` 展开 AST。
- [ ] 新增断言: 非 debugger 模式执行期间不得出现 `OpExec/OpEval`。

### E. 测试与清理

- [x] 新增/更新 lowering 单测（for/range/defer/try/switch/type-switch/closure 等）。
- [x] 修复因强制 lowering 导致的回归（`lhs=nil`、type-switch assign 形态）。
- [x] 全量测试 `go test ./...` 通过。
- [ ] 清理过时测试: 删除只验证 AST fallback 语义、与新执行模型冲突的用例。
- [ ] 增加“主路径脱 AST”专项测试集:
  - [ ] 运行时任务栈无 AST 节点
  - [ ] 关键 opcode 全 payload 化
  - [ ] 导入模块/闭包/type-switch 在无 AST 模式一致

## 当前剩余任务（下一轮必须完成）

1. 完成 `dispatch` AST fallback 清零（`OpJumpIf/OpEvalLHS/OpIndex/OpComposite/OpAssert/OpFor*/OpSwitch*`）。
2. 将 `OpExec/OpEval` 从主路径彻底移除，仅保留可替代的调试适配层。
3. 重写 `Disassemble` 展开逻辑，禁止依赖 `handleExec/handleEval`。
4. 为“无 AST 主路径”补强测试并清理旧测试。
5. 删除 `Task.Node`，改 source 元数据结构，完成最终解耦。

## 验收标准（Done Definition）

- 非 debugger 模式下:
  - 任务栈中不出现 `OpExec/OpEval`。
  - `dispatch` 不包含 `task.Node.(*ast.*)` 读取分支。
  - `Task` 结构不持有 AST 节点引用。
- debugger 模式下:
  - 断点、单步、暂停功能等价保留。
- 质量:
  - `go test ./...` 持续通过。
  - 新增专项测试覆盖主路径“完全脱 AST”约束。

