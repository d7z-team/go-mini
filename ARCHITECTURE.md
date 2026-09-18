# 架构

本文说明职责、依赖方向和状态所有权。接入方式见 [使用指南](USAGE.md) 与
[RPC 指南](RPC.md)，生成与验证见 [开发指南](DEVELOPMENT.md)。

## 包职责与依赖

| 路径 | 职责 |
| --- | --- |
| 根包 `minigo` | 嵌入 facade：Engine、配置与源码库注册 |
| `compiler/` | 源码检查、结构化类型、泛型、HIR、优化、链接与编译缓存 |
| `compiler/analysis`、`compiler/doc`、`compiler/format` | 源码索引、文档提取与格式化 |
| `compiler/language`、`compiler/service` | 语言查询、文档事务、增量快照与构建会话 |
| `runtime/bytecode/` | 字节码、执行镜像、调试符号和校验契约 |
| `runtime/` | Go 执行、调度、限制、调试与热更新 |
| `playground/runtime-rust/` | Rust 执行后端、tooling 和 WASM SDK |
| `ffi/` | 通用宿主调用、会话和完成事件 |
| `rpc/`、`rpc/router/`、`rpc/gateway/` | 绑定与资源、动态路由、发布与传输 |
| `stdlib/`、`stdlib/host/` | 标准库源码与能力声明；可选宿主 provider |
| `tooling/` | 文档渲染、LSP/DAP、MRPC 与数据工具 |
| `cmd/` | CLI 与维护命令入口 |

```text
minigo ──> compiler ──> runtime/bytecode <── runtime ──> ffi
   └──────────────────────────────────────> runtime

stdlib/host ──> stdlib、rpc、ffi
rpc/router ──> rpc ──> ffi
rpc/gateway ──> rpc/router、rpc
tooling ──> compiler facts、runtime debug API
cmd ──> library packages
```

compiler 拥有源码语义，runtime 消费字节码。宿主调用通过 FFI，RPC 通过适配器与生成源码扩展。
边界由 `scripts/check-boundaries.sh` 检查。

源码、资源与 `.mrpc` 声明是分发边界。字节码、缓存、生成绑定和 bootstrap bundle
是当前工具链的派生物，随事实源更新。

## 编译与链接

```text
应用源码 + 标准库/注册库 + 宿主注册的源码模块
  → SourceSet / 包依赖图
  → scanner / parser / AST
  → semantic / 结构化类型
  → 泛型特化 / typed HIR / 优化
  → package artifact
  → 入口链接与可达性裁剪
  → ExecutionImage + 可选 ProgramSymbols
```

compiler 的 Check 完成语义检查，Compile 生成包产物，Prepare 链接执行镜像。
Engine 的 Compile 封装完整流程并返回 Program。宿主在创建 Engine 前提供应用与库的源码，模块身份由逻辑导入前缀声明。

源码发现、包归属与位置映射由 `compiler/workspace` 集中管理。CLI、LSP 与 DAP 共用显式源码根，
编辑器位置索引不参与编译语义缓存。长期会话通过文档 overlay 或重建快照更新源码。

类型语义使用 `compiler/types` 的结构化模型；类型文本在源码、字节码、RPC 与显示边界转换。
分析与构建共享依赖类型事实，增量复用根据源码和依赖导出身份失效。
优化保持求值顺序、捕获与副作用；链接和运行时加载分别验证依赖、身份与指令。
Program 在验证及执行准备成功后才发布。

## 缓存与工具链身份

| 层次 | 缓存内容与失效依据 |
| --- | --- |
| Package | 包产物与导出；工具链、源码/资源、tags、优化级别及依赖导出 |
| Prepare | 执行镜像；可达包产物身份、入口及宿主能力要求 |
| Symbols | 独立调试信息；代码身份与源码位置 |

