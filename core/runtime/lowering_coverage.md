# Go-Mini AST Lowering Coverage Matrix

本文档记录了 `go-mini` 中 AST 节点在 `core/lowering/task_lowering.go` 中的转换（Lowering）状态。AST 只允许停留在 frontend / compiler / lowering / analysis 边界，runtime 包不持有或导入 AST。

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
| `ast.GenDeclStmt` | **Lowered** | 转换为 `OpDeclareInitVars` (变量声明与可选初始化) |
| `ast.ProgramStmt` | **Handled at Lowering Init** | 在 lowering 初始化阶段处理，不进入执行任务路径 |
| `ast.FunctionStmt` | **Handled at Lowering Init** | 在 lowering 初始化阶段转换为 `PreparedFunction` |
| `ast.StructStmt` | **Handled at Lowering Init** | 在 lowering 初始化阶段写入 schema payload |
| `ast.InterfaceStmt` | **Handled at Lowering Init** | 在 lowering 初始化阶段写入 schema payload |
| `ast.BadStmt` | **Explicitly Rejected** | 非法节点在 lowering 阶段返回错误 |

## 2. 表达式 (Expressions)

| AST 节点 | 状态 | 说明 |
| :--- | :--- | :--- |
| `ast.BinaryExpr` | **Lowered** | 转换为 `OpApplyBinary` 或 `OpJumpIf` (逻辑运算) |
| `ast.UnaryExpr` | **Lowered** | 转换为 `OpApplyUnary` |
| `ast.IdentifierExpr` | **Lowered** | 变量转换为 `OpLoadVar`，命名常量转换为 `OpPush` |
| `ast.LiteralExpr` | **Lowered** | 转换为 `OpPush` |
| `ast.ConstRefExpr` | **Lowered** | 转换为 `OpPush` (常量值) |
| `ast.CallExprStmt` | **Lowered** | 转换为 `OpCall` + `CallData` |
| `ast.MemberExpr` | **Lowered** | 转换为 `OpMember` |
| `ast.IndexExpr` | **Lowered** | 转换为 `OpIndex` + `IndexData` |
| `ast.SliceExpr` | **Lowered** | 转换为 `OpSlice` + `SliceData` |
| `ast.CompositeExpr` | **Lowered** | 转换为 `OpComposite` + `CompositeData` |
| `ast.TypeAssertExpr` | **Lowered** | 转换为 `OpAssert` + `AssertData` |
| `ast.StarExpr` | **Lowered** | 转换为 `OpApplyUnary` (Dereference) |
| `ast.AddressExpr` | **Lowered** | slot 目标转换为 `OpAddressOf`，composite 目标转换为 `OpAddressAlloc` |
| `ast.ImportExpr` | **Lowered** | 转换为 `OpImportInit` |
| `ast.FuncLitExpr` | **Lowered** | 转换为 `OpMakeClosure` + `ClosureData` |
| `ast.BadExpr` | **Explicitly Rejected** | 非法节点在 lowering 阶段返回错误 |

## 3. 左值 (LHS)

| AST 节点 | 状态 | 说明 |
| :--- | :--- | :--- |
| `ast.IdentifierExpr` | **Lowered** | 变量转换为 `OpEvalLHS` (LHSTypeEnv)，常量作为调用参数 LHS 时转换为 `LHSTypeNone` |
| `ast.IndexExpr` | **Lowered** | 转换为 `OpEvalLHS` (LHSTypeIndex) |
| `ast.MemberExpr` | **Lowered** | 转换为 `OpEvalLHS` (LHSTypeMember) |
| `ast.StarExpr` | **Lowered** | 转换为 `OpEvalLHS` (LHSTypeStar) |
| `nil` | **Lowered** | 转换为 `OpEvalLHS` (LHSTypeNone) |
