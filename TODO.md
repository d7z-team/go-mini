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
- [x] **收敛局部预声明为精确作用域分析**: 已移除函数级 `predeclareFunctionLocals` 依赖，改为按 block/synthetic inner block 精确建模。
- [x] **补充符号解析异常场景测试**: 已覆盖 shadowing、分支内声明、for/range/catch 局部变量、typed-nil AST 边界。

### M. 运行时栈帧重构
- [x] **新增 `OpLoadLocal` / `OpStoreLocal`**。
- [x] **新增 `OpLoadUpvalue` / `OpStoreUpvalue`**。
- [x] **将根作用域全局绑定迁移到专用 global store**: session root/global 已使用独立 `Globals` 仓库，避免继续把全局变量塞进通用 `MemoryPtr`。
- [x] **将 `Scope` 主存储从 `map[string]*Var` 迁移为 `[]*Var` 数组布局**: locals/upvalues/return 已全部落到 `SlotFrame` 数组布局，globals 落到 root `Globals` 仓库，`MemoryPtr` 仅保留兼容名字绑定。
- [x] **清理 `MemoryPtr` / `SlotFrame` 双写同步**: local/upvalue/param/return/global 已不再镜像双写，`MemoryPtr` 只剩非索引化兼容绑定仓库。
- [x] **移除参数的名字别名主依赖**: 参数 slot 已成为主路径，名字查找仅通过 `SlotFrame` 名表兜底。
- [x] **保留调试名表**: 已建立 `SlotFrame` 的 slot->name 与 name->slot 索引，供调试/兼容查找使用。
- [x] **统一调试导出到 `SlotFrame` 名表**: debugger 变量导出已优先走 `SlotFrame` 名表，`MemoryPtr` 仅保留 global/import/module 兼容回退。
- [x] **重构 `CaptureVar/Load/Store/NewVar`**: 现有名字 API 已统一优先走 `SlotFrame` 名表与 slot/upvalue，`MemoryPtr` 仅保留 global/import/module 兼容路径。
- [x] **收敛特殊作用域绑定到 slot**: `range`/`catch`/`__return__`/参数/`nil` 与 root globals 已接入 slot/builtin/global-store 主路径，module session 也复用同一套 root/global store 规则。
- [x] **让 runtime 真正消费 `SymbolRef`**: `LoadVarData/DeclareVarData/LHSData/CallData/ClosureData` 已接入 slot/upvalue 主路径，名字查找仅保留兼容兜底。

### N. 闭包与调用链路收口
- [x] **`VMClosure.Upvalues` 改为索引结构**，不再用 `map[string]*Var`。
- [x] **移除 closure/runtime 对名字型 upvalue map 的主依赖**: closure 已仅保留 `UpvalueSlots + UpvalueNames`。
- [x] **函数入参与返回值走固定 frame 布局**。
- [x] **评估并收敛 `ValueStack` 与 call frame 的职责边界**: `CallBoundary` 已记录并恢复 `valueBase/lhsBase`，临时 LHS 状态已从 `ValueStack` 分离为独立 `LHSStack`。
- [x] **收紧调用链路兼容回退**: `LoadReturn/StoreReturn` 已改为严格依赖 frame return slot，调用热路径不再回退到 `"__return__"` 名字绑定。
- [x] **将 `OpCallBoundary` 元数据结构化**: 已收敛为 `CallBoundaryData` 强类型 payload，统一承载 `name/oldStack/oldExec/valueBase/lhsBase`。

---

## Phase 5: LHS/指针/Any 语义收口
**目标**: 去掉当前赋值路径中的“值栈 + LHS 描述符 + Any 包装”混杂状态，简化寻址与更新逻辑。**

### O. 左值与寻址模型重构
- [x] **引入专门的 `LHSStack` 或 `Address` 模型**，彻底分离“表达式值”与“赋值目标”。
- [x] **`OpEvalLHS` 不再往 `ValueStack` 压入 `TypeAny` 包装描述符**。
- [x] **抽出统一 `resolveAddress/loadAddress/storeAddress/updateAddress` 原语**: `LHSEnv/LHSIndex/LHSMember/LHSDeref` 已统一收敛为可读写地址解析入口，赋值/成员写入/索引写入/解引用写入共享同一套寻址原语。
- [x] **合并 `Assignment` 与 `IncDec` 寻址路径**，减少重复实现: `OpAssign` 与 `OpIncDec` 已统一走 `LHSValue -> resolveAddress -> store/update` 流程，多赋值也复用同一组地址原语。

