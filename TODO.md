# TODO: Go-Mini 当前状态与剩余工作

更新时间: 2026-06-01

本文只记录当前架构状态、剩余事项和验证门禁。已完成的历史演进细节以 git 提交和对应测试为准，不在这里继续堆积。

## 当前架构状态

- 执行主路径以 `PreparedProgram` / `go-mini-bytecode` 为装载工件。
- `core/frontend` 定义源码前端边界；`core/gofrontend` 是 Go source / Go AST -> Mini AST 的唯一 Go 前端转换包，其他语言只能实现 frontend 输出 Mini AST。Go 风格类型必须在 Go 前端立即规范化。
- `ExecutableArtifact` 只保留 bytecode 与源码摘要，`ExecutableProgram` 只保留 bytecode artifact 与 runtime executor；AST、模板 hover 预览和 LSP 缓存只存在于 `AnalysisArtifact` / `AnalysisProgram` 或 compiler 内部 artifact。
- `core/lowering` 是 Mini AST -> `runtime.PreparedProgram` 边界；compiler 调用 `lowering.PrepareProgram`。
- `core/lowering` 对不支持的 AST 节点与非法 canonical type 返回 lowering error，不以 panic 作为主错误通道。
- Runtime 执行 `lowered task plan`，`Task` 只保留 opcode、payload 和 `SourceRef`。
- `PreparedProgram` 在生成、bytecode 装载和 executor 初始化阶段执行 task payload / scope-flow / exports 校验。
- canonical type 是 Mini AST / lowering / compiler / runtime 的统一类型格式；Go 风格类型在 Go 前端输入层规范化。
- Go 前端将 `byte` / `uint8` 规范化为 `Byte`，将 `rune` 和字符字面量规范化为 `Rune`；底层运行时仍以 `Int64` 数值存储，只有 `Byte` 在赋值/FFI/reflect 写入时校验 `0..255` 范围，`Rune` 不做 Unicode scalar 校验。`[]byte` 是 `Array<Byte>`，`[]rune` 是 `Array<Rune>`；公开 canonical type 没有独立 bytes 专用类型，FFI 只在 wire codec 内部对 `Array<Byte>` 使用紧凑 bytes 编码。
- canonical type 文本格式统一由 `core/typespec` 实现；`core/ast/ast_types.go` 是前端门面，`core/runtime/schema.go` 是 VM/schema 门面。
- 运算类型门禁由 `core/typespec` 统一定义；AST 语义检查与 runtime fallback 使用同一套二元运算、比较、nil-comparable 与赋值规则，`Any` 不再作为 `Equals` 通配符。
- 运算符重载是 compiler 阶段 AST 语法糖：前端只输出普通一元/二元表达式，AST 检查在原生运算不支持时解析接收者 `Op*` 方法，模板展开后、优化前改写为真实方法调用，lowering / bytecode / runtime 不保留重载分派。
- Go 前端保留源码常量值类型，AST 语义检查显式标记源码命名常量引用，lowering 写入 `PreparedProgram.Constants` / `ConstantTypes` 并把表达式常量降为 `OpLoadConst`；外部 FFI 常量只作为 package member schema 和 bytecode requirement 存在，不写入源码 AST 或 prepared source constants。
- FFI 统一为 schema-only 注册链路，生成代码、runtime schema 和 compiler 校验使用同一套 `RuntimeFuncSig` / `RuntimeStructSpec` / `RuntimeInterfaceSpec`；runtime FFI 返回路径按 wire schema 解码，不反射解构任意 Go host 值。
- FFI 常量在 `ffigen` 生成阶段落到显式 `ConstInt64` / `ConstByte` / `ConstRune` / `ConstFloat64` / `ConstString` / `ConstBool` constructor；schema、bound surface、compiler 外部依赖与 bytecode requirement 中只携带 canonical primitive 类型。
- 公开扩展入口统一为 `executor.UseSurface(...)`。
- FFI route / struct / interface schema 冲突判断由 runtime 统一实现，engine 与 runtime 注册路径复用同一套兼容性规则；FFI route、package value 和 surface 注册在所有冲突检查通过后才写入 executor 状态，bind 阶段产生的 pinned handle 失败时会回滚。
- 公开 FFI schema 使用具体 `HostRef<T>`、typed interface schema、`Error` 和 channel endpoint 表达宿主身份、错误与 channel；`Any` 面向纯值数据。
- VM `Any` slot 使用显式 wrapper 保持 nil 与动态值身份；VM pointer、HostRef、channel、module、closure、interface 和 host error/interface identity 可以留在 VM `Any` 这类 runtime-only 路径内；FFI Any wire 只承载纯值，继续拒绝 VM pointer、HostRef、channel、module、closure 和 host error/interface handle。
- MethodID 0 / `Invoke` 用于显式 schema route 与 typed interface method 调用。
- Runtime 以统一 module registry 表达源码库和 FFI package；FFI type-only schema 也注册为对应 FFI module 的 type member。module path 是唯一身份，source/FFI 不允许同路径共存，import、reflect 和 requirement 校验只查 registry。
- Compiler 会把已导入源码库和 FFI package 写入 bytecode `ModuleRequirements`；bytecode 装载会在执行前校验源码 module hash，以及 FFI 函数、常量、包值、类型 schema、方法 route 与 route MethodID。
- `ffigen` 生成 `SurfaceXxx(...) *surface.Bundle` / `SurfaceXxxSchema()`，通过结构化 `FFIRouteDecl` 一次声明 schema route，type method 使用 `TypePackagePath` / `TypeMemberName` 标识 owner，并由 `RouterBridge + BindSchemaRoutes` 绑定；Go 端 proxy 在显式 `ffigen:proxy` 时生成，`ffigen:global` 生成只读 HostRef package value。
- FFI 包值是 runtime 绑定的只读成员；HostRef 包值通过 pinned handle 保持生命周期，不受普通 handle destroy/remove 释放。
- 只处理原生值类型且无系统资源能力的默认标准库子集位于 `core/ffilib`，当前包括 native `errors.New` / `errors.Is` / `errors.As` / `errors.Unwrap` / `errors.Stack`、VM 源码库 `fmt` / native `fmt/internal`、native `reflect`，以及 FFI `strings`、`strconv`、`math`、`sort`；该子集由 `engine.NewMiniExecutor()` 默认注册，注册失败通过 error 返回。
- `reflect` 只读取 Go-Mini runtime / FFI schema metadata，用于 struct 字段、容器值、方法、函数、统一 module registry 包成员和基础 kind introspection；不会调用 Go 原生 `reflect` API。VM 源码 struct/interface 的 runtime identity 使用 `modulePath.Type`，不同模块中的同名同结构类型也不相等，`TypeFrom` 不做未限定短名查找；FFI 函数与 HostRef 类型可被反射，是因为 `ffigen` / surface schema 显式注册了 route/type metadata，FFI type owner 来自结构化 `PackagePath + MemberName` / `reflectspec.Owner`，不从 schema 文本拆分推断。编译期字符串字面量 `reflect.Package` / `TypeFrom` / `Zero` / `MakeMap` 会为已知 FFI package/type 记录 bytecode requirement，动态字符串 lookup 仅做运行期 metadata 查询。reflect API 声明集中在 `core/reflectspec`；`Field` / `Index` / `MapKeys` / `MapIndex` / `Unwrap` / `Elem` 返回 VM `Any`，array/map/struct 等可变容器返回 detached snapshot，pointer/HostRef/channel/module/closure/interface 等身份值保持 runtime identity；`Zero` / `MakeMap` / `SetField` / `SetMapIndex` / `Assign` / `Append` 按 VM `Any` 语义创建或写入运行时值，metadata struct 只读；缺失 lookup、嵌套 unknown named type 与不适用 index API 返回零值 metadata 或 `ok=false`，FFI/JSON/持久化边界单独执行纯值校验。
- VM 可见 `Error` 直接承载 Go `error`；VM 创建的 error 使用带 VM identity 的 `VMStackError` 记录创建点 stack，FFI 返回的 host error 使用 `VMHostError` 保留 handle/bridge identity 和可解析的 host error chain，`errors.Is/As` 与 VM 源码 `fmt.Errorf("%w")` 复用 Go error wrapping 语义。
- `fmt` 是 core 默认注册的单个 VM 源码库：`Print` / `Println` / `Printf` / `Sprint` / `Sprintf` / `Errorf` 使用 VM `reflect` 格式化值，支持 VM struct 值、VM pointer 和复合容器；只有 `fmt/internal.Write` 与 `fmt/internal.Errorf` 是 native helper，分别接收已格式化字符串和显式 error causes，不再通过 FFI `Any` 把 VM 值交给 Go `fmt`。
- 顶层 `ffilib` 继续承载完整标准库 FFI surface，负责注册 io/os/time/context/image 等外层资源、调度或模板能力；通过 `executor.UseSurface(ffilib.Surface())` 装配，core 纯库不需要外层手动重复装配。
- 顶层 `ffilib` 的 `encoding/json` 是单个 VM 源码库实现：`Marshal` / `Decode` / `Unmarshal(data, out any)` 通过 VM `reflect`、源码 parser/emitter 和普通源码函数完成，不调用 Go 标准库 `encoding/json`；`Unmarshal` 可以作为 runtime package member 或函数值暴露，pointer target 仅留在 VM `Any` 内，`Marshal` / `Decode` 的 JSON 纯值边界拒绝 VM pointer、HostRef、channel、module、closure、host identity 和循环值；JSON 转换与 tag helper 是 `encoding/json` 包内私有实现，不再拆分 helper module。
- `core/ffilib/testutil` 提供统一表达式/代码块 FFI 测试 harness；`core/ffilib` 与顶层 `ffilib` 模块测试均通过 `test.Out*` / `test.Done()` 校验执行完成与输出。
- 仓库采用 `core` / `ffilib` / `examples` 多模块布局，root 只保留 `go.work`、文档和仓库级脚本。
- 调用模板是 compiler 阶段能力：模板注册暴露 schema 给前端校验、LSP 补全与基于源码切片的 hover 渲染预览，随后在首次语义检查后、AST 优化前展开为真实 Mini AST；runtime / bytecode 不保留模板节点或模板执行逻辑。
- 模板函数支持全局保留名和包成员入口；包成员模式按实际使用校验，真实包/member 校验签名一致，template-only package member 只允许 direct call 且不进入 runtime exports。导入的 VM 源码库语义检查继承 template raw arg / template-only metadata。模板 raw arg 只在首次语义检查时跳过普通参数 assignability，展开后仍是普通 AST；模板可读取实参静态 canonical type，但 `encoding/json.Unmarshal` 这类普通 VM pointer target API 不再依赖 template。
- `core/ffigo` 承载 FFI wire / bridge / helper 类型。
- `core/e2e` 聚焦核心语言、runtime、module、FFI 机制测试；完整标准库 FFI 覆盖位于顶层 `ffilib`。
- `ffigen` 只保留 `-pkg` / `-out` 参数模型；CLI 位于 `core/cmd/ffigen`，生成器核心位于 `core/ffigen`，`ffigen:module` 是 VM 可见模块名来源。
- `core/surface.Bundle` 承载声明式 schema、runtime bind、compiler-only templates 和纯 VM 源码库；surface 冲突通过 `Bundle.Err` / `UseSurface` 返回错误。
- `core/surface.Bundle` 可以携带纯 VM 源码库；engine 在 `UseSurface` 阶段解析源码用于校验与 resolved module hash，随后只保留规范化源码描述，后续 compiler / LSP / module 装载每次按需重新解析 fresh AST。
- 纯 VM 模块不提供公开的预编译模块动态注册入口；VM 库只能通过 `surface.Library(...)` 装配，编译主程序时按实际 import 编译并嵌入 bytecode。
- 纯 VM 源码库的可见成员来自 `ModuleExports` / `PreparedProgram.Exports` 显式导出表，导出范围限定为 ASCII 大写开头的 Go-style exported identifier；runtime `TypeModule` 成员访问读取 `VMModule.Data`。
- executor 准备源码模块时先在 staging 中完成当前模块及依赖模块编译，全部成功后再把 prepared module closure 写入主程序 bytecode，避免依赖失败留下半成品执行工件。
- compiler 将导入的 surface library 写入 bytecode `ModuleRequirements`、`ModuleHashes` 和 `Modules`；runtime 只校验 embedded module hash 并从 `PreparedProgram.Modules` 装载 `PreparedProgram`，不提供动态 module loader。
- `PreparedFunction` 记录 VM 方法 receiver 元数据；源码库闭包中的私有指针方法值通过闭包词法 executor 与 receiver 索引解析。
- 多返回函数和 tuple-return FFI route 的结果可以直接作为另一个多返回函数的返回值转发。
- 标准库 `context` 由 VM 源码库提供公开 API，内部 `context/internal` FFI 只提供 sentinel error 与 timer；`WithValue` key 校验通过 VM `reflect` 的 `Map<Any, Bool>` 写入路径复用 map key 可比较规则。`Done()` 使用 VM receive-only channel，deadline timer 通过异步 FFI waiter 完成调度，已过期 deadline 同步取消，VM context 父子取消通过 child 注册表传播；`context` deadline / timeout 统一基于宿主真实时间。
- runtime run 统一挂载 `RunController`；独立 pause/resume、debugger breakpoint pause 和 continue/step 共用同一控制面，`ExecutableProgram.Start(...)` / `MiniExecutor.StartExecute(...)` 返回 `RunHandle` 供宿主控制当前 run。
- VM timer 统一来自 `VMClock` / `VMTimer`；pause 会冻结 `time.Sleep` 这类脚本等待，但 `time.Now` / `Since` / `Until`、`context.WithTimeout`、`context.WithDeadline` 和宿主 `context.Context` 的取消/截止时间继续使用真实时间。
- Eval 便捷入口先由 compiler/lowering 产出临时 `PreparedFunction`，runtime 执行 prepared function。
- VM 并发模型是单线程协作式 VM 执行上下文调度；`go f()` 创建子执行上下文，不返回 handle/result。
- 语言级 channel/select 已落到 Go frontend、AST 检查、lowering、bytecode payload 和 runtime；支持 `make(chan T[, cap])`、send/receive、二值 receive、`close`、`len`、`cap`、`select`、`default` 和 channel `for range`。
- channel canonical type 为 `Chan<T>` / `RecvChan<T>` / `SendChan<T>`，同样由 `core/typespec`、AST 门面和 runtime schema 门面统一解析与渲染。
- 上下文切换来自内部 safe point 或异步 FFI completion。
- 异步 FFI completion 由 VM 调度器内部队列接收，不因固定 channel 容量丢失；host goroutine 只入队 completion 和唤醒信号，不执行 VM task。
- 异步 FFI 启动后必须返回 `ffigo.WaitHandle` 描述等待来源；调度器区分 `WaitExternal` 与 `WaitDependsOnVM`，只有存在外部 wake source 时才会在全挂起状态继续等待。
- FFI schema 支持 channel endpoint：wire 上传递 channel endpoint ID，host 端通过 `ffigo.ChannelRegistry` / `ChannelEndpoint` 收发 payload；endpoint goroutine 等待 host channel、唤醒调度器并完成 wire 编解码。channel endpoint decode 会校验 schema 方向，endpoint close / host channel close 会通过 `UnregisterChannel` 释放 registry 条目。`ffigen` channel 参数使用方向类型代理。
- 当所有 VM 执行上下文都不可运行，且 pending async FFI 都依赖 VM 继续执行时，runtime 返回 `VMAllBlockedError`，错误包含 execution context、FFI route、method ID 和 wait reason。
- context 取消或 VM 致命错误会统一 abort 当前 run，取消 pending FFI，清理 module loading / waiters，并按 frame 错误路径执行必要 session 清理。
- 同步 FFI 调用阻塞整个 VM；只有返回 `ffigo.Async[T]` 的 FFI 会挂起当前 VM 执行上下文。
- FFI completion 时执行 copy-back；共享变量交错按 completion 处理顺序写回。
- VM 变量存储统一为 `Slot{Decl, Value}`；声明类型属于 slot，赋值只规范化并更新 slot value。
- VM struct 是独立 `TypeStruct` / `VMStruct`；struct 赋值、参数传递、返回值和 value receiver 按值复制字段 slot。
- VM array/map、VM pointer、closure、module、interface 和 host handle 按引用语义共享；VM 内部不并行执行，因此无宿主级数据竞争。
- VM pointer 是 runtime-only `TypePointer`，只保存 VM slot 引用，不使用 host handle ID，也不是宿主地址；解引用写入统一走 slot assignment 和声明类型校验，`Ptr<T>` 与 `T` 之间不做隐式互转。
- VM pointer 可以进入 VM `Any`、VM map/array 和 channel 等 runtime-only 值路径；不允许进入 FFI wire、JSON 纯值、持久化 wire 或 host identity 路径；运行时地址写入、channel send、composite literal、append/delete/index/slice 均执行目标类型校验。
- Go 前端支持 `&x`、`&T{...}` 和 `&struct{...}{...}`；VM 可寻址 slot 支持取地址与解引用写入。
- map key 保留 primitive key 类型，避免 string/int/bool/float key 在运行时被同一个字符串键混淆。
- FFI struct schema 区分 `VMValue` 和 `HostOpaque`；`HostOpaque` 以 `HostRef<T>` 形式进入 VM。
- Opaque host object 由 FFI factory/return 形成。
- root `main` 返回后，未完成子执行上下文立即停止；子执行上下文 panic 默认失败整个 VM，除非在子上下文内部 recover。
- Debugger pause event 显式暴露 `RunID` 和 `ExecutionContextID`；前者标识当前 run，后者表示触发暂停的 VM 执行上下文，root 通常为 1，子上下文为后续递增 ID。
- 局部变量、参数、返回值、upvalue 访问以 slot/frame 为主路径，名字表只服务调试和必要兼容查找。
- 模块导入、全局初始化、共享状态和 Eval/Execute 均通过 `SharedState + 独立 Session` 模型运行。
- bytecode JSON、prepared executable、module import、runtime 初始化均已接入 bytecode-first 主链；bytecode 装载执行使用 `Executable`。
- 对外 JSON / 持久化 / CLI 装载使用 `go-mini-bytecode`。
- Debugger session 的断点、按 run ID 绑定的单步策略和 `NextEvent(ctx)` 事件拉取均封装在并发安全方法后，运行中增删断点不会直接读写公开 map；debugger 事件在 VM 已进入 `Paused` 后投递，恢复执行和单步控制由 `RunHandle` 提供。
- stdio LSP 声明 full text sync；didOpen/didChange 进入 server 侧 diagnostics debounce，didSave 立即 flush pending diagnostics，didClose 取消 pending diagnostics 并清理旧诊断。

