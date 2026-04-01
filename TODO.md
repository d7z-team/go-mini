# TODO: Go-Mini 演进路径

更新时间: 2026-04-01

## Phase 1: Runtime 全量脱 AST (已完成)
**目标**: 编译/执行形态分离，执行主路径不再依赖 `ast.Node` 引用。

### A-E. 核心重构与加固 (Status: Done)
- [x] 建立 `Task` 数据负载模型与 Lowering 转换逻辑。
- [x] 移除 `Task.Node` 物理字段，调试转向 `SourceRef`。
- [x] `dispatch` 完全 Payload 化，移除所有 AST fallback 逻辑。
- [x] 建立 AST 节点 Lowering 覆盖矩阵文档 (@file:core/runtime/lowering_coverage.md)。
- [x] 全量测试与专项“无 AST”约束测试通过。

---

## Phase 2: 类型元数据内核化与 FFI/ffigen 对齐 (Next)
**目标**: 先统一类型与 FFI schema，再继续深层 runtime 重构，避免 `ffigen`、编译器、runtime 三套类型语义并存。**

### F. 统一 RuntimeType / FFISchema
- [x] **定义 `RuntimeType`**: 建立枚举化/结构化的运行时类型描述，替代执行期频繁解析 `ast.GoMiniType` 字符串。
- [x] **定义 `RuntimeFuncSig`**: 覆盖参数、返回值、变长参数、tuple 返回等 FFI 调用约束。
- [x] **定义 `RuntimeStructSpec`**: 覆盖字段顺序、字段类型、命名类型 ID、布局信息。
- [x] **定义 `RuntimeInterfaceSpec`**: 覆盖接口方法集、方法签名、后续 vtable 索引。
- [x] **定义 `TypeID` / `CanonicalPath` 规范**: 统一脚本类型名、`ffigen` 全路径类型名、FFI 句柄类型名的唯一标识规则。

### G. MiniExecutor 注册链路重构
- [x] **重构 `RegisterFFI`**: 内部不再长期存储原始 `ast.GoMiniType`，改为注册后立即解析为 `RuntimeFuncSig`。
- [x] **重构 `RegisterStructSpec`**: 结构体 schema 进入统一 `RuntimeStructSpec` 仓库。
- [x] **重构 `GetExportedSpecs`**: 编译器验证从统一 schema 派生，而不是直接暴露原始字符串 spec。
- [x] **梳理默认内置函数/标准库 spec**: `panic/recover/append/new/require` 与 stdlib ffigen 产物全部迁移到统一 schema。
- [x] **保留兼容层**: 对外 API 仍允许传入旧字符串 spec，但内部只保留解析后的 schema。

### H. ffigen 生成器升级
- [x] **升级 `ffigen` 元数据输出**: 生成代码能够输出可直接注册的 schema 结构，而不是只输出字符串 spec。
- [x] **保留第一阶段兼容模式**: 允许生成代码继续调用旧版 `RegisterFFI/RegisterStructSpec`，由运行时兼容层接管。
- [x] **统一指针/句柄语义**: 明确 `Ptr<T>` 在 FFI 边界上表示 handle，不与 VM 内部解引用语义混用。
- [x] **统一 reverse proxy 契约**: 生成的 `*_ReverseProxy` 与 `ToVar/InvokeCallable` 新表示兼容。
- [x] **补充 ffigen 迁移样例**: 至少覆盖 stdlib、普通 service、struct-direct、reverse proxy 四类生成模式。

### I. Runtime FFI 编解码脱 AST
- [x] **移除 `evalFFI` 对 `ast.GoMiniType.ReadCallFunc()` 的运行时依赖**。
- [x] **移除 `serializeVar/deserializeVar` 对 `ast.GoMiniType` 字符串分支的主依赖**。
- [x] **移除 FFI struct 编解码对 `e.program.Structs` 的依赖**，改为直接读取 `RuntimeStructSpec`。
- [x] **统一 Interface 编解码**: `VMInterface` 不再长期持有 `map[string]*ast.FunctionType`，改为运行时接口 schema。
- [x] **明确 Any/Handle/Error/Tuple 在线路协议中的表示**，减少启发式 fallback。

---

## Phase 3: Runtime 元数据彻底脱 AST
**目标**: 执行器内存结构不再持有 `ast.*` 元数据，类型检查和方法匹配全部转向 runtime metadata。**

### J. Executor 元数据清理
- [x] **移除 `Executor.structs` 对 `*ast.StructStmt` 的持有**。
- [x] **移除 `Executor.interfaces` 对 `*ast.InterfaceStmt` 的持有**。
- [x] **移除 `Executor.types` 对 `ast.GoMiniType` 直接映射的主依赖**。
- [x] **移除 `Executor.program` 在执行热路径中的类型元数据职责**，仅保留调试/LSP 所需边界。
- [x] **清理 `ExecExpr` 临时回退逻辑**，避免继续以 AST expr 作为执行期补丁入口。

