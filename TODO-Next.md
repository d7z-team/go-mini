# Go-Mini 执行器非递归重构计划 (Iterative Stack-Machine)

本项目旨在将 `go-mini` 核心的 AST 递归执行器重构为基于显式状态机的非递归（迭代式）执行器，以彻底消除深层递归导致的宿主栈溢出风险，并为未来的协程支持奠定基础。

考虑到任务极高（9/10）的复杂度和破坏语义的风险，重构将采用**渐进式并行开发**策略。我们将保留现有的 `executor.go`，在一个新的文件中构建迭代执行器，并在通过所有 E2E 测试后进行最终切换。

---

## 阶段零：安全规范与解卷状态机 (Phase 0: Specification)
**目标**：确立迭代器的底层铁律，防止语义漂移。

- [ ] **定义指令流原子性**: 明确只有 `OpExec` 任务可作为 Debugger 的断点触发点，其他 `OpEval` 等中间任务对用户透明。
- [ ] **解卷状态机 (Unwind State Machine)**: 
  - 引入 `UnwindMode`：当发生控制流跳转时，进入 `UnwindReturn`, `UnwindPanic`, `UnwindBreak`, 或 `UnwindContinue` 模式。
  - **拦截矩阵 (Intercept Matrix)**：在解卷（不断 Pop 任务）期间：
    1. `OpScopeExit` 和 `OpFinally`: 在**任何** Unwind 模式下都必须被触发（保证 `defer` 和 `finally` 绝对执行）。如果 `finally` 内没有新的中断，执行完毕后**恢复原有的 Unwind 模式**继续解卷。
    2. `OpCatchBoundary`: 仅在 `UnwindPanic` 模式下拦截，拦截后恢复正常执行，并将 Panic 对象注入。
    3. `OpLoopBoundary`: 仅在 `UnwindBreak` 和 `UnwindContinue` 模式下拦截。
    4. `OpCallBoundary`: 仅在 `UnwindReturn` 模式下拦截，将返回值写回调用者，恢复正常执行。(`UnwindPanic` 会直接穿透它！)。
  - **异步 Defer 保证**: 考虑到 `defer` 内可能调用脚本函数（必须使用任务队列执行），解卷过程必须是**可挂起**的。遇到 `defer` 时，解卷器需将函数调用压入 `TaskStack` 并暂停解卷，待调用完成后再继续解卷。
- [ ] **ValueStack 多值与解包协议**: 
  - 定义函数返回多个值（Tuple/Result）时在 `ValueStack` 中的排列顺序（通常是按返回顺序压栈，逆序弹出）。
  - **Tuple 展开规则**: `OpMultiAssign` 必须明确能够处理栈顶的单一 `TypeArray/TypeResult`，将其隐式展开为 N 个独立元素，以便于后续出栈赋值。
  - **Cell 自动解包规则**: 所有加载变量的指令（如 `OpLoadVar`）必须自动解包 `TypeCell`，确保压入 `ValueStack` 的是纯净的求值结果，防止破坏后续算术指令。
- [ ] **栈平衡检查**: 在每个顶级 `OpExec` 任务开始前，强制检查 `ValueStack` 是否为空，防止上一个表达式求值残留导致数据污染。

## 阶段一：基础设施与核心调度 (Foundation & Dispatcher)
**目标**：建立非递归执行器的心智模型和核心数据结构。

- [ ] **创建核心文件**: 创建 `core/runtime/task.go` 和 `core/runtime/executor_iterative.go`。
- [ ] **定义操作码 (OpCode)**: 设计状态机的核心指令集（如 `OpExec`, `OpEval`, `OpEvalLHS`, `OpApplyBinary`, `OpPop`, `OpScopeEnter`, `OpScopeExit`, `OpAssign`, `OpIncDec`, `OpRunDefers`, `OpFinally` 等）。
- [ ] **定义任务与栈结构**:
  - `Task`: 包含 OpCode, AST 节点引用以及可能需要的辅助数据（如迭代状态、环境指针）。
  - `ValueStack`: 一个用于存放表达式求值中间结果（`*Var`）的显式栈。
- [ ] **扩展 StackContext**: 在上下文中集成 `TaskStack []Task`、`ValueStack` 以及 `UnwindMode` 状态。
- [ ] **实现主调度循环**: 编写 `IterativeExecutor.Run()` 循环，实现基础的任务弹出、指令限制检查、Context 超时检查和 Debugger 挂载点。同时加入 FFI 反向调用（脚本->宿主->脚本）的重入深度保护。

## 阶段二：基础表达式求值 (Basic Expressions Evaluation)
**目标**：将所有无副作用的、不改变控制流的表达式非递归化。

- [ ] **字面量与标识符**: 实现对 `LiteralExpr`, `IdentifierExpr`, `ConstRefExpr` 的求值（将结果压入 `ValueStack`）。
- [ ] **基础一元/二元运算 (严格顺序)**: 将 `UnaryExpr` 和普通的 `BinaryExpr`（如 `+`, `-`, `*`）拆解为两步计算。**核心铁律**: 任务栈是后进先出，为了保证 Go 的左到右求值顺序，必须先压入 `OpApplyBinary`，再压入 `Eval(Right)`，最后压入 `Eval(Left)`。
- [ ] **复合类型构造**: 实现 `CompositeExpr`（Array/Map/Struct）的非递归构建，需管理多个子表达式结果的出栈组装。
- [ ] **索引与切片**: 实现 `IndexExpr` 和 `SliceExpr`，确保能够从 `ValueStack` 中正确获取目标对象和索引并计算。
- [ ] **成员访问**: 实现 `MemberExpr` 的非递归取值。
- [ ] **统一左值寻址 (OpEvalLHS)**: 实现对 `a`, `a[i]`, `a.m` 的非递归寻址。压入栈的不是值，而是 `LHS_Env`, `LHS_Index` 或 `LHS_Member` 描述符，供赋值或自增自减任务使用。

