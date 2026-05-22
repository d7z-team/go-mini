# Go-Mini 使用文档

`go-mini` 是一个 Go 风格脚本引擎。

- 编译阶段生成稳定 `go-mini-bytecode`
- `core/gofrontend` 负责 Go source / Go AST 到 Mini AST 的前端转换
- `core/lowering` 负责 AST 到 `PreparedProgram` 的唯一转换
- 运行时执行 lowered task plan / prepared program
- FFI 通过 `ffigen` 生成 schema-only 桥接代码
- 调用模板在 compiler 阶段展开成真实代码，bytecode / runtime 不保留模板执行逻辑
- 执行入口统一落在 `Artifact` / bytecode，runtime 只消费 prepared executable，AST 只保留在编译、分析和调试边界
- canonical type 文本由 `core/typespec` 统一实现；AST 前端通过 `core/ast/ast_types.go` 使用，runtime/VM 通过 `core/runtime/schema.go` 使用

仓库采用多模块布局：

- `gopkg.d7z.net/go-mini/core`: 核心引擎、compiler、runtime、LSP core、FFI wire、`core/cmd/ffigen` 和纯原生类型 `core/ffilib` 标准库子集
- `gopkg.d7z.net/go-mini/ffilib`: 完整标准库 FFI 装配，以及 io/os/time/fmt/image 等外层标准库模块
- `gopkg.d7z.net/go-mini/examples`: 示例脚本和 `examples/cmd/exec`、`examples/cmd/lsp-server`

日常开发通过 root `go.work` 联动本地模块；`ffilib` 的 `core` 依赖版本只表示最低兼容版本，实际项目可同时依赖更高版本的 `core`。

## 1. 基础执行

```go
executor := engine.NewMiniExecutor()
ffilib.RegisterAll(executor)

program, err := executor.NewRuntimeByGoCode(`
package main
import "fmt"
func main() {
    fmt.Println("hello")
}
`)
if err != nil {
    panic(err)
}
if err := program.Execute(context.Background()); err != nil {
    panic(err)
}
```

`core` 默认提供核心引擎，以及 `errors`、`strings`、`strconv`、`math`、`sort` 这类纯原生值类型标准库 FFI。若需要 io/os/time/fmt/image 等完整标准库 FFI，再调用顶层 `ffilib.RegisterAll`。

`NewRuntimeByGoCode` 是便捷入口，内部仍会先 `CompileGoCode(...)`，再从编译产物创建运行时；对外持久化、跨进程传输和正式装载统一推荐使用 bytecode。

如果你需要持久化或跨进程传输，可以直接使用 bytecode：

```go
compiled, err := executor.CompileGoCode(`package main`)
if err != nil {
    panic(err)
}

payload, err := compiled.MarshalBytecodeJSON()
if err != nil {
    panic(err)
}

loaded, err := executor.NewRuntimeByBytecodeJSON(payload)
if err != nil {
    panic(err)
}
_ = loaded
```

## 2. 编程接口

### 2.0 Canonical Type

Mini AST、lowering、compiler、bytecode 和 runtime 只接受 canonical type。常见格式包括：

- `Array<T>`
- `Map<K, V>`
- `Ptr<T>`
- `HostRef<T>`
- `tuple(A, B)`
- `function(A, B) R`
- `interface{Read(TypeBytes) tuple(Int64, Error);}`
- `struct { Name String; }`

Go 风格输入如 `[]int`、`*T`、`map[string]int`、`interface{}` 只允许出现在 Go 前端，必须在 `core/gofrontend` 转换阶段立即规范化。手写 AST、JSON AST、bytecode 和 FFI schema 不做兼容修复。

### 2.1 编译为 Artifact

```go
executor := engine.NewMiniExecutor()

artifact, err := executor.CompileGoCode(`
package main
func main() {}
`)
if err != nil {
    panic(err)
}
```

`Artifact` 是编译产物，包含：

- `Program`: 源码编译时保留的 AST，用于 LSP / debugger / 分析；从 bytecode 装载的 artifact 不重建该字段
- `Bytecode`: 稳定 `go-mini-bytecode`，其中 `Executable` 是运行装载所需的 prepared program