### K. 接口与结构体运行时优化
- [x] **定义 `StructLayout`**: 字段顺序、偏移、大小、字段索引在 lowering/构建期完成。
- [x] **定义接口方法索引/vtable**: 消除 `CheckSatisfaction` 中昂贵的字符串签名匹配。
- [x] **重构 `CheckSatisfaction`**: 基于 `RuntimeInterfaceSpec` 与方法索引校验。
- [x] **收敛命名类型解析**: 命名类型、别名类型、FFI canonical path 类型统一走同一解析表。
- [x] **补齐命名类型防环机制**: 覆盖 primitive alias、alias 链、自引用/环引用，避免 `initializeType` / type resolution 自旋。
- [x] **统一 metadata registry**: 收敛 `namedTypes/namedTypeIDs`、`structSchemas/structTypeIDs`、`interfaceSpecs/interfaceTypeIDs` 的双表同步逻辑。
- [x] **移除接口 satisfaction 热路径懒解析**: `CheckSatisfaction` 不再在 cache miss 时运行期 `ParseRuntimeInterfaceSpec(...)`。
- [x] **清理 struct schema 线性扫描 fallback**: 统一 raw/typeID/canonical 查询路径，避免全表匹配。

---

## Phase 4: 静态符号解析与栈帧索引化
**目标**: 热路径不再通过变量名做 `map` 查找，闭包捕获与局部变量访问全部索引化。**

### L. Lowering/编译期符号解析
- [x] **引入符号解析上下文**: 区分 `Global/Local/Upvalue/Builtin/FFI`。
- [x] **为局部变量分配固定 slot**。
- [x] **为闭包捕获分配 upvalue 索引**。
- [x] **让 lowering 输出带 slot 信息的任务数据**，而不是只输出变量名。
- [ ] **收敛局部预声明为精确作用域分析**: 当前 `predeclareFunctionLocals` 仍是函数级近似方案，后续需要替换为按 block/branch/loop 精确建模。
- [ ] **补充符号解析异常场景测试**: 覆盖 shadowing、分支内声明、for/range/catch 局部变量、typed-nil AST 边界。

### M. 运行时栈帧重构
- [ ] **新增 `OpLoadLocal` / `OpStoreLocal`**。
- [ ] **新增 `OpLoadUpvalue` / `OpStoreUpvalue`**。
- [ ] **将 `Scope` 主存储从 `map[string]*Var` 迁移为 `[]*Var` 数组布局**。
- [ ] **保留调试名表**: 仅调试/LSP/错误报告保留 slot->name 映射。
- [ ] **重构 `CaptureVar/Load/Store/NewVar`**: 逐步从名字查找迁移到 slot 访问。
- [ ] **让 runtime 真正消费 `SymbolRef`**: 当前 `LoadVarData/DeclareVarData/LHSData/CallData/ClosureData` 已带符号元信息，但执行仍按名字路径运行。

### N. 闭包与调用链路收口
- [ ] **`VMClosure.Upvalues` 改为索引结构**，不再用 `map[string]*Var`。
- [ ] **函数入参与返回值走固定 frame 布局**。
- [ ] **评估并收敛 `ValueStack` 与 call frame 的职责边界**。

---

## Phase 5: LHS/指针/Any 语义收口
**目标**: 去掉当前赋值路径中的“值栈 + LHS 描述符 + Any 包装”混杂状态，简化寻址与更新逻辑。**

### O. 左值与寻址模型重构
- [ ] **引入专门的 `LHSStack` 或 `Address` 模型**，彻底分离“表达式值”与“赋值目标”。
- [ ] **`OpEvalLHS` 不再往 `ValueStack` 压入 `TypeAny` 包装描述符**。
- [ ] **抽出统一 `resolveAddress/loadAddress/storeAddress/updateAddress` 原语**。
- [ ] **合并 `Assignment` 与 `IncDec` 寻址路径**，减少重复实现。

### P. 指针与 Any/Box 语义统一
- [ ] **废弃 `__deref__` 成员 hack**，建立原生解引用 load/store 原语。
- [ ] **统一 `TypeAny` 自动拆箱**: 将 Any 穿透收敛到 `Var`/地址边界，而非散落在 opcode 中。
- [ ] **统一 `TypeCell` 读取语义**: 尽量避免各 opcode 手动拆 `Cell`。
- [ ] **明确 VM 指针语义与 FFI Handle/Ptr 语义边界**，避免再次混用。
- [ ] **拆分 `TypeAny` 清理清单**: 分别收敛算术、成员访问、调用、FFI、LHS 五类路径中的手动拆箱逻辑。

---

## Phase 6: 模块隔离、IR 对齐与持久化
**目标**: 让模块加载、编译产物、执行 IR 真正成为可隔离、可缓存、可序列化的工业化管线。**

### Q. 模块加载沙箱化
- [ ] **重构 `OpImportInit`**: 不再通过直接切换当前 session/executor 字段来“劫持”执行流。
- [ ] **引入独立 module session**: 模块初始化在隔离上下文执行。
- [ ] **引入显式 commit/rollback 机制**: 模块初始化失败时主 session 状态不被污染。
- [ ] **补充循环依赖、panic、部分初始化场景测试**。

