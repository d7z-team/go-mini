# Go-Mini AST Lowering Coverage Matrix

本文档记录了 `go-mini` 中所有 AST 节点在 `core/runtime/task_lowering.go` 中的转换（Lowering）状态，确保运行时完全脱 AST（AST-free）。

## 1. 语句 (Statements)

| AST 节点 | 状态 | 说明 |
| :--- | :--- | :--- |
| `ast.IfStmt` | **Lowered** | 转换为 `OpBranchIf` + `BranchData` |
| `ast.ForStmt` | **Lowered** | 转换为 `OpLoopBoundary` + `ForData` + `OpForCond` |
| `ast.RangeStmt` | **Lowered** | 转换为 `OpRangeInit` + `RangeData` + `OpRangeIter` |
| `ast.SwitchStmt` | **Lowered** | 转换为 `OpLoopBoundary` + `SwitchData` + `OpSwitchTag` |
| `ast.TryStmt` | **Lowered** | 转换为 `OpCatchBoundary` + `OpFinally` |
| `ast.DeferStmt` | **Lowered** | 转换为 `OpScheduleDefer` + `DeferData` |
| `ast.ReturnStmt` | **Lowered** | 转换为 `OpReturn` |
| `ast.AssignmentStmt` | **Lowered** | 转换为 `OpAssign` |
| `ast.MultiAssignmentStmt` | **Lowered** | 转换为 `OpMultiAssign` |
| `ast.IncDecStmt` | **Lowered** | 转换为 `OpIncDec` |
| `ast.BlockStmt` | **Lowered** | 转换为 `OpScopeEnter`/`OpScopeExit` |
| `ast.ExpressionStmt` | **Lowered** | 转换为表达式任务 + `OpPop` (若有返回值) |
| `ast.CallExprStmt` | **Lowered** | 转换为 `OpCall` + `CallData` |
| `ast.InterruptStmt` | **Lowered** | 转换为 `OpInterrupt` (break/continue) |
| `ast.GenDeclStmt` | **Lowered** | 转换为 `OpDeclareVar` (变量声明) |
| `ast.ProgramStmt` | **Handled at Init** | 在 `Executor` 初始化阶段处理，不进入执行路径 |
| `ast.FunctionStmt` | **Handled at Init** | 在 `Executor` 初始化阶段处理，转换为运行时闭包 |
| `ast.StructStmt` | **Handled at Init** | 在 `Executor` 初始化阶段处理，注册到结构体映射 |
| `ast.InterfaceStmt` | **Handled at Init** | 在 `Executor` 初始化阶段处理，注册到接口映射 |
| `ast.BadStmt` | **Explicitly Rejected** | 非法节点，在 Lowering 阶段抛出 Panic |

## 2. 表达式 (Expressions)

| AST 节点 | 状态 | 说明 |
| :--- | :--- | :--- |
| `ast.BinaryExpr` | **Lowered** | 转换为 `OpApplyBinary` 或 `OpJumpIf` (逻辑运算) |
| `ast.UnaryExpr` | **Lowered** | 转换为 `OpApplyUnary` |
| `ast.IdentifierExpr` | **Lowered** | 转换为 `OpLoadVar` |
| `ast.LiteralExpr` | **Lowered** | 转换为 `OpPush` |
| `ast.ConstRefExpr` | **Lowered** | 转换为 `OpPush` (常量值) |
| `ast.CallExprStmt` | **Lowered** | 转换为 `OpCall` + `CallData` |
| `ast.MemberExpr` | **Lowered** | 转换为 `OpMember` |
| `ast.IndexExpr` | **Lowered** | 转换为 `OpIndex` + `IndexData` |
| `ast.SliceExpr` | **Lowered** | 转换为 `OpSlice` + `SliceData` |
| `ast.CompositeExpr` | **Lowered** | 转换为 `OpComposite` + `CompositeData` |
| `ast.TypeAssertExpr` | **Lowered** | 转换为 `OpAssert` + `AssertData` |
| `ast.StarExpr` | **Lowered** | 转换为 `OpApplyUnary` (Dereference) |
| `ast.ImportExpr` | **Lowered** | 转换为 `OpImportInit` |
| `ast.FuncLitExpr` | **Lowered** | 转换为 `OpMakeClosure` + `ClosureData` |
| `ast.BadExpr` | **Explicitly Rejected** | 非法节点，在 Lowering 阶段抛出 Panic |

## 3. 左值 (LHS)

| AST 节点 | 状态 | 说明 |
| :--- | :--- | :--- |
| `ast.IdentifierExpr` | **Lowered** | 转换为 `OpEvalLHS` (LHSTypeEnv) |
| `ast.IndexExpr` | **Lowered** | 转换为 `OpEvalLHS` (LHSTypeIndex) |
| `ast.MemberExpr` | **Lowered** | 转换为 `OpEvalLHS` (LHSTypeMember) |
| `ast.StarExpr` | **Lowered** | 转换为 `OpEvalLHS` (LHSTypeStar) |
| `nil` | **Lowered** | 转换为 `OpEvalLHS` (LHSTypeNone) |