### 2.2 直接编译为 bytecode.Program

```go
bytecodeProgram, err := executor.CompileGoCodeToBytecode(`
package main
func main() {}
`)
if err != nil {
    panic(err)
}
```

如果你已经有文件名，也可以：

```go
bytecodeProgram, err := executor.CompileGoFileToBytecode("script.mgo", source)
```

### 2.3 从 MiniProgram 取出 bytecode

```go
program, err := executor.NewRuntimeByGoCode(`
package main
func main() {}
`)
if err != nil {
    panic(err)
}

bytecodeProgram, err := program.Bytecode()
if err != nil {
    panic(err)
}
_ = bytecodeProgram
```

### 2.4 bytecode JSON 互转

```go
program, err := executor.NewRuntimeByGoCode(source)
if err != nil {
    panic(err)
}

payload, err := program.MarshalBytecodeJSON()
if err != nil {
    panic(err)
}

loaded, err := executor.NewRuntimeByBytecodeJSON(payload)
if err != nil {
    panic(err)
}
_ = loaded
```

如果你只想把 JSON 恢复成编译产物，而不是立刻执行：

```go
artifact, err := executor.ArtifactFromBytecodeJSON(payload)
if err != nil {
    panic(err)
}
_ = artifact
```

bytecode JSON 装载执行只依赖 `Executable`。如果 payload 缺少 executable prepared program，运行时装载会拒绝；不会通过展示指令或旧元数据重建 AST 后执行。

### 2.5 推荐的互转路径

- 源码编译：`CompileGoCode`
- 已有 AST 编译：`CompileProgram`
- 源码便捷执行：`NewRuntimeByGoCode`
- 正式装载执行：`NewRuntimeByCompiled` 或 `NewRuntimeByBytecodeJSON`
- 源码转 bytecode：`CompileGoCodeToBytecode`
- 程序导出：`MarshalBytecodeJSON`
- bytecode JSON 恢复编译产物：`ArtifactFromBytecodeJSON`

### 2.6 AST 分析入口

如果你需要 tolerant 语义分析、LSP 或调试辅助信息，而不是直接执行 AST：

```go
program, diags := executor.AnalyzeProgramTolerant(astProgram, nil)
if len(diags) > 0 {
    // 处理诊断
}
_ = program
```

`AnalyzeProgramTolerant(...)` 只用于分析边界，不是运行时装载口。执行仍应先编译成 `Artifact` 或 bytecode。第二个参数可以传入 `map[uri]source`，用于 LSP 模板 hover 这类必须保留源码切片的展示；不需要源码展示时传 `nil`。

### 2.7 调用模板

调用模板用于把源码中的虚拟函数调用在 compiler 首次语义检查后、AST 优化前展开成真实代码调用。顶层 `ffilib.RegisterAll` 会注册标准库 FFI 和 `print(...)` / `println(...)` 模板，它们会展开为 `fmt.Print(...)` / `fmt.Println(...)`。模板只参与编译期校验、补全和 AST 展开；展开完成后，lowering、bytecode 和 runtime 只看到真实函数调用。

自定义模板的最短使用方式是在执行器上注册 `calltemplate.FunctionTemplate`，然后在脚本里像调用普通函数一样调用模板名。下面示例借用 `ffilib.RegisterAll` 提供 `fmt` 包；如果模板体引用自定义包，需要先注册对应 FFI/schema：

```go
executor := engine.NewMiniExecutor()
ffilib.RegisterAll(executor)

err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
    ID:        "demo.trace",
    Name:      "trace",
    SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, true, runtime.SpecAny),
    Body:      `{{ pkg "fmt" }}.Println({{ args }})`,
})
if err != nil {
    panic(err)
}

program, err := executor.NewRuntimeByGoCode(`
package main

func main() {
    trace("user", 7)
}
`)
```

也可以注册包成员模板，把不存在的包当作 compile-only facade 使用：

```go
err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
    ID:          "audit.Log",
    PackagePath: "audit",
    Name:        "Log",
    SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, true, runtime.SpecAny),
    Body:        `{{ pkg "fmt" }}.Println({{ args }})`,
})
```

脚本中按包成员调用即可：

