# TODO: Go-Mini 当前状态与剩余工作

更新时间: 2026-05-22

本文只记录当前架构状态、剩余事项和验证门禁。已完成的历史演进细节以 git 提交和对应测试为准，不在这里继续堆积。

## 当前架构状态

- 执行主路径以 `PreparedProgram` / `go-mini-bytecode` 为唯一装载工件，非调试运行不依赖 AST 节点。
- `core/gofrontend` 是 Go source / Go AST -> Mini AST 的唯一前端转换包；Go 风格类型必须在这里立即规范化。
- `core/lowering` 是唯一 Mini AST -> `runtime.PreparedProgram` 边界；compiler 调用 `lowering.PrepareProgram`，runtime 包和依赖图不再引入 `core/ast`。
- `core/lowering` 对不支持的 AST 节点与非法 canonical type 返回 lowering error，不以 panic 作为主错误通道。
- Runtime 执行 `lowered task plan`，`Task` 只保留 opcode、payload 和 `SourceRef`。
- `PreparedProgram` 在生成、bytecode 装载和 executor 初始化阶段执行 task payload / scope-flow 校验，非法 executable bytecode 必须在执行前拒绝。
- Mini AST / lowering / compiler / runtime 只接受 canonical type；Go 风格类型只允许停留在 Go 前端输入层。
- canonical type 文本格式统一由 `core/typespec` 实现；`core/ast/ast_types.go` 是前端门面，`core/runtime/schema.go` 是 VM/schema 门面，runtime 不再通过 AST 类型 API 拼接或解析 VM 类型文本。
- FFI 统一为 schema-only 注册链路，生成代码、runtime schema 和 compiler 校验使用同一套 `RuntimeFuncSig` / `RuntimeStructSpec` / `RuntimeInterfaceSpec`。
- FFI route / struct / interface schema 冲突判断由 runtime 统一实现，engine 与 runtime 注册路径复用同一套兼容性规则。
- 只处理原生值类型且无系统资源能力的标准库 FFI 子集位于 `core/ffilib`，当前包括 `errors`、`strings`、`strconv`、`math`、`sort`；该子集由 `engine.NewMiniExecutor()` 默认注册。
- 顶层 `ffilib` 继续承载完整标准库 FFI 装配，负责注册 io/os/time/fmt/image 等外层资源、调度或模板能力；core 纯库不需要外层手动重复装配。
- 仓库采用 `core` / `ffilib` / `examples` 多模块布局，root 只保留 `go.work`、文档和仓库级脚本。
- 调用模板是 compiler 阶段能力：模板注册暴露 schema 给前端校验、LSP 补全与基于源码切片的 hover 渲染预览，随后在首次语义检查后、AST 优化前展开为真实 Mini AST；runtime / bytecode 不保留模板节点或模板执行逻辑。
- 模板函数支持全局保留名和包成员入口；包成员模式按实际使用校验，真实包/member 必须校验签名一致，不存在的包作为编译期 facade 处理且不需要 dummy module，fixed-point 展开后必须从 AST import、artifact imports 与 bytecode 中完全移除，并由最终无模板语义检查兜底。
- `core/ffigo` 只承载 FFI wire / bridge / helper 类型，不得引入 Go parser/AST 或 Mini AST converter。
- `core` 不得 import 或调用顶层 `ffilib`；除 `core/ffilib` 纯库测试外，`core/e2e` 只保留核心语言、runtime、module、FFI 机制测试，不依赖顶层标准库装配。
- `ffigen` 只保留 `-pkg` / `-out` 参数模型；CLI 位于 `core/cmd/ffigen`，生成器核心位于 `core/ffigen`，`ffigen:module` 是 VM 可见模块名来源。
- VM 并发模型是单线程协作式 VM 执行上下文调度；`go f()` 创建子执行上下文，不返回 handle/result。
- VM 侧不暴露公开 yield API；上下文切换来自内部 safe point 或异步 FFI completion。
- 异步 FFI completion 由 VM 调度器内部队列接收，不因固定 channel 容量丢失；host goroutine 只入队 completion 和唤醒信号，不执行 VM task。
- context 取消或 VM 致命错误会统一 abort 当前 run，取消 pending FFI，清理 module loading / waiters，并按 frame 错误路径执行必要 session 清理。
- 同步 FFI 调用阻塞整个 VM；只有返回 `ffigo.Async[T]` 的 FFI 会挂起当前 VM 执行上下文。
- FFI completion 时执行 copy-back；共享变量交错按 completion 处理顺序写回。
- VM 变量存储统一为 `Slot{Decl, Value}`；声明类型属于 slot，赋值只规范化并更新 slot value。
- VM struct 是独立 `TypeStruct` / `VMStruct`，不再复用 map；struct 赋值、参数传递、返回值和 value receiver 按值复制字段 slot。
- VM array/map、VM pointer、closure、module、interface 和 host handle 按引用语义共享；VM 内部不并行执行，因此无宿主级数据竞争。
- VM pointer 指向 VM slot，不是宿主地址；解引用写入统一走 slot assignment 和声明类型校验。
- map key 保留 primitive key 类型，避免 string/int/bool/float key 在运行时被同一个字符串键混淆。
- FFI struct schema 区分 `VMValue` 和 `HostOpaque`；`HostOpaque` 只能以 `HostRef<T>` 形式进入 VM。
- VM 不能创建 opaque host object；`T{}`、`var x T`、`new(T)` 以及直接包含 opaque value 的 VM 值类型会被编译/运行时拒绝。
- root `main` 返回后，未完成子执行上下文立即停止；子执行上下文 panic 默认失败整个 VM，除非在子上下文内部 recover。
- Debugger pause event 显式暴露 `ExecutionContextID`；该字段表示 VM 执行上下文 ID，root 通常为 1，子上下文为后续递增 ID。
- 局部变量、参数、返回值、upvalue 访问以 slot/frame 为主路径，名字表只服务调试和必要兼容查找。
- 模块导入、全局初始化、共享状态和 Eval/Execute 均通过 `SharedState + 独立 Session` 模型运行。
- bytecode JSON、prepared executable、module import、runtime 初始化均已接入 bytecode-first 主链；bytecode 装载执行只使用 `Executable`，不从展示信息重建 AST。