### P. 指针与 Any/Box 语义统一
- [x] **废弃 `__deref__` 成员 hack**，建立原生解引用 load/store 原语: `*p` 的读写已统一走 `Dereference` + `LHSDeref` + `dereferenceValue/resolveAddress`，不再依赖成员式 hack。
- [x] **统一 `TypeAny` 自动拆箱**: VM 主路径已收敛到 `unwrapValue`/地址边界，算术、成员访问、调用、常用 builtin 与接口契约校验不再各自手写 `TypeAny -> *Var/*VMMap/*VMArray` 拆箱。
- [x] **统一 `TypeCell` 读取语义**: VM 主路径已优先走 `unwrapValue`/slot-frame 读取，减少 opcode/builtin 中散落的 `TypeCell` 手动拆箱；剩余清理主要集中在 FFI 编解码边界。
- [x] **明确 VM 指针语义与 FFI Handle/Ptr 语义边界**，避免再次混用: 已显式区分 VM 内部 pointer (`TypeHandle + Ref(*Var) + Bridge=nil`) 与宿主 opaque handle (`TypeHandle + Bridge/Handle`)，并在 member/deref/FFI Any 编解码路径按边界分别处理。
- [x] **拆分 `TypeAny` 清理清单**: 算术、成员访问、调用、FFI、LHS 五类主路径已统一收敛到 `unwrapValue`/`unwrapFFIValue`/地址原语，剩余零散兼容分支不再构成独立语义路径。

---

## Phase 6: 模块隔离、IR 对齐与持久化
**目标**: 让模块加载、编译产物、执行 IR 真正成为可隔离、可缓存、可序列化的工业化管线。**

### Q. 模块加载沙箱化
- [x] **重构 `OpImportInit`**: import 主路径已改为在独立模块上下文中同步执行并返回模块值，不再直接切换父 `session.Executor/Stack/ValueStack/LHSStack` 劫持执行流。
- [x] **引入独立 module session**: 脚本模块初始化已在独立 `module session` 中执行，父 session 仅接收成功产物与 step/module 状态回写。
- [x] **引入显式 commit/rollback 机制**: 模块初始化仅在成功后写入 `ModuleCache`；失败路径会回滚 `LoadingModules`，并保持父 session/module cache 不被半初始化状态污染。
- [x] **补充循环依赖、panic、部分初始化场景测试**: 已覆盖 circular import、运行期初始化失败导致的 rollback、以及 partial-init 不写入 `ModuleCache/LoadingModules` 的隔离回归。
- [x] **将 `panic` 纳入结束结构语义建模**: 返回路径分析已将 `panic(...)` 视为终止调用，模块初始化失败/回滚现在可以通过前端语义正确表达；更细的 `unreachable` 诊断仍可后续单列增强。

### R. IR/Bytecode 管线统一
- [x] **统一 `core/runtime/task.go` 与 `core/compiler/bytecode.go` 的指令集定义**: bytecode builder 已改为直接复用 `runtime.OpCode.String()` 输出真实 runtime opcode；少数 runtime 中不存在的展示专用指令已显式标成 pseudo op，避免继续伪装成同一套 IR。
- [x] **明确唯一执行 IR**: 当前执行主路径已明确为 `lowered task plan`，并已作为 `bytecode.executable` 段稳定挂入 bytecode 工件；`NewRuntimeByCompiled` 现在可直接消费 bytecode roundtrip 后恢复出的 executable plan。
- [x] **将 lowering 从 `Executor` 启动路径移出**，成为独立编译步骤: `NewRuntimeByCompiled`、模块 import 和通用 JSON 装载入口都已切到 prepared/bytecode-first；runtime 内部 `NewExecutor(program)` 兼容构造已删除，仓库内部默认不再走 AST 即时 lowering 主链。
- [x] **实现 lowered IR / bytecode 持久化**，支持“一次编译，多次加载”。
- [x] **清理展示型 bytecode 与真实执行 IR 双轨并存问题**: bytecode 现已成为唯一序列化/装载工件，`blueprint + executable` 承担真实装载职责，展示指令仅作为同一 bytecode 容器内的反汇编注释层，不再构成独立执行 IR。

---

## Phase 7: 验证、迁移与性能
**目标**: 用专项 benchmark 和迁移用例证明重构收益，不以“架构更漂亮”代替验收。**

