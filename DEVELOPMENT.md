# 开发指南

本文集中说明开发环境、生成、验证与诊断。用户接入见 [USAGE.md](USAGE.md) 和
[RPC.md](RPC.md)，职责与生命周期见 [ARCHITECTURE.md](ARCHITECTURE.md)。

[日常验证](#日常工作流) · [生成](#生成与文档) · [测试组织](#测试组织) ·
[Rust](#rust-验证) · [WASM](#wasm-与-typescript) · [诊断](#缓存与性能诊断)

## 开发环境

Go 开发及标准库差分基准使用 **Go 1.26.6**，Rust 使用 **1.98.1**，命令编排使用 make。
依赖版本由 go.mod、Cargo.lock 和 npm lockfile 固定，lint 版本由 Makefile 固定。

Go/Mini-Go 开发使用 Go 工具链；完整生成还需要 rustfmt。Rust 与 WASM 开发从 Git checkout 进行。

Makefile 默认导出 `GOTOOLCHAIN=go1.26.6`，覆盖其启动的 Go 命令与子进程；显式设置的
环境变量或 make 参数优先。直接调用 `go` 时，本机版本不符则设置 `GOTOOLCHAIN=go1.26.6`。

## 日常工作流

在仓库根运行 `make` 或 `make help` 查看目标说明。目标按生成、Go、fuzz、Rust、RPC、
WASM 和清理分组；先运行行为所属包的测试，再按改动影响扩大验证：

```bash
make test TEST_PACKAGES='./compiler/semantic' TEST_FLAGS='-run TestName'
make test TEST_PACKAGES='./rpc/... ./integrations'
make race RACE_PACKAGES='./rpc/...'
make lint test build
```

`TEST_FLAGS` 替换默认参数；`-count=1` 仅跳过 Go 测试结果缓存，Mini-Go 编译缓存仍可复用。
race 默认按包串行运行，可用 `RACE_PACKAGES=./...` 扩大范围。

同一次 make 调用按顺序执行目标及其前置任务，`make -j` 也保持这层编排串行，避免生成、
构建和测试争用共享产物。Go、Cargo 内部仍使用各自的并行策略；需要控制资源时可设置
`GOFLAGS=-p=1`、`GOMAXPROCS`、`CARGO_BUILD_JOBS` 和 `RUST_TEST_THREADS`。

| 修改范围 | 验证 |
| --- | --- |
| Go 实现或共享行为 | 目标测试；`make lint test build` |
| 并发与资源生命周期 | 相关 race、取消、关闭和失败路径测试 |
| compiler、stdlib、schema、generator | 先完成 `make generate`，再测试派生物 |
| stdlib API 或源码注释 | `make doc` 与相关测试 |
| Rust / WASM | 下文对应目标及配置下的 Clippy |
| 跨语言协议 | 两侧独立测试与 `make test-rpc-conformance` |
| 手写文档 | 检查链接、命令与示例 |

实现按稳定职责组织，公共 API 调整同时核对调用方式。提交前检查 `git diff --check`，
生成输入与 Git 跟踪的派生物一起提交，并报告实际完成的验证。

## 生成与文档

根 [generate.go](generate.go) 是统一生成入口：

| 事实源 | 派生物 |
| --- | --- |
| `.mrpc` 声明 | Go、Mini-Go、Rust binding |
| 编译器、bytecode 与标准库源码 | bootstrap bundle |
| compiler / bytecode 源码 | compiler identity |
| bytecode 模型、工具 DTO | [spec](spec/README.md) 契约与 Rust 描述 |
| 共享源码与行为观察 | 预编译镜像、差分数据和 manifest |
| compiler 词法与类型事实 | VS Code grammar |

```bash
make generate
make doc
```

生成完成后再测试，保持源码、identity 与镜像一致。schema、envelope 或缓存格式变化时更新
所属 version/domain；重复生成应逐字节一致。

Rust tooling 与 npm tools 使用根生成流程产出的 compiler 镜像。
`testdata/runtime/{execution,stdlib}.json.gz` 和 `playground/runtime-rust/tooling/assets/compiler.json.gz`
按需生成并在本地复用。`make runtime-artifacts` 补齐全部三份镜像，
`make runtime-compiler-image` 仅补齐 compiler 镜像；相关 Make 测试目标准备各自所需文件。
SDK 构建自动准备编译器镜像，最终 npm 分发包包含该镜像。
直接运行消费镜像的 Cargo 或 Go 测试前先准备产物。按需目标只补齐缺失文件；
修改生成输入后执行 `make generate` 更新产物与 manifest，完成后再启动测试。
升级移植依赖时审查适配、保留许可证，并验证原生与 VM 行为。

`docs/reference` 从标准库源码注释生成，维护入口为 `make doc`。
手写文档按以下职责组织，调查和实施记录保存在任务记录中：

| 文档 | 侧重 |
| --- | --- |
| [README](README.md) | 安装、可运行入门示例和导航 |
| [USAGE](USAGE.md)、[RPC](RPC.md) | 使用方式、配置及调用方需要遵守的契约 |
| [ARCHITECTURE](ARCHITECTURE.md) | 职责、依赖、数据流与生命周期 |
| 本文 | 维护、生成、测试和诊断 |
| 组件 README / 使用指南 | 入门导航 / 完整接入示例与生命周期契约 |
| [testdata](testdata/README.md) | 共享数据的归属、预期与更新方式 |
| [AGENTS](AGENTS.md) | AI/agent 执行仓库任务的约束 |

## 测试组织

测试放在行为所属 Go package 或 Rust 模块；标准库公开行为放在同目录的 `*_test.mgo`。
无单一包 owner 的跨层场景归 [integrations](integrations/README.md)，共享输入见
[testdata 导航](testdata/README.md)。

runtime 状态机优先用最小字节码和精确 PollSteps；跨层行为才编译源码。
测试验证结果、诊断和生命周期，区分入口返回、scope 结束与宿主清理，
负责回收实例、连接、资源与 goroutine。计数边界通过确定性状态构造验证。
复杂构造器集中在所属 domain 的测试辅助文件。

重型编译和集成测试复用磁盘缓存；失效规则、故障注入与阶段测试使用隔离 backend。
共享数据区分手写预期和生成观察，两侧各自校验；跨进程 peer 的构建由 Makefile 编排。

## Rust 验证

在仓库根目录按改动选择目标。安装见 [Rust README](playground/runtime-rust/README.md)，
接入 API 见 [Rust 使用指南](playground/runtime-rust/USAGE.md)。

| 命令 | 范围 |
| --- | --- |
| `make runtime-rust-lint` | rustfmt、workspace 全 feature/target 的 Clippy |
| `make runtime-rust-test` | 默认 VM 测试与 release 模式的 tooling 测试 |
| `make runtime-rust-rpc-test` | RPC、生成绑定与 Gateway |
| `make runtime-rust-host-test` | 原生 Host 与清理生命周期 |
| `make runtime-rust-host-conformance` | Rust provider 执行标准库镜像 |
| `make runtime-rust-conformance` | Go provider 经 broker 执行相同镜像 |
| `make test-rpc-conformance` | Go/Rust Endpoint 与 Gateway 互操作 |
| `make runtime-compiler-test` | Rust、Node、Chromium 的 compiler entry 与语言工具 |
| `make runtime-rust-bench` | 固定工作量的执行、分配与 GC 观测 |

Cargo 消费预生成镜像，修改 compiler 或共享输入后先完成根生成。
tooling 的编译器会话测试使用 release 模式，在生产请求期限内验证编译工作量。
调度、帧复用、GC 和热更新变更同时验证资源计费、取消、关闭与失败后的状态。

## WASM 与 TypeScript

需要 Node.js 22.18+、Rust 的 `wasm32-unknown-unknown` 标准库，以及匹配 Cargo.lock 的
wasm-bindgen-cli（当前 0.2.128）：

```bash
rustup target add wasm32-unknown-unknown
cargo install wasm-bindgen-cli --version 0.2.128 --locked
make runtime-wasm-build
npm exec --prefix playground/runtime-rust/runtime-wasm -- playwright install chromium firefox
make runtime-wasm-test
make runtime-wasm-pack
```

build 安装 npm 开发依赖并生成 TypeScript、声明、Worker 和 WASM；
test 准备 fixture 与 Go peer，验证类型、格式、浏览器/Node、RPC 及独立安装包。
runtime 浏览器测试默认使用 Chromium/Firefox，`MINIGO_BROWSERS` 可选择引擎；
tools 和打包消费测试使用 Chromium。WebKit 还需相应宿主库。

在 `playground/runtime-rust/runtime-wasm` 中可直接运行 `npm ci`、`npm run build`、
`npm run lint`；`npm run build -- --core` 构建无 RPC 的版本。
pack 通过 prepack 构建完整 RPC 分发，失败构建保留原 dist。
`WASM_BINDGEN` 选择绑定工具；`MINIGO_BUILD_STD=1` 使用 rust-src 和 Cargo 的 build-std。

接入、部署及 tarball 使用见 [runtime-wasm README](playground/runtime-rust/runtime-wasm/README.md)。
WASM 改动还应在目标配置下执行 Clippy，并检查原生生命周期回归。

## RPC 实现维护

接口与资源用法见 [RPC.md](RPC.md)。变更按所属契约核对：

| 变更 | 核对重点 |
| --- | --- |
| MRPC schema / generator | 类型、命名、契约身份、三语言 codec 与输出回滚 |
| FFI / Result | 接收或丢弃、迟到回复、配额与资源回收 |
| Endpoint | wire、操作状态、租期授权、控制分片与断线终态 |
| Router / publication | 原子替换、旧 lease、重连与关闭等待 |

验证重点是消息顺序、背压与清理终态：接纳确认先于业务结果，取消不越过完整请求；
续租确认关联已发送批次，过期授权不能恢复。资源关闭失败仍保留清理责任与额度，
迟到结果只回收本次新资源，等待取消不丢失已经开始的清理。

MRPC、FFI envelope 和 Endpoint framing 的身份由各自源码定义。
共享协议输入与更新入口见 [RPC 数据说明](testdata/rpc/README.md)。

## 缓存与性能诊断

持久缓存可通过 `MINIGO_CACHE` 设置，完整配置见
[编译缓存](USAGE.md#编译缓存)。常用诊断命令：

```bash
go run ./cmd/mini-go cache inspect
go run ./cmd/mini-go cache verify
MINIGO_DEBUG=cachetrace=1 go run ./cmd/mini-go check main.mgo
MINIGO_DEBUG=cachehash=1,cacheverify=1 go run ./cmd/mini-go check main.mgo
```

`cacheverify` 在命中后重建并比较结果。分析缓存命中时，结合 trace 和 profile 检查实际执行的阶段。

日常验证保留缓存；`make cache-clean` 清理 Mini-Go 缓存，`make clean` 还删除根目录的
`bin/`、`.cache/`、`build/`，并清理 Go test/fuzz 缓存。Go build cache、Rust target、
SDK 的 node_modules/dist 与生成镜像保留，可按所属工具和目录单独清理。

性能测量使用 Go benchmark/pprof 或 Rust 基准：

```bash
go test ./runtime -run '^$' -bench 'Benchmark(VM|RevisionRetention)' -benchmem
make runtime-rust-bench
```

固定工作负载与构建参数，串行保留多次采样。消融一次只改变一个机制，并独立验证行为、
计费与回收；单条路径的收益不代表整体性能。调查报告和原始结果保存在 `/tmp`。

引用遍历改动同时检查地址别名、共享 backing 去重与观察过程不初始化值。
`go test ./runtime -run '^$' -bench '^BenchmarkByteArrayPointerCensus$' -benchmem`
用于比较字节数组指针计费的宿主分配；逻辑字节数由对应行为测试独立验证。

长期运行验证使用固定工作集，预热后重复调用、取消与热更新，观察存活资源是否稳定。
分别记录 VM 逻辑费用、保留版本、宿主内存和连接等系统资源；WASM 另记线性内存容量。
累计分配不能代替存活内存，短时测试也不能证明任意长时间运行可靠。
可运行的有限宿主循环见 [Rust 长期宿主](playground/runtime-rust/USAGE.md#长期宿主)。

## Fuzz 与自举

```bash
FUZZTIME=30s make fuzz-syntax
FUZZTIME=30s make fuzz-runtime
make bootstrap-test
```

普通测试运行固定 fuzz seeds；持续变异同时验证成功不变量和失败后的状态。
语法组覆盖前端与工具，runtime 组覆盖执行、加载、协议与资源；稳定复现样本纳入所属 corpus。

Go 自举经源码适配器比较原生与 VM compiler 的镜像、hash 和诊断，属于较重验证。
Rust 常规测试直接使用宿主预编译镜像。

## 编辑器扩展

在 `vscode-ext/` 执行 `npm ci`、`npm test` 和 `npm run package`，
产物为 `/tmp/mini-go.vsix`。grammar 由根生成流程维护，
用户配置见 [扩展 README](vscode-ext/README.md)。