```go
package main

import "audit"

func main() {
    audit.Log("created", 1001)
}
```

如果 `audit` 没有真实模块，compiler 只把它用于识别模板调用；模板展开后该 import 会从可执行产物里移除。如果 `audit` 是真实包，则实际使用 `audit.Log(...)` 时会校验真实成员签名和 `SourceSig` 一致。

模板入口有两类：

- 全局函数模板：例如 `println(...)`，可以在任意位置调用；源码中不能声明同名函数、变量、参数或 import alias。
- 包成员模板：例如 `aaa.BBB(...)`，调用会在展开后替换成模板生成的真实代码；实际使用模板时，如果 `PackagePath` 对应真实包，compiler 会校验真实成员签名一致，如果包不存在则推导为 compile-only facade，展开后移除对应 import，并由最终无模板语义检查确认没有残留依赖。

模板体使用 Go `text/template` 渲染，常用 helper：

- `pkg "fmt"`：取得卫生的内部包 alias，并自动加入真实 import；参数必须是精确的完整包路径字符串字面量。
- `args`：展开全部调用参数，适合转发可变参数。
- `arg 0`：展开第 0 个参数占位符，不附带 `...`。
- `callArg 0`：展开第 0 个参数占位符；如果它是可变参数调用的最后一个参数，会保留 `...`。
- `argc` / `ellipsis`：读取参数数量和调用点是否使用 `...`。
- `fresh "name"`：生成稳定且不会撞用户代码的临时标识符。

模板不再单独声明 imports。`pkg` 包存在性在模板实际渲染时校验，因此未使用的模板不会污染无关编译。模板参数以 AST 占位符方式回填，不允许通过 `.Args` 等模板数据对象访问参数，也不把参数重新格式化为 Go 源码。模板展开是 fixed-point：模板生成的代码如果继续调用模板，会递归展开直到只剩真实代码；递归模板链会在编译期报错。模板体形态由使用位置和渲染结果推导：表达式位置必须渲染成表达式；语句列表位置先按表达式解析，失败且源码签名返回 `Void` 时再按语句列表解析；`defer` / `go` 中的模板必须最终展开为单个 call expression。

LSP hover 会展示模板的最终渲染视图。展示参数来自调用点源码切片，`pkg` 以 `import "fmt"` 和 `fmt.Println(...)` 这类用户可读形式显示，不暴露 `__gomini_tpl_` 内部 alias；这只是 IDE 展示，实际 compiler 仍使用卫生 alias 和 AST 占位符完成展开。

模板注册会拒绝覆盖真实内置函数、FFI 函数、常量、结构体和接口；源码也不能声明全局模板同名标识符。`__gomini_tpl_` 前缀保留给模板展开器，用户源码不能声明该前缀。

## 3. CLI

`examples/cmd/exec` 使用 bytecode-first 模型：

```bash
# 执行源码
mini-exec -run script.mgo

# 只编译并输出 bytecode JSON
mini-exec -o script.json script.mgo

# 反汇编源码编译结果
mini-exec -d script.mgo

# 从 bytecode 执行
mini-exec -bytecode script.json
```

## 4. 安全与沙盒

### 指令步数限制

```go
program.SetStepLimit(10000)
```

### 指针语义

脚本支持 `new(T)` 和 `*p`，但指针本质是受控 VM 引用，不是宿主裸地址。

```go
p := new(Int64)
*p = 123
println(*p)
```

### 值与引用语义

运行时变量统一存放在 slot 中。slot 持有声明类型，赋值时把右侧值规范化后写入 slot，而不是替换掉变量的类型身份。

值语义：

- primitive、`TypeBytes` 和 VM struct 按值复制。
- struct composite literal 生成真实 VM struct，不再退化为 map。
- struct 字段本身也是 typed slot；函数参数、返回值和 value receiver 会复制 struct 的字段 slot。

引用语义：

- array、map、`Ptr<T>`、`HostRef<T>`、closure、module 和 interface 内部目标仍共享底层对象。
- `Ptr<T>` 指向 VM slot；`*p = v` 会写回目标 slot，并继续执行声明类型校验。
- 闭包 capture 共享同一个 slot，因此子 VM 执行上下文可以修改父作用域捕获变量。