## 剩余工作

### Debugger 执行上下文标识与暂停策略

- [x] 决定调试事件显式暴露 VM 执行上下文标识。
- [x] 当前 debugger pause 策略固定为 all-stop；任一执行上下文命中断点或人工暂停时，整个 VM 通过统一 `RunController` 进入暂停。
- [x] 补齐 debugger 执行上下文标识与 all-stop 多上下文调试回归测试。
- [ ] 如后续需要 non-stop 多上下文调试，再单独设计 per-context pause 集合、命令路由和事件顺序。

### Channel / Select 语义

- [x] 评估并确认需要语言级 channel/select，以支持 `context.Context.Done()` 这类 receive-only channel FFI 形态。
- [x] 完成基于单线程 VM 调度器的语义设计，不新增 host goroutine 执行 VM task。
- [x] 明确 send/receive/select 与 async FFI completion 的调度关系：VM 内部 channel 等待是 `WaitDependsOnVM`，FFI channel endpoint 等待是外部 wake source。
- [x] 明确关闭、阻塞、取消、panic/recover 与 root 生命周期语义，并补齐 all-blocked 错误路径。
- [x] 实现 lowering / bytecode / runtime payload、Go frontend、FFI schema、`ffigen` 和 e2e 回归测试。

### Benchmark 与指标

- [ ] 建立局部变量 slot 访问 benchmark。
- [ ] 建立接口 satisfaction / vtable benchmark。
- [ ] 建立 FFI 编解码 benchmark，覆盖 struct、tuple、variadic、handle、copy-back、async return。
- [ ] 建立 metadata 解析/命中 benchmark，覆盖 named type、interface spec、struct schema 的注册期和运行期成本。
- [ ] 建立 import 初始化开销 benchmark。
- [ ] 输出当前基线数据，形成后续优化对比口径。
- [ ] 针对热点路径做优化前后指标对比，至少跟踪耗时、分配次数和 GC 压力。

## 待办

- [x] 支持 AST 层运算符重载语法糖，并在 compiler 中改写为真实方法调用。


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
