# Go-Mini 执行器非递归重构计划 (Iterative Stack-Machine) - COMPLETED

本项目旨在将 `go-mini` 核心的 AST 递归执行器重构为基于显式状态机的非递归（迭代式）执行器，以彻底消除深层递归导致的宿主栈溢出风险，并为未来的协程支持奠定基础。

**状态：已完成 (2026-03-21)**
**最终架构：非递归任务栈 + 统一解卷状态机 + 绝对内存隔离**

---

## 阶段零：安全规范与解卷状态机 (Phase 0: Specification) [DONE]
- [x] **定义指令流原子性**: 明确只有 `OpExec` 任务可作为断点触发点。
- [x] **解卷状态机 (Unwind State Machine)**: 实现了 `UnwindReturn`, `UnwindPanic`, `UnwindBreak`, `UnwindContinue` 模式。
- [x] **拦截矩阵 (Intercept Matrix)**: 正确拦截 `OpScopeExit` (defer), `OpCatchBoundary` (recover), `OpLoopBoundary`, 和 `OpCallBoundary`。
- [x] **ValueStack 多值与解包协议**: 实现了 Tuple 展开和 Cell 自动解包逻辑。
- [x] **栈平衡检查**: 在迭代循环中确保数据流隔离。

## 阶段一：基础设施与核心调度 (Foundation & Dispatcher) [DONE]
- [x] **定义操作码 (OpCode)**: 设计了完善的指令集（见 `core/runtime/task.go`）。
- [x] **定义任务与栈结构**: 实现显式 `TaskStack` 和 `ValueStack`。
- [x] **扩展 StackContext**: 集成运行时 Session 状态及暂停/恢复控制信号。
- [x] **实现主调度循环**: `Executor.Run()` 核心循环实现，支持指令计数和超时检查。

## 阶段二：基础表达式求值 (Basic Expressions Evaluation) [DONE]
- [x] **字面量与标识符**: 实现非递归加载。
- [x] **基础一元/二元运算**: 严格遵循从左到右求值顺序。
- [x] **复合类型构造**: 实现了 Array/Map/Struct 的分步组装。
- [x] **索引与切片**: 实现 `IndexExpr` 和 `SliceExpr` 非递归化。
- [x] **统一左值寻址 (OpEvalLHS)**: 统一了变量、索引、成员及指针（`*p`）的寻址逻辑。

## 阶段三：分支流与短路求值 (Control Flow & Short-Circuit) [DONE]
- [x] **短路逻辑 (`&&`, `||`)**: 通过 `OpJumpIf` 实现跳过右侧求值。
- [x] **If 语句**: 基于条件压入分支任务。
- [x] **Switch 语句**: 实现了 Case 分支的平铺化迭代匹配。

## 阶段四：复杂作用域、循环与异常边界 (Scopes, Loops & Try) [DONE]
- [x] **作用域指令对**: 通过 `OpScopeEnter/Exit` 管理作用域生命周期。
- [x] **For/Range 循环**: 完美适配 Go 1.22 迭代变量拷贝语义。
- [x] **Try-Catch-Finally**: 依靠解卷状态机实现高度鲁棒的异常处理。
- [x] **中断 (Break/Continue)**: 实现 Unwind 模式下的任务栈快速回退。

## 阶段五：函数、模块与副作用语句 (Calls, Modules & Mutations) [DONE]
- [x] **严格赋值顺序**: 确保 LHS 寻址早于 RHS 求值。
- [x] **原地增减 (`IncDecStmt`)**: 解决了自增副作用的原子性问题。
- [x] **函数调用 (`CallExprStmt`)**: 实现了非递归 Frame 创建和词法作用域绑定。
- [x] **返回机制 (`ReturnStmt`)**: 通过 `OpCallBoundary` 拦截 Return 解卷并回传值。
- [x] **模块加载平铺化 (`ImportModule`)**: 彻底消除 Import 导致的深度递归风险。

## 阶段六：集成、测试与清理 (Integration & Switchover) [DONE]
- [x] **顶层入口接管**: `Executor.Execute` 已完全切换到迭代引擎。
- [x] **E2E 验证**: 全量 60+ 测试用例 100% 通过。
- [x] **性能分析 (Benchmark)**: 确认重构未引入性能衰退，且消除了 Stack Overflow。
- [x] **代码清理**: 旧递归逻辑已彻底剥离。