## 阶段三：分支流与短路求值 (Control Flow & Short-Circuit)
**目标**：引入基于任务栈修改的跳转逻辑。

- [ ] **短路逻辑 (`&&`, `||`)**: 引入 `OpJumpIf` 机制。先压入左侧求值，再压入条件检查指令。如果短路，则直接从 `ValueStack` 取值并跳过右侧求值任务。
- [ ] **If 语句**: 实现 `IfStmt`。计算条件后，根据栈顶的 bool 值决定是向 `TaskStack` 压入 `Body` 还是 `ElseBody`。
- [ ] **Switch 语句**: 将 `SwitchStmt` 的求值和匹配逻辑展平，通过任务队列依次评估 `CaseClause`。

## 阶段四：复杂作用域、循环与异常边界 (Scopes, Loops & Try)
**目标**：解决 Go 1.22 循环变量捕获问题和控制流拦截机制。

- [ ] **作用域指令对**: 实现 `OpScopeEnter` 和 `OpScopeExit`，替代原有的 `WithScope` 回调。确保在 `OpScopeExit` 时触发当前层级的 `Defer` 任务。
- [ ] **Block 语句**: 适配 `BlockStmt`，在进入和离开时注入 Scope 指令对。
- [ ] **For/Range 循环**: 引入特殊的循环锚点任务 (`OpLoopBoundary`)。每次循环通过任务调度模拟 Go 1.22 的迭代变量隔离拷贝和同步回写。为优化性能，考虑在 Task 的 Data 中引用差异表减少 Map 拷贝。
- [ ] **Try-Catch-Finally**: 
  - 将 `TryStmt` 拆解为压入 `OpFinally` (如果存在), `OpCatchBoundary` (如果存在), 然后是 `OpExec(Body)`。
  - 完全依靠阶段零设计的“解卷状态机”处理异常冒泡和 Finally 拦截。
- [ ] **中断 (Break/Continue)**: 重构 Interrupt 机制。当遇到 `InterruptStmt` 时，触发对应的 Unwind 模式，执行任务栈展开（Unwind），丢弃当前任务直到遇到匹配的循环锚点。

## 阶段五：函数、模块与副作用语句 (Calls, Modules & Mutations)
**目标**：实现函数调用的非递归化和副作用状态修改的原子性。

- [ ] **严格赋值顺序 (`AssignmentStmt` & `MultiAssignmentStmt`)**: 
  - 引入 `OpAssign` 和 `OpMultiAssign` 指令。
  - 根据 Go 语义，确保入栈顺序为：压入 `OpAssign` -> 压入所有 RHS 求值 -> 压入所有 LHS 寻址任务 (`OpEvalLHS`)。
- [ ] **原地增减 (`IncDecStmt`)**: 引入 `OpIncDec` 指令，弹出 `ValueStack` 中的 LHS 描述符，并进行原子性自增减，防止如 `a[f()]++` 时 `f()` 被计算两次的副作用。
- [ ] **函数调用 (`CallExprStmt`)**: 
  - 遇到调用时，先将所有参数压栈求值。
  - 实现 `OpDoCall` 指令，用于创建一个新的函数执行帧（Frame），将其压入 `TaskStack`。
  - **静态词法作用域 (Lexical Scoping)**: `OpDoCall` 必须显式地将其 `Stack` 的 `Parent` 指针连接到 **闭包的母上下文 (Context)** 或 **全局环境**，绝不能连接到动态的调用者作用域（避免动态作用域陷阱）。
- [ ] **返回机制 (`ReturnStmt`)**: 执行时进入 `UnwindReturn` 模式。触发 Unwind 逻辑，将返回值压入 `ValueStack`，并展开 `TaskStack` 直到退出当前的函数帧（即 `OpCallBoundary`）。
- [ ] **闭包 (`FuncLitExpr`)**: 确保闭包字面量构建时能够正确捕获当前上下文中的 `TypeCell` 变量。
- [ ] **内置函数与 FFI**: 对于同步的内建函数（如 `make`, `append`）和 FFI 路由调用，直接在 `OpDoCall` 阶段以同步原子的方式执行，并将结果压入 `ValueStack`。
- [ ] **模块加载平铺化 (`ImportModule`)**: 将 `import` 改造为任务。遇到新模块导入，挂起当前任务，压入新模块的执行任务，彻底消除 Import 导致的宿主栈递归。

## 阶段六：集成、测试与清理 (Integration & Switchover)
**目标**：无缝切换，保证 100% 语义向后兼容。

- [ ] **顶层入口接管**: 在 `IterativeExecutor` 中实现与旧 `Executor` 相同的 `Execute(ctx)` 接口，初始化全局变量和 Main 函数任务。
- [ ] **E2E 验证**: 运行 `core/e2e` 下的所有测试用例（尤其是 `concurrency_test.go`, `semantics_test.go`, `closure_test.go`, `recover_test.go`）。确保所有测试在非递归执行器下全绿。
- [ ] **性能分析 (Benchmark)**: 运行 `benchmark/engine_test.go`，确保非递归重构没有引入严重的性能退化，同时通过深度嵌套测试验证栈溢出已解决。
- [ ] **代码清理**: 确认稳定后，删除旧的 `executor.go` 中的递归逻辑，正式完成架构切换。
