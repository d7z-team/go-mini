# TODO: Go-Mini 演进路径

更新时间: 2026-03-31

## Phase 1: Runtime 全量脱 AST (基本完成)
**目标**: 编译/执行形态分离，执行主路径不再依赖 `ast.Node` 引用。

### A-E. 核心重构与加固 (Status: Done)
- [x] 建立 `Task` 数据负载模型与 Lowering 转换逻辑。
- [x] 移除 `Task.Node` 物理字段，调试转向 `SourceRef`。
- [x] `dispatch` 完全 Payload 化，移除所有 AST Fallback 逻辑。
- [x] 建立 AST 节点 Lowering 覆盖矩阵文档 (@file:core/runtime/lowering_coverage.md)。
- [x] 全量测试与专项“无 AST”约束测试通过。

---

## Phase 2: 深度解耦与性能演进 (Next Steps)
**目标**: 彻底消除运行时元数据对 AST 的依赖，实现静态符号解析，提升 VM 工业化程度。

### F. 类型系统与元数据脱 AST
- [ ] **定义 RuntimeType**: 建立二进制/枚举形式的运行时类型描述符，替代 `ast.GoMiniType` (string) 的实时解析。
- [ ] **结构体布局优化**: 在 Lowering 阶段计算 `StructLayout`（字段偏移量/大小），避免运行期成员寻址的动态解析。
- [ ] **接口虚函数表 (vtable)**: 预计算接口方法索引，消除 `CheckSatisfaction` 时昂贵的字符串签名匹配。
- [ ] **元数据清理**: 移除 `Executor` 中 `structs/interfaces/funcs` 字典对 `ast.*Stmt` 原始节点的持有。

### G. 静态符号解析与栈帧优化 (Static Resolution)
- [ ] **符号表构建**: 在 Lowering/编译期识别变量属性（Local/Upvalue/Global），分配固定索引。
- [ ] **引入局部变量指令**: 新增 `OpLoadLocal` / `OpStoreLocal`，直接使用 `int` 索引访问栈帧。
- [ ] **栈帧结构演进**: 将 `Scope` 存储从 `map[string]*Var` 迁移至 `[]*Var`（数组布局，降低查找开销）。
- [ ] **闭包捕获索引化**: 实现 Upvalues 的索引化访问，消除闭包内的变量名查找。

### H. 架构债务清理 (Refactor Obscure Logic)
- [ ] **左值描述符重构**: 引入专门的 `LHSStack` 或优化寻址机制，彻底分离“值计算结果”与“赋值目标描述符”。
- [ ] **解引用原语化**: 废弃 `__deref__` 成员 Hack，建立原生的指针解引用操作。
- [ ] **模块加载沙箱化**: 优化 `OpImportInit` 劫持逻辑，确保模块初始化在隔离的上下文进行，防止 Panic 导致会话状态错乱。
- [ ] **统一 Any/Box 逻辑**: 在 `Var` 底层统一 `TypeAny` 的自动拆箱，消除核心逻辑中重复的手动穿透。
- [ ] **合并寻址路径**: 整合 `IncDec` 与 `Assignment` 的代码路径，减少重复代码。

### I. 字节码管线整合与持久化
- [ ] **对齐指令集**: 统一 `core/runtime` 与 `core/compiler` 的指令集定义。
- [ ] **程序序列化**: 实现 Lowered Tasks 的二进制持久化，支持“一次编译，多次加载”。
- [ ] **离线编译**: 将 Lowering 逻辑彻底移至 `Executor` 启动之前的独立编译环节。

### J. 验证与性能
- [ ] **基准测试**: 建立针对栈帧索引访问、类型解析的专项 Benchmark。
- [ ] **性能指标**: 目标实现符号查找开销降低 50% 以上，GC 压力降低 20%。

## 验收标准 (Definition of Done)
1. **纯净运行时**: `Executor` 内存数据结构不包含任何 `ast` 包定义的类型。
2. **零名称查找**: 循环体（Hot path）内部不再通过字符串变量名查找内存。
3. **安全隔离**: 跨模块导入失败或报错时，主 Session 状态保持一致。
4. **性能飞跃**: `go test ./core/benchmark` 关键指标有量级提升。