### R. IR/Bytecode 管线统一
- [ ] **统一 `core/runtime/task.go` 与 `core/compiler/bytecode.go` 的指令集定义**。
- [ ] **明确唯一执行 IR**: 决定 runtime 直接执行 Task，还是编译成更稳定的 bytecode。
- [ ] **将 lowering 从 `Executor` 启动路径移出**，成为独立编译步骤。
- [ ] **实现 lowered IR / bytecode 持久化**，支持“一次编译，多次加载”。
- [ ] **清理展示型 bytecode 与真实执行 IR 双轨并存问题**。

---

## Phase 7: 验证、迁移与性能
**目标**: 用专项 benchmark 和迁移用例证明重构收益，不以“架构更漂亮”代替验收。**

### S. 测试与迁移验证
- [ ] **重构测试分层**: 将 VM 原生语义、FFI/ffigo 通道、ffilib 标准库、端到端脚本场景拆分为独立测试目录/套件，避免长期混在 `core/e2e`。
- [ ] **建立分层测试门禁**: `runtime unit`、`ffi bridge`、`ffilib integration`、`script e2e` 分层执行，并分别设定超时与失败归因。
- [ ] **拆分 ffilib 测试责任**: 标准库 host/bridge/schema 行为不再依赖脚本级 e2e 混合验证。
- [ ] **拆分 FFI/ffigo 与 VM 原生测试责任**: 宿主编解码、handle、bridge 协议与 VM 语义测试分离。
- [ ] **补充 FFI schema 兼容测试**: 覆盖旧版字符串 spec 和新版 schema 注册。
- [ ] **补充 ffigen 生成产物回归测试**: stdlib、业务 service、reverse proxy、canonical path。
- [ ] **补充 import 隔离回归测试**: panic、circular import、partial init。
- [ ] **补充 slot/upvalue 回归测试**: shadowing、closure、nested functions、loop capture。
- [ ] **补充 LHS/deref/Any 回归测试**。
- [ ] **补充命名类型/接口 vtable/canonical path 组合回归**: 覆盖 alias 链、FFI canonical type、interface satisfaction 交叉场景。
- [ ] **将 `go test ./core/e2e` 设为阶段门禁**: 关键阶段收尾必须通过全量 e2e，而不是只跑局部用例。

### T. Benchmark 与指标
- [ ] **建立局部变量 slot 访问 benchmark**。
- [ ] **建立接口 satisfaction/vtable benchmark**。
- [ ] **建立 FFI 编解码 benchmark**: 尤其是 struct、tuple、variadic、handle。
- [ ] **建立 metadata 解析/命中 benchmark**: 覆盖 named type、interface spec、struct schema 的注册期/运行期命中成本。
- [ ] **建立 import 初始化开销 benchmark**。
- [ ] **对比重构前后指标**: 目标符号查找开销降低 50% 以上，GC 压力降低 20% 以上。

### U. 兼容层退场与收口
- [ ] **移除 `ffigen` legacy registrar 分支**: 生成器不再输出 `RegisterFFI/RegisterStructSpec` 兼容代码。
- [ ] **移除 runtime 对旧字符串 spec 的兼容主路径**: 旧接口保留薄适配层，但执行/注册主链路只消费 schema。
- [ ] **制定兼容层下线顺序**: 先生成器、再标准库、再业务样例，最后删除 legacy API。
- [ ] **补充兼容层退场回归**: 确保删除 legacy 分支后 stdlib/业务生成物仍可稳定运行。

---

## 当前建议执行顺序
1. **先做 F/G/H/I**: 统一类型 schema，升级 `ffigen` 与注册链路，让 FFI/runtime/compiler 使用同一套 metadata。
2. **再做 J/K**: 清理 `Executor` 中的 AST 元数据持有，把接口/结构体校验转到 runtime metadata。
3. **再做 L/M/N**: 推进局部变量、栈帧、闭包捕获索引化。
4. **再做 O/P**: 收掉 LHS、deref、Any/Box 的语义债务。
5. **最后做 Q/R/S/T**: 完成模块隔离、IR 对齐、持久化与性能验证。

## 验收标准 (Definition of Done)
1. **纯净运行时**: `runtime.Executor` 与 FFI 编解码主路径不再持有任何 `ast.*` 运行时元数据。
2. **统一类型系统**: 编译器、runtime、`ffigen` 使用同一套 schema，不再长期依赖 `ast.GoMiniType` 字符串解析。
3. **零名称查找热路径**: 循环体、函数体、闭包体的局部变量访问不再通过字符串变量名查找内存。
4. **语义边界清晰**: VM pointer/deref、FFI handle/Ptr、Any/Box 三套语义边界明确且实现分离。
5. **模块安全隔离**: 跨模块导入失败、panic、循环依赖时，主 session 状态保持一致。
6. **生成链路稳定**: `ffigen` 生成代码在新旧兼容期内可平滑迁移，reverse proxy 与 canonical path 行为不回退。
7. **性能飞跃**: `go test ./core/benchmark` 与新增专项 benchmark 显示关键指标有显著提升。
