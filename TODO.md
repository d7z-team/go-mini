# TODO: Go-Mini 当前状态与剩余工作

更新时间: 2026-05-20

本文只记录当前架构状态、剩余事项和验证门禁。已完成的历史演进细节以 git 提交和对应测试为准，不在这里继续堆积。

## 当前架构状态

- 执行主路径以 `PreparedProgram` / `go-mini-bytecode` 为唯一装载工件，非调试运行不依赖 AST 节点。
- Runtime 执行 `lowered task plan`，`Task` 只保留 opcode、payload 和 `SourceRef`。
- Mini AST / lowering / compiler / runtime 只接受 canonical type；Go 风格类型只允许停留在 Go 前端输入层。
- FFI 统一为 schema-only 注册链路，生成代码、runtime schema 和 compiler 校验使用同一套 `RuntimeFuncSig` / `RuntimeStructSpec` / `RuntimeInterfaceSpec`。
- `ffigen` 只保留 `-pkg` / `-out` 参数模型，`ffigen:module` 是 VM 可见模块名来源。
- VM 并发模型是单线程 cooperative fiber；`go f()` 创建 VM fiber，不返回 handle/result。
- VM 侧不暴露公开 yield API；上下文切换来自内部 safe point 或异步 FFI completion。
- 异步 FFI completion 由 scheduler 内部队列接收，不因固定 channel 容量丢失；host goroutine 只入队 completion 和唤醒信号，不执行 VM task。
- context 取消或 VM 致命错误会统一 abort 当前 run，取消 pending FFI，清理 module loading / waiters，并按 frame 错误路径执行必要 session 清理。
- 同步 FFI 调用阻塞整个 VM；只有返回 `ffigo.Async[T]` 的 FFI 会挂起当前 fiber。
- FFI completion 时执行 copy-back；共享变量交错按 completion 处理顺序写回。
- VM 变量存储统一为 `Slot{Decl, Value}`；声明类型属于 slot，赋值只规范化并更新 slot value。
- VM struct 是独立 `TypeStruct` / `VMStruct`，不再复用 map；struct 赋值、参数传递、返回值和 value receiver 按值复制字段 slot。
- VM array/map、VM pointer、closure、module、interface 和 host handle 按引用语义共享；VM 内部不并行执行，因此无宿主级数据竞争。
- VM pointer 指向 VM slot，不是宿主地址；解引用写入统一走 slot assignment 和声明类型校验。
- map key 保留 primitive key 类型，避免 string/int/bool/float key 在运行时被同一个字符串键混淆。
- FFI struct schema 区分 `VMValue` 和 `HostOpaque`；`HostOpaque` 只能以 `HostRef<T>` 形式进入 VM。
- VM 不能创建 opaque host object；`T{}`、`var x T`、`new(T)` 以及直接包含 opaque value 的 VM 值类型会被编译/运行时拒绝。
- root `main` 返回后，未完成 child fiber 立即停止；child fiber panic 默认失败整个 VM，除非在 child 内部 recover。
- Debugger pause event 显式暴露 `FiberID`；root fiber 通常为 1，child fiber 为后续递增 ID。
- 局部变量、参数、返回值、upvalue 访问以 slot/frame 为主路径，名字表只服务调试和必要兼容查找。
- 模块导入、全局初始化、共享状态和 Eval/Execute 均通过 `SharedState + 独立 Session` 模型运行。
- bytecode JSON、prepared executable、module import、runtime 初始化均已接入 bytecode-first 主链。

## 剩余工作

### Debugger Fiber 标识与暂停策略

- [x] 决定调试事件显式暴露 fiber 标识。
- [x] 当前 debugger pause 策略固定为 all-stop；任一 fiber 命中断点或人工暂停时，整个 VM 暂停等待全局 command。
- [x] 补齐 debugger fiber 标识与 all-stop 多 fiber 调试回归测试。
- [ ] 如后续需要 non-stop 多 fiber 调试，再单独设计 per-fiber pause 集合、命令路由和事件顺序。

### Channel / Select 语义评估

- [ ] 评估是否需要语言级 channel/select。
- [ ] 若需要，先完成基于单线程 scheduler 的语义设计。
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
GOCACHE=/tmp/go-build-cache go test -timeout 180s ./core/runtime
GOCACHE=/tmp/go-build-cache go test -timeout 180s ./core/e2e/...
GOCACHE=/tmp/go-build-cache go test -timeout 180s ./...
```

涉及 CLI、`ffigen`、生成物或标准库 FFI 时，额外执行：

```bash
timeout 180s env GOCACHE=/tmp/go-build-cache go generate ./...
timeout 180s env GOCACHE=/tmp/go-build-cache make test
```

## 架构约束

- 非调试执行主路径不得重新引入 AST 节点依赖。
- 新能力必须先落到 lowering / compiler / bytecode payload，再由 runtime 消费。
- 对外 JSON / 持久化 / CLI 装载保持 bytecode-first。
- FFI 只走 schema-only，不引入 spec/registrar 双轨。
- Host opaque object 不得被 VM materialize；只能通过 FFI factory/return 形成 `HostRef<T>`。
- 引用/值语义相关改动必须保持 slot assignment 为唯一写入路径，避免重新引入 boxed cell 或无类型变量覆盖。
- VM 可见类型名保持短路径 / 模块路径语义，不把完整 Go import path 写回 schema 文本。
- VM 内部始终单线程执行，不新增宿主 goroutine 执行 VM 指令。
- 新增并发能力必须证明不会破坏单线程 scheduler 语义。
