# go-mini LSP 增强实施路线图 (TODO-lsp)

本文档概述了 `go-mini` 语言服务协议 (LSP) 的优化方向、现有问题诊断及分阶段实施计划。

---

## 🛑 1. 现有问题诊断 (Problem Analysis)

目前 LSP 逻辑分散且处于初级阶段，存在以下核心缺陷：

- **[性能] 计算冗余且无状态**：每次查询（Hover/Definition）都触发全量 `Check()` 和 AST 遍历，随代码量增加延迟呈线性增长。
- **[语义] 作用域信息丢失**：`SemanticContext` 在校验后被丢弃，导致代码补全无法快速获取光标处的可见符号列表。
- **[元数据] FFI "黑盒"现象**：LSP 位于 `core/ast`，无法感知 `core/runtime` 中注册的 FFI 路由（如 `os.ReadFile` 的参数签名）。
- **[精度] 坐标映射鲁棒性差**：在处理复杂嵌套表达式（如 `a.B(c.D())`）时，`FindNodeAt` 容易定位偏移。
- **[深度] 类型推导不足**：Hover 只能显示基础类型，无法实现结构体字段的深度关联跳转。

---

## 🏗️ 2. 阶段化实施计划 (Phased Roadmap)

### 第一阶段：基础设施与符号索引 (Foundation & Indexing) —— 预计 3 MD
**目标**：建立持久化符号表，点亮 IDE 导航视图。

1. **符号表持久化 (Symbol Indexing)**
   - 重构 `SemanticContext`，校验后生成 `SymbolTable` 并缓存于 `MiniProgram`。
   - 记录每个 `Ident` 对应的定义、类型及文档注释。
2. **文档大纲 (Document Symbols)**
   - 实现 `QuerySymbols()` API，遍历顶层 `func/var/struct` 生成树状导航。
3. **坐标定位优化 (Precision Locating)**
   - 引入“最窄匹配算法”优化 `FindNodeAt`，确保在嵌套节点中精准击中最小叶子节点。

### 第二阶段：智能补全与 FFI 联动 (IntelliSense) —— 预计 5 MD
**目标**：实现“键入即补全”，打通宿主与沙盒的元数据。

1. **作用域回溯 (Scope Backtrack)**
   - 为 AST 节点关联所属 `Scope`。补全时向上爬升，收集所有可见符号。
2. **成员推导 (Member Completion)**
   - 实现 `SelectorExpr` (a.B) 的自动补全。基于 `a` 的类型推导其 `Fields` 或 `Methods`。
3. **FFI 元数据注入 (Bridge Metadata)**
   - 在 `MiniExecutor` 中增加 `GetExportedSpecs()`，将 `ffigen` 的元数据暴露给 LSP，支持 `os.` 等包补全。

### 第三阶段：参数签名与安全重构 (Signature & Rename) —— 预计 3 MD
**目标**：辅助复杂调用，提供基础重构能力。

1. **参数签名助手 (Signature Help)**
   - 键入 `(` 时触发，匹配函数定义并高亮当前参数位。
2. **安全重命名 (Rename)**
   - 基于 `FindAllReferences` 实现。增加冲突检查，确保新名称在作用域内唯一。

### 第四阶段：代码美化 (Formatter) —— 预计 5 MD
**目标**：实现生产级的 `mini-fmt`。

1. **AST Printer 实现**
   - 编写 `mini-ast` 到 Go 源码的逆向转换器。
2. **注释保留策略 (Comment Preservation)**
   - 在 `Parser` 阶段保留注释坐标，通过位置匹配插回生成的源码，确保非破坏性格式化。

---

## 🚀 3. 当前任务：符号表重构

**任务目标**：修改 `core/ast/ast_valid.go` 中的 `SemanticContext`，使其支持符号导出，并为 `MiniProgram` 增加缓存机制。

- [ ] 在 `SemanticContext` 中增加 `ExportedSymbols` 映射。
- [ ] 优化 `Check()` 流程，将发现的符号填入映射。
- [ ] 在 `MiniProgram` 中实现 `BuildSymbolIndex()` 懒加载逻辑。