请求 context、宿主对象和凭据保持瞬态。符号可随源码位置更新而独立于执行代码。
缓存诊断与工具链生成见 [开发指南](DEVELOPMENT.md#缓存与性能诊断)。

## 执行与状态所有权

| 对象 | 所拥有的状态 |
| --- | --- |
| Engine / compiler session | 源码注册、配置与请求协调 |
| Program | 可跨实例共享的不可变代码、类型元数据与符号 |
| Instance | globals、初始化、revision、调度器、限额与独占 FFI Session |
| Execution / scope | 一次入口及派生任务、计时器、FFI 和累计费用 |
| 执行帧 | 局部变量、求值栈、defer 与容器迭代状态 |
| Host / Bridge / backend | 由嵌入方管理的共享宿主实现 |

Instance 的可变状态由单一 owner 管理。Poll 推进指令并消费完成事件；异步回调只交付事件，
不重入 VM。调试、快照和热更新在同一边界协调。
调度公平性、单次 Poll 的推进量与 scope 累计资源预算分别管理。

入口返回与后台任务结束是不同状态：library 调用可以留下后台任务，main 返回会结束其余任务。
取消和配额按 scope 结算；未恢复的后台 panic 使实例失败。关闭停止新工作，取消等待并回收资源。

实例生命周期为 Open → Closing → Closed；执行故障进入 Faulted，仍需关闭释放资源。
宿主管理业务接纳与正常退出，执行句柄区分入口结果和 scope 终态。关闭由实例 owner
取消剩余工作并等待宿主清理；等待者取消只撤销本次关闭等待。WASM Worker 按 FIFO 接纳调用，
关闭时取消已运行和排队的调用，并完成各自的结果通知。

FFI 结果只接受一次接收或丢弃决策，迟到回复也由 owner 回收。异步消费者持有缓冲直到完成。
Instance 关闭自己的会话，共享 Host 与 backend 由创建方关闭；宿主清理在 VM 锁外进行。

guest 费用与宿主实际内存分别观测。Rust 使用 arena 句柄和 tracing 回收；返回宿主的快照
独立拥有数据并保留循环和别名。类型元数据、缓存及反射对象受生命周期或预算约束。

## 语言服务与调试

Go LSP 原生调用 compiler 的语言核心。会话分别管理输入 revision 和分析 snapshot，
成功分析后发布结果，取消和失败保留原快照。构建捕获对应版本的源码与符号。

Rust tooling 与 TypeScript `./tools` 通过常驻编译器 VM 复用同一语言和源码装配规则。
宿主提供异步文件获取与传输，恢复时使用已确认的工作区和打开缓冲区。
服务连接拥有请求队列和 IO 任务，断开时关闭会话并回收任务。

调试目标与编译器使用独立实例。DAP 适配 runtime 调试 API，Rust 与 WASM 共用 Rust 适配层。
ProgramSymbols 绑定精确代码身份；变量引用属于一次暂停，恢复后失效。

## 热更新

版本诊断区分已发布 revision 与待提交 Program；有界引用扫描返回独立数据，不延长旧版本寿命。
补丁差异描述不可变代码之间的结构关系，实例准入和提交由运行时负责。
API 与使用边界见[热更新指南](USAGE.md#运行时热更新)。

热更新先准备、后提交：准备检查类型、globals、导出、入口和能力要求，失败保持原状态；
提交在 Instance owner 下切换 revision。已有帧、defer 和闭包保留旧 revision，
新命名调用使用当前 revision，兼容的 global 槽保持稳定。

FFI Session 跨 revision 存续。VM patch 与服务 publication 分别提交；
provider 替换先验证新服务，再切换新绑定入口并等待旧 lease 结束。
Router 对退出路由但尚未清理完成的服务继续承担关闭责任与容量管理。

## 标准库与 RPC

标准库纯逻辑位于 `stdlib/src`，时钟与随机熵由运行环境注入，console、文件系统和环境
通过 provider 接入。能力声明用于装配，强制能力在实例创建与热更新准备时校验。

| 组件 | 职责 |
| --- | --- |
| Binder / RouteSet | 契约匹配与绑定生命周期 |
| Result / resource | 接收提交资源，丢弃回滚；别名共享关闭状态 |
| FFI Host | 为各 VM 创建独立会话 |
| Endpoint | 双向操作、绑定租约、分片、限额与结果决策 |
| Router / Catalog | 路由选择、注册及一致的服务快照 |
| Publication / Gateway | 发布与替换、旧连接退役、认证与传输 |

operation 从请求到结果决定保持同一身份，binding 独立拥有服务与已接受资源。
各自的 owner 自动续租；撤销授权与清理完成是不同状态。资源借用持续到 handler 实际退出，
已接管的关闭和决定不随等待方取消。控制消息保留有界容量，并可穿插数据分片。

Rust RPC 使用宿主 Tokio runtime。WASM 由浏览器 Worker 或 Node worker_threads 驱动，
平台适配层交付网络与计时事件；JS 宿主对象通过 ID 和字节与 VM 交互。
原生同步 I/O 由有界 BlockingPool 管理，关闭等待资源及已拥有的清理任务完成。