## 剩余工作

### Debugger 执行上下文标识与暂停策略

- [x] 决定调试事件显式暴露 VM 执行上下文标识。
- [x] 当前 debugger pause 策略固定为 all-stop；任一执行上下文命中断点或人工暂停时，整个 VM 暂停等待全局 command。
- [x] 补齐 debugger 执行上下文标识与 all-stop 多上下文调试回归测试。
- [ ] 如后续需要 non-stop 多上下文调试，再单独设计 per-context pause 集合、命令路由和事件顺序。

### Channel / Select 语义评估

- [ ] 评估是否需要语言级 channel/select。
- [ ] 若需要，先完成基于单线程 VM 调度器的语义设计。
- [ ] 明确 send/receive/select 与 async FFI completion 的调度关系。
- [ ] 明确关闭、阻塞、取消、panic/recover 与 root 生命周期语义。
- [ ] 设计 lowering / bytecode / runtime payload 结构后再进入实现。

### Benchmark 与指标

- [ ] 建立局部变量 slot 访问 benchmark。
- [ ] 建立接口 satisfaction / vtable benchmark。
- [ ] 建立 FFI 编解码 benchmark，覆盖 struct、tuple、variadic、handle、copy-back、async return。
- [ ] 建立 metadata 解析/命中 benchmark，覆盖 named type、interface spec、struct schema 的注册期和运行期成本。
- [ ] 建立 import 初始化开销 benchmark。
- [ ] 输出当前基线数据，形成后续优化对比口径。
- [ ] 针对热点路径做优化前后指标对比，至少跟踪耗时、分配次数和 GC 压力。

## 变更门禁

每次涉及 runtime、compiler、bytecode、FFI 或标准库生成物的改动，至少执行：

```bash
GOCACHE=/tmp/go-build-cache bash -lc 'cd core && go test -timeout 180s ./runtime ./runtime/tests'
GOCACHE=/tmp/go-build-cache bash -lc 'cd core && go test -timeout 180s ./e2e/...'
GOCACHE=/tmp/go-build-cache bash -lc 'cd ffilib && go test -timeout 180s ./...'
GOCACHE=/tmp/go-build-cache bash -lc 'cd examples && go test -timeout 180s ./...'
```

涉及 CLI、`ffigen`、生成物或标准库 FFI 时，额外执行：

```bash
timeout 180s env GOCACHE=/tmp/go-build-cache make gen
timeout 180s env GOCACHE=/tmp/go-build-cache make test
```

覆盖率报告使用跨包覆盖口径：

```bash
timeout 180s env GOCACHE=/tmp/go-build-cache make coverage
```

## 架构约束

- 非调试执行主路径不得重新引入 AST 节点依赖。
- runtime 包及其依赖图不得引入 `core/ast`；AST 相关转换必须停留在 `core/gofrontend`、`core/lowering`、compiler 或分析/调试边界。
- `core/ffigo` 不得 import `core/ast`、`go/ast`、`go/parser`、`go/scanner`、`go/token`；Go 前端转换只允许在 `core/gofrontend`。
- `core` 不得 import 顶层 `ffilib`；`core/ffilib` 只承载并默认注册纯原生类型标准库子集，完整标准库 FFI 只能由顶层 `ffilib.RegisterAll` 装配。
- 新能力必须先落到 lowering / compiler / bytecode payload，再由 runtime 消费。
- 对外 JSON / 持久化 / CLI 装载保持 bytecode-first。
- FFI 只走 schema-only，不引入 spec/registrar 双轨。
- Host opaque object 不得被 VM materialize；只能通过 FFI factory/return 形成 `HostRef<T>`。
- 引用/值语义相关改动必须保持 slot assignment 为唯一写入路径，避免重新引入 boxed cell 或无类型变量覆盖。
- VM 可见类型名保持短路径 / 模块路径语义，不把完整 Go import path 写回 schema 文本。
- 除 `core/typespec` 和 `core/ast/ast_types.go` 外，不得手动拼接 canonical type 文本；前端走 `ast_types` 构造器，VM/runtime 走 `runtime.TypeSpec` / schema 构造器。
- VM 内部始终单线程执行，不新增宿主 goroutine 执行 VM 指令。
- 新增并发能力必须证明不会破坏单线程 VM 调度器语义。