### S. 测试与迁移验证
- [x] **重构测试分层**: 场景型/黑盒型测试已统一下沉到各模块测试目录；`ffilib` 测试归入各实际模块 `tests`，debugger 归入 `core/debugger/tests`，LSP/查询诊断归入 `core/ast/tests` 与 `core/tests`，pipeline/序列化/无 AST 归入 `core/tests` 与 `core/runtime/tests`，`ffigen` 输入/输出回归归入 `cmd/ffigen/tests`，`core/e2e` 主线回归则按 `language/functions/types/modules/ffi/runtime/security` 语义目录直接放置测试文件，不再额外套 `tests` 子层。
- [x] **建立分层测试门禁**: `Makefile` 已提供 `test-runtime`、`test-ffilib`、`test-ast`、`test-debugger`、`test-core`、`test-ffigen`、`test-script-e2e` 与 `test-layered`；其中 `test-script-e2e` 已直接覆盖 `./core/e2e/...`，避免新增语义分类目录时漏跑。
- [x] **拆分 ffilib 测试责任**: stdlib/json/time/strings/os/filepath/math/io/image 等回归已迁到 `core/ffilib/*/tests`，并压成最小 smoke/contract test。
- [x] **拆分 FFI/ffigo 与 VM 原生测试责任**: `ffigen` 输入/输出、reverse proxy、桥接样例与迁移回归已迁到 `cmd/ffigen/tests`；`canonicaltest/structtest/storagelib` 夹具测试已下沉到各自模块目录；`core/e2e` 已不再承担夹具堆放职责，只保留按语义分类组织的脚本执行主线回归；LSP host-FFI 交叉场景归入 `core/tests`。
- [x] **补充 ffigen 生成产物回归测试**: 已覆盖 stdlib、业务 service、reverse proxy、canonical path 与跨包 import 生成物。
- [x] **补充 import 隔离回归测试**: 已覆盖 panic、circular import、direct partial init 与 transitive partial init，不允许污染 `ModuleCache/LoadingModules`。
- [x] **补充 slot/upvalue 专项回归测试**: 已补齐 `scope_slots_test.go` 的多层 upvalue 转发共享 cell 回归，以及 `core/e2e/functions` 里的 nested closure mutation / shadowing / loop capture 组合回归。
- [x] **补充 LHS/deref/Any 回归测试**: 已补 `Any` 包 map/member、`Any+Cell+Ptr` 解引用写回、`Any` 包标量成员访问报错，以及 e2e 的 `Any` 包指针读写回归。
- [x] **补充命名类型/接口 vtable/canonical path 组合回归**: 已覆盖 alias 链、FFI canonical type、interface satisfaction 与 canonical path 交叉场景。
- [x] **将 `go test ./core/e2e/...` 设为阶段门禁**: 关键阶段收尾必须通过全量 e2e 语义树，而不是只跑局部用例。

### T. Benchmark 与指标
- [ ] **建立局部变量 slot 访问 benchmark**。
- [ ] **建立接口 satisfaction/vtable benchmark**。
- [ ] **建立 FFI 编解码 benchmark**: 尤其是 struct、tuple、variadic、handle。
- [ ] **建立 metadata 解析/命中 benchmark**: 覆盖 named type、interface spec、struct schema 的注册期/运行期命中成本。
- [ ] **建立 import 初始化开销 benchmark**。
- [ ] **对比重构前后指标**: 目标符号查找开销降低 50% 以上，GC 压力降低 20% 以上。

### U. 兼容层退场与收口
- [x] **移除 `ffigen` legacy registrar 分支**: `cmd/ffigen` 与仓库内 stdlib/业务生成物已切到 schema-only 注册，不再输出/依赖 `RegisterFFI/RegisterStructSpec` 双分支。
- [x] **移除 runtime 对旧字符串 spec 的兼容主路径**: `MiniExecutor.RegisterFFI/RegisterStructSpec/AddFuncSpec/AddStructSpec` 与 `compiler.Config.Specs` 已删除；主链只保留 schema 注册与 schema 校验入口。
- [x] **制定兼容层下线顺序**: 已按“生成器 -> 仓库内 stdlib 生成物 -> 业务/e2e 样例 -> 删除 legacy API”顺序完成收口。
- [x] **补充兼容层退场回归**: 已补 `ffigen_migration_test` 并验证 `go test ./...`，确保 stdlib/业务生成物在 schema-only 下稳定运行。

## 后续补充项
- [ ] **补充命名函数值语义**: 支持 `fn := increment`、命名函数作为参数/返回值的一等值语义，并补齐语义、lowering、runtime 与专项测试；当前仅函数字面量/返回函数闭包属于稳定支持范围。
- [ ] **明确 `ffigen` 的值对象 / 宿主对象生成边界**: 纯数据 struct、DTO、可完整字段编解码的类型应保持值语义 `T`；有方法集、共享身份、宿主资源或句柄生命周期的对象才生成 `Ptr<T>`/opaque handle。需要补齐判定规则、生成器回归与专项样例，避免把复杂类型无原则抬升为 `Ptr<T>`。
- [ ] **消除 `ffigen` 重复 `_FFI_StructSchema` 生成**: 同一包内多个 target 引用相同 struct/type 时，当前会各自重复生成 `X_FFI_StructSchema` / `X_StructSchema` 并触发 redeclare。需要引入包级 schema 去重与稳定命名策略，避免 `ffigen:module`、`ffigen:methods`、跨 target 共享类型时重复落盘。

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