map 与 struct 是不同运行时类型。map key 会保留 primitive key 类型，`Map<Int64, String>{1: "a"}` 与 `Map<String, String>{"1": "a"}` 不会在运行时混淆。

## 4.1 VM 执行上下文调度语义

当前并发模型只有一类 VM 原语：`go f()`。它会创建子 VM 执行上下文，但整个 VM 始终单线程执行；所谓并发只是 VM 调度器在内部 safe point 或异步 FFI completion 处切换执行上下文。

### 基本用法

VM 侧不暴露公开 yield API。需要等待外部事件或让出执行时，应通过异步 FFI；标准库 `time.Sleep(ns)` 就是异步 FFI，返回值类型仍是 `Void`。

```go
package main

import "time"

var result = 0

func work() {
    result = 42
}

func main() {
    go work()
    time.Sleep(1)
    if result != 42 {
        panic("worker did not run")
    }
}
```

```go
package main

import "time"

var done = false

func worker() {
    time.Sleep(10000000)
    done = true
}

func main() {
    go worker()
    time.Sleep(20000000)
    if !done {
        panic("worker did not finish")
    }
}
```

失败语义：

- 子执行上下文的 panic 会让整个 VM 执行失败，除非在子上下文内部 recover
- `go f()` 没有返回值，父流程需要通过共享状态或显式同步协议观察结果
- 需要把结果带回父流程时，使用共享变量、显式 channel 类库（未来能力）或其它同步协议

### Root 生命周期

root `main` 是整个程序的根执行上下文。

- `main()` 正常返回时，所有未完成子执行上下文会立即停止
- `main()` panic 返回时，同样会停止所有未完成子执行上下文
- runtime 不会在退出阶段自动等待后台执行上下文收尾

这意味着 `go f()` 的默认语义是 fire-and-forget，但不是“main 退出后继续后台存活”。

### 结果共享

因为 VM 永远单线程，闭包 capture、VM array/map、VM pointer 和 host handle 都按普通引用语义共享；不会出现并行数据竞争，但仍需要明确调度点，否则子执行上下文可能没有机会运行。

示例：

```go
package main

import "time"

func main() {
	result := 0
	go func(x Int64) {
		result = x * 2
	}(21)
	time.Sleep(1)
	if result != 42 {
		panic("unexpected result")
	}
}
```

### 当前限制

- 没有 `chan/select` 语义
- 同步 FFI 调用会阻塞整个 VM；只有返回 `ffigo.Async[T]` 的异步 FFI 会挂起当前执行上下文并在 completion 时恢复

### 使用警告

- `go f()` 不代表宿主 goroutine，也不提供并行执行。
- 没有异步 completion 或内部 safe point 时，root `main` 可能直接返回并停止尚未运行的子执行上下文。
- 子执行上下文的 panic 默认会失败整个 VM；需要隔离失败时在子上下文内部使用 `try/recover`。

## 5. FFI 生成器

`ffigen` 负责把 Go 接口或结构体导出为 schema-only FFI 桥接代码；CLI 入口在 `core/cmd/ffigen`，生成器核心在 `core/ffigen`。

### 5.1 参数模型

现在只保留两个参数：

```bash
go run gopkg.d7z.net/go-mini/core/cmd/ffigen -pkg orderlib -out order_ffigen.go interface.go
```

- `-pkg`: 生成文件的 Go 包名
- `-out`: 输出文件

命令行参数只包含：

- `-pkg`
- `-out`

### 5.2 目录模式与文件模式

`ffigen` 现在分两种模式。

#### 目录模式

输入是一个目录，`-out` 也必须是目录：

```bash
go run gopkg.d7z.net/go-mini/core/cmd/ffigen -pkg orderlib -out ./gen ./
```

目录模式行为：

- 按整个 Go package 处理
- 输出文件名固定为 `ffigen_<pkg>.go`
- 自动跳过已有 `ffigen_*.go`
- 包内 `ffigen:module` 最多只能有一个
- 适合正式生成

#### 文件模式

输入是一个或多个 `.go` 文件，`-out` 是输出文件：

```bash
go run gopkg.d7z.net/go-mini/core/cmd/ffigen -pkg orderlib -out order_ffigen.go interface.go
```

文件模式行为：

- 维持单文件/多文件生成习惯
- 适合局部样例和历史用法
- 不允许抢占目录模式的保留文件名 `ffigen_<pkg>.go`

### 5.3 注释约定

```go
// ffigen:module order
type OrderAPI interface {
    New(id string) (*Order, error)
}

// ffigen:methods
type Page struct {
    URL String
}

// ffigen:module io
// ffigen:interface
type Reader interface {
    Read(buf []byte) (int64, error)
}
```

- `ffigen:module <name>`
  定义 VM 暴露的模块名。
- `ffigen:methods [prefix]`
  只用于 `struct`，表示导出该结构体的方法集。
- `ffigen:interface`
  只用于命名 `interface`，表示额外导出一个 VM 命名接口 schema。

### 5.4 命名规则

VM 可见类型名以 `ffigen:module` 为准：

- 本地类型：`order.Order`
- 跨模块类型：优先解析为对方模块名，例如 `io.File`
- 完整 Go import path 不会暴露到 `Ptr<T>`、struct schema、方法前缀中

### 5.5 导出模式

#### 接口导出

```go
// ffigen:module browser
type Browser interface {
    NewPage() (*Page, error)
}
```

这类是标准 FFI service interface，会生成完整的：

- proxy
- host router
- bridge
- `RegisterXxx(...)`

如果输入是文件模式，普通命名 `interface` 也会按历史行为生成完整 FFI target。

#### 命名接口 schema 导出

```go
// ffigen:module io
// ffigen:interface
type Reader interface {
    Read(buf []byte) (int64, error)
}
```

这类只生成：

- `RuntimeInterfaceSpec`
- `RegisterXxxSchema(...)`

不会生成 proxy/router/bridge。当前主要用于把宿主命名接口暴露给 VM 类型系统，例如 `io.Reader`、`io.Writer`。

目录模式下，只有显式标记 `ffigen:interface` 的命名接口才会按 schema-only 方式导出；未标记的普通命名接口会被跳过。

#### 结构体方法集导出

```go
// ffigen:methods
type Calculator struct {
    Base Int64
}
```

结构体上只写 `ffigen:methods` 时，默认使用结构体名作为方法集前缀。

这类会生成结构体方法路由和对应 struct schema，不属于 `ffigen:interface`。

### 5.6 Struct ownership

FFI struct schema 有两类 ownership：

- `VMValue`: 普通值类型 struct。VM 可以创建 zero value、composite literal 和 `new(T)`，FFI wire 使用 struct payload 传递字段。
- `HostOpaque`: opaque host type。VM 只能持有 `HostRef<T>`，不能创建 `T{}`、`var x T`、`new(T)`，也不能把直接包含 opaque value 的类型作为 VM 值创建。

`HostOpaque` 对象只能来自 FFI 工厂函数或 FFI 返回值。例如 `sync.WaitGroup` 必须通过 `sync.NewWaitGroup()` 获得，不能写 `sync.WaitGroup{}`。

`ffigen` 生成 schema 时会把普通值 struct 标记为 `VMValue`，把通过 `HostRef<T>` 暴露的宿主对象标记为 `HostOpaque`。同一个 Go 类型不能同时作为 VM value struct 和 host opaque reference 暴露。

### 5.7 注册

生成后直接调用 `RegisterXXX`：

```go
executor := engine.NewMiniExecutor()
registry := executor.HandleRegistry()

orderlib.RegisterOrderAPI(executor, impl, registry)
```

## 6. Interface

`go-mini` 支持命名接口、匿名接口、接口嵌入、类型断言和 type switch。`ffigen` 当前只生成宿主到 VM 的 schema/proxy/router，不再提供反向代理生成功能。

## 7. LSP / IDE

LSP 和查询能力建立在源码 AST 之上，执行主路径使用 compiled artifact / prepared program。常用 API：

```go
hover := program.GetHoverAt(10, 5)
refs := program.GetReferencesAt(10, 5, true)
def := program.GetDefinitionAt(10, 5)
```

详细集成方式见 [LSP.md](./LSP.md)。
