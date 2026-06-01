# Go-Mini 使用文档

`go-mini` 是一个 Go 风格脚本引擎。

- 编译阶段生成稳定 `go-mini-bytecode`
- 运行时只装载和执行 bytecode
- Go 源码、其他语言前端和纯 VM 源码库都会在编译阶段进入同一套 bytecode 产物
- FFI 通过 `ffigen` 生成 schema-only 桥接代码
- FFI schema 冲突判断由 runtime 统一实现，compiler 与 runtime 注册路径共享同一套一致性规则
- 对外扩展通过 `executor.UseSurface(...)` 装配 FFI schema/bind、模板和纯 VM 源码库
- `reflect` 基于 Go-Mini runtime / FFI schema metadata，不调用 Go 原生 `reflect` API
- 调用模板在编译阶段展开成真实代码
- surface 可以携带纯 VM 源码库；编译后的 bytecode 可独立装载已使用的 VM 源码模块
- 执行入口统一落在 `ExecutableArtifact` / bytecode；源码分析使用 `AnalysisProgram`
- 类型文本统一使用 canonical type
- 运算符重载按普通方法调用语义编译
- 命名常量按声明值求值，不作为 runtime 变量加载；局部变量可以遮蔽同名常量，常量不能作为赋值目标
- `Any` 不是静态通配符；VM 语言层 `Any` 使用动态值 wrapper，FFI `Any` wire 只接受纯值数据
- VM 可见 `Error` 直接承载 Go `error`，VM 创建的 error 会附带 VM stack，FFI host error 会保留 host error chain
- 语言级 channel/select 编译为 bytecode 执行，保持单线程协作式 VM 调度
- 异步 FFI 必须暴露等待来源；所有 VM 执行上下文只剩互相等待时会返回明确的 all-blocked 错误
- 每次 run 都挂一个 `RunController`；独立 pause/resume、debugger breakpoint pause 和 continue/step 都绑定当前 `RunHandle`
- Debugger 事件只在 VM 已进入 `Paused` 后投递；单步状态按 run ID 绑定并在 run 结束时清理，事件投递不会反向阻塞 VM 进入暂停点
- 调度器只报告 runnable / external-idle / all-blocked / done 快照；最终的 pause / resume / all-blocked 判定由 runtime 主循环统一仲裁
- VM timer 通过独立 `VMClock` / `VMTimer` 提供，pause 时冻结 `time.Sleep` 这类脚本等待；`time.Now` / `Since` / `Until`、`context.WithTimeout` 和 `context.WithDeadline` 继续使用真实时间

仓库采用多模块布局：

- `gopkg.d7z.net/go-mini/core`: 核心引擎、compiler、runtime、LSP core、FFI wire、`core/cmd/ffigen` 和 `core/ffilib` 默认标准库子集
- `gopkg.d7z.net/go-mini/ffilib`: 完整标准库 FFI 装配，以及 io/os/time/context/fmt/image 等外层标准库模块
- `gopkg.d7z.net/go-mini/examples`: 示例脚本和 `examples/cmd/exec`、`examples/cmd/lsp-server`

日常开发通过 root `go.work` 联动本地模块；`ffilib` 的 `core` 依赖版本表示最低支持版本，实际项目可同时依赖更高版本的 `core`。

## 1. 基础执行

```go
executor, err := engine.NewMiniExecutor()
if err != nil {
    panic(err)
}
if err := executor.UseSurface(ffilib.Surface()); err != nil {
    panic(err)
}

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

`core` 默认提供核心引擎、native `errors` / `fmt.Errorf` / `reflect`，以及 `strings`、`strconv`、`math`、`sort` 这类纯原生值类型标准库 FFI。若需要 io/os/time/context/fmt/image 等完整标准库 FFI，再通过 `executor.UseSurface(ffilib.Surface())` 装配顶层 surface。

`NewRuntimeByGoCode` 是便捷入口；对外持久化、跨进程传输和正式装载推荐使用 bytecode。

如果你需要运行中控制暂停/恢复，可以使用 `Start(...)`：

```go
run, err := program.Start(context.Background())
if err != nil {
    panic(err)
}
if err := run.Pause(runtime.PauseReason{Kind: "manual"}); err != nil {
    panic(err)
}
if err := run.Resume(); err != nil {
    panic(err)
}
if err := run.Wait(); err != nil {
    panic(err)
}
```

宿主 `context.Context` 仍然是 run 的硬取消边界；pause 冻结的是 VM 等待型时间语义，而不是宿主请求超时、`context` deadline 或 `time.Now` 这类真实时间观测。

如果你启用了 debugger，会话对象只负责断点、按 run ID 绑定的单步策略和可取消事件拉取；恢复执行和单步都通过当前 run handle 完成。断点/单步事件只会在 `RunController` 已进入 `Paused` 后投递，因此收到事件时可以安全读取暂停状态并决定 `Continue()` 或 `StepInto()`。

```go
dbg := debugger.NewSession()
dbg.AddBreakpoint(10)
ctx := debugger.WithDebugger(context.Background(), dbg)

run, err := program.Start(ctx)
if err != nil {
    panic(err)
}

event, err := dbg.NextEvent(ctx)
if err != nil {
    panic(err)
}
_ = event

if err := run.StepInto(); err != nil {
    panic(err)
}
if err := run.Continue(); err != nil {
    panic(err)
}
```

对无 package 声明的 snippet，`MiniExecutor.StartExecute(...)` 提供同样的 run-handle 控制入口。

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

源码、bytecode 和 FFI schema 只接受 canonical type。常见格式包括：

- `Array<T>`
- `Map<K, V>`
- `Chan<T>` / `RecvChan<T>` / `SendChan<T>`
- `Ptr<T>`
- `HostRef<T>`
- `tuple(A, B)`
- `function(A, B) R`
- `interface{Read(TypeBytes) tuple(Int64, Error);}`
- `struct { Name String; }`

Go 风格输入如 `[]int`、`*T`、`map[string]int`、`interface{}` 会在 Go 前端转换阶段立即规范化；标量 `rune` 和 `byte` 都会归一到 `Int64`，字符字面量如 `'A'` / `'\n'` / `'\xff'` / `'你'` 也是 `Int64` code point。`[]byte` 归一为 `TypeBytes`，`[]rune` 归一为 `Array<Int64>`。其他语言前端应实现 `core/frontend.Frontend` 并输出 canonical type；执行装载只接受 bytecode。

公开 FFI schema 使用具体 `HostRef<T>` 或明确的 typed interface schema 表达 host identity。`Ptr<T>` 表示 VM slot 引用；channel 通过 `Chan<T>` / `RecvChan<T>` / `SendChan<T>` 作为 schema endpoint 暴露。`Any` 在 VM 语言层是动态值 wrapper，用于保存 nil 身份和真实动态值；它不是类型系统通配符，`Any` 赋给具体类型需要类型断言或显式转换路径。

FFI `Any` wire 只面向 primitive、bytes、array、map 和 VM value struct 这类纯值数据。VM pointer、`HostRef<T>`、channel、module、host error/interface handle 不允许进入 VM `Any` 或 FFI `Any`；一等函数、闭包和接口身份也不会被 FFI `Any` 编码。FFI 返回值按 route schema 从 wire 解码，不反射解构任意 Go host struct、slice、array 或 pointer。

运算和比较遵循统一类型门禁：数值之间可做数值运算和比较，`String` 只和 `String` 做拼接与有序比较，`TypeBytes` 只和 `TypeBytes` 拼接，`%` / 位运算 / 位移只接受 `Int64`。不同 primitive 类型不会通过字符串化或 `Any` 通配自动比较；例如 `10 == "10"` 是编译错误，不会求值为 `true` 或 `false`。动态 `Any` 参与比较时会在运行时展开真实值并继续执行同一套规则，类型不匹配会返回 VM 错误。

当原生运算不支持时，compiler 会尝试解析左操作数上的运算符方法，并把表达式改写为普通方法调用，例如 `a + b` -> `a.OpAdd(b)`、`-a` -> `a.OpNeg()`。支持的方法名包括 `OpAdd`、`OpSub`、`OpMul`、`OpDiv`、`OpMod`、`OpBitAnd`、`OpBitOr`、`OpBitXor`、`OpLsh`、`OpRsh`、`OpEq`、`OpNeq`、`OpLt`、`OpLe`、`OpGt`、`OpGe`、`OpNeg`、`OpPos`、`OpNot` 和 `OpBitNot`。比较运算和 `OpNot` 必须返回 `Bool`，所有重载方法都必须返回单个非 `Void` 值；`&&` / `||` 不参与重载，不做右操作数查找或交换律匹配。

函数返回支持 Go 风格 tuple 转发：如果当前函数声明多个返回值，`return other()` 可以直接转发另一个 tuple-return 函数或 FFI route 的结果。编译和执行都会按 tuple item 校验与赋值。

### 2.1 编译为 ExecutableArtifact

```go
executor, err := engine.NewMiniExecutor()
if err != nil {
    panic(err)
}

artifact, err := executor.CompileGoCode(`
package main
func main() {}
`)
if err != nil {
    panic(err)
}
```

`ExecutableArtifact` 是可执行编译产物，包含源码摘要和稳定的 `go-mini-bytecode`。

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

### 2.3 从 ExecutableProgram 取出 bytecode

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

bytecode JSON 可以直接恢复为可执行程序，也可以先恢复为 `ExecutableArtifact` 后再装载执行。无效或不完整的 bytecode 会在装载阶段返回错误。

### 2.5 推荐的互转路径

- 源码编译：`CompileGoCode`
- 其他语言前端编译：`CompileWithFrontend`
- 源码便捷执行：`NewRuntimeByGoCode`
- 正式装载执行：`NewRuntimeByArtifact` 或 `NewRuntimeByBytecodeJSON`
- 源码转 bytecode：`CompileGoCodeToBytecode`
- 程序导出：`MarshalBytecodeJSON`
- bytecode JSON 恢复编译产物：`ArtifactFromBytecodeJSON`

FFI、编译期模板和纯 VM 源码库都通过 `UseSurface(...)` 装配。VM 模块源码使用 `surface.Library(...)` 注册；编译后的 bytecode 会携带已使用的 VM 源码模块。

源码库和 FFI package 共享同一套 module namespace，FFI type-only schema 也会成为对应 FFI module 的 `type` member。`import` 只按精确 module path 解析；Go 源码里的默认 alias 仍来自 path 最后一段，但内部身份始终是完整 path，不做 suffix、短包名、slash/dot 互转或按最长已知 path 拆分的兜底。同一个 path 不能同时注册为源码库和 FFI package。

### 2.6 源码分析入口

如果你需要 tolerant 语义分析、LSP 或调试辅助信息，而不是直接执行：

```go
program, diags := executor.AnalyzeProgramTolerant(astProgram, nil)
if len(diags) > 0 {
    // 处理诊断
}
_ = program
```

`AnalyzeProgramTolerant(...)` 用于源码分析，不用于执行装载。第二个参数可以传入 `map[uri]source`，用于 LSP hover 等需要源码展示的场景；不需要源码展示时传 `nil`。

Go 源码的 tolerant 分析入口是：

```go
analysis, diags := executor.AnalyzeGoCodeTolerant(source)
_ = analysis
_ = diags
```

`CompileGoCode` / `CompileWithFrontend` 返回 `ExecutableArtifact`；`AnalyzeGoCodeTolerant` / `AnalyzeProgramTolerant` 返回 `AnalysisProgram`。

### 2.7 调用模板

调用模板用于为脚本提供编译期便捷函数。顶层 `ffilib.Surface()` 会注册常用模板，例如 `print(...)` / `println(...)`。

自定义模板的最短使用方式是在执行器上注册 `calltemplate.FunctionTemplate`，然后在脚本里像调用普通函数一样调用模板名。下面示例借用 `ffilib.Surface()` 提供 `fmt` 包；如果模板体引用自定义包，需要先注册对应 FFI/schema：

```go
executor, err := engine.NewMiniExecutor()
if err != nil {
    panic(err)
}
if err := executor.UseSurface(ffilib.Surface()); err != nil {
    panic(err)
}

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

也可以注册包成员模板：

```go
err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
    ID:          "audit.Log",
    PackagePath: "audit",
    Name:        "Log",
    SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, true, runtime.SpecAny),
    Body:        `{{ pkg "fmt" }}.Println({{ args }})`,
})
```

脚本中按包成员调用：

```go
package main

import "audit"

func main() {
    audit.Log("created", 1001)
}
```

模板体使用 Go `text/template` 渲染，常用 helper：

- `pkg "fmt"`：引用 Go-Mini 包并自动加入 import；参数必须是精确的完整包路径字符串字面量。
- `args`：展开全部调用参数，适合转发可变参数。
- `arg 0`：展开第 0 个参数占位符，不附带 `...`。
- `callArg 0`：展开第 0 个参数占位符；如果它是可变参数调用的最后一个参数，会保留 `...`。
- `argc` / `ellipsis`：读取参数数量和调用点是否使用 `...`。
- `fresh "name"`：生成稳定且不会撞用户代码的临时标识符。

模板体应渲染为当前调用位置可接受的表达式、语句或调用。递归模板链会在编译时报错。

### 2.8 Surface VM 源码库

`surface.Bundle` 除了 FFI schema、bind 和模板，也可以携带纯 VM 源码模块。源码库适合用 Go-Mini 代码实现可复用模块。

```go
bundle := surface.Merge(
    surface.Library("mathx", surface.GoFile("mathx.mgo", `
package mathx

func Double(v int) int {
    return v * 2
}
`)),
)

executor, err := engine.NewMiniExecutor()
if err != nil {
    panic(err)
}
if err := executor.UseSurface(bundle); err != nil {
    panic(err)
}
```

脚本按普通 import 使用：

```go
package main

import "mathx"

func main() {
    if mathx.Double(21) != 42 {
        panic("bad math")
    }
}
```

源码库 path 必须唯一，且源码中的 `package` 声明必须匹配 path 最后一段，最后一段必须是可作为 Go package 的短标识符。例如 `surface.Library("example/mathx", ...)` 内部源码应声明 `package mathx`。源码库不能和 FFI package 或 FFI type-only module 使用同一个 path；需要隐藏 FFI 细节时，使用独立内部 FFI module path 再由源码库封装。

源码库对外暴露 Go 风格 exported identifier，也就是 ASCII 大写字母开头的函数、变量、常量、类型、结构体和接口。小写 helper 只在库源码内可见。编译后的 bytecode 会携带已使用的源码库，并在 `ModuleRequirements` 中记录 module hash，因此 bytecode JSON 可以在新的 executor 中直接装载执行；FFI 依赖仍需要通过 `UseSurface(...)` 提供。

### 2.9 Error 语义

VM 的 `Error` 是直接存放在 `TypeError` 中的 Go `error`。`errors.New(...)`、`fmt.Errorf(...)`、`panic(...)` 和 runtime 自身创建的可捕获错误会包装为 `VMStackError`，记录创建点 VM stack；`errors.Stack(err)` 返回这段 VM stack 文本，没有 VM stack 的 host error 返回空字符串。

```go
package main

import "errors"
import "fmt"

func main() {
    root := errors.New("root")
    wrapped := fmt.Errorf("outer: %w", root)

    if !errors.Is(wrapped, root) {
        panic("missing wrap")
    }
    if errors.Unwrap(wrapped).Error() != "root" {
        panic("bad unwrap")
    }

    var out error
    if errors.As(wrapped, &out) {
        _ = out.Error()
    }
}
```

`fmt.Errorf` 复用 Go 的 `%w` 规则。单个 `%w` 可通过 `errors.Unwrap` 取得下一层；多个 `%w` 会形成 Go join-style wrapping，`errors.Is` 会遍历匹配，`errors.Unwrap` 与 Go 标准库一致返回 `nil`。`errors.As` 使用 Go-like 写法：先声明目标变量，再传入 `&target`。VM 侧目标使用 `Error` / `Any` slot 表达。

FFI 返回的 Go `error` 会进入 `VMHostError` 包装。包装会保留 host handle/bridge identity，并在可解析时保留原始 Go error chain，因此 VM 内的 `errors.Is(hostWrapped, hostTarget)` 会走 Go `errors.Is` 语义。VM 创建的 error 另有 runtime-local identity，error equality 与 map key 不依赖 Go 反射 comparable 规则。跨 FFI 传递 error 时在 schema 中显式声明 `Error`。

### 2.10 Reflect

`reflect` 是 core 默认注册的 native 包，面向 VM struct 序列化/反序列化和 FFI schema 查看。它只读取 Go-Mini compiler/lowering/runtime 已注册的 metadata；FFI 函数、HostRef 类型和包成员之所以可见，是因为 `ffigen` 或 surface schema 显式注册了 route/type/package 信息，不来自 Go 原生反射。

`Package`、`Packages`、`Members` 和 `MemberByName` 读取统一 runtime module registry，因此会同时看到已装载的源码库模块、FFI package 和 type-only FFI schema。源码库成员来自 `PreparedProgram.Exports`，FFI 成员来自 surface schema；二者使用同一个精确 module path namespace，FFI type owner 由 schema 的 package path 和 member name 显式给出。

当 `reflect.Package("pkg")`、`reflect.TypeFrom("...")`、`reflect.Zero("...")` 或 `reflect.MakeMap("...")` 使用编译期字符串字面量时，compiler 会为已知 FFI package/type 记录 bytecode module requirement。动态字符串 lookup 仍然是运行期 metadata 查询，不会隐式装载源码库，也不会让 `Packages()` 把所有潜在模块变成执行依赖。

常用 API：

- `TypeOf(v) Type`、`TypeFrom(typeName) (Type, Bool)`、`KindOf(v) Int64`、`KindOfType(t) Int64`
- `Fields(v) []StructField`、`FieldsOfType(t) []StructField`、`Field(v, name) (Any, Bool)`、`SetField(v, name, value) Error`
- `Zero(typeName) Any`：返回可写入的 pure-Any zero value，便于按字段填充；不会返回 VM pointer
- `Len(v) Int64`、`Index(v, i) (Any, Bool)`、`MapKeys(v) ([]Any, Bool)`、`MapIndex(v, key) (Any, Bool)`、`MakeMap(typeName) (Any, Bool)`、`SetMapIndex(v, key, value) Error`、`Unwrap(v) (Any, Bool)`、`Assign(target, value) Error`、`Append(target, value) Error`
- `Methods(v) []Method`、`MethodsOfType(t) []Method`
- `IsNil`、`IsStruct`、`IsPtr`、`IsHostRef`、`IsChan`、`IsFunc`、`IsFFIFunc`、`IsVMFunc`、`IsNativeFunc`
- `Package(path) (PackageInfo, Bool)`、`Packages() []PackageInfo`、`Members(p) []Member`、`MemberByName(p, name) (Member, Bool)`

`reflect.Type` 提供 Go-like 方法：`String`、`Kind`、`Name`、`PkgPath`、`Elem`、`Key`、`AssignableTo`、`Comparable`、`NumField`、`Field`、`FieldByName`、`NumMethod`、`Method`、`MethodByName`、`NumIn`、`In`、`NumOut`、`Out`、`IsVariadic`。`reflect.StructField` 只暴露 schema metadata 字段，tag 解析等上层语义由具体源码库自己处理。

VM 源码中的 struct/interface 使用模块限定名作为 runtime 身份。`main.User`、`alpha.User` 和 `beta.User` 是三个不同类型，即使字段完全一致也不会互相 assignable；`Type.String()` 返回限定名，`Type.Name()` 返回短类型名，`Type.PkgPath()` 返回模块路径。`TypeFrom` 查询 VM 源码类型时也要求限定名，例如 `TypeFrom("alpha.User")`；`TypeFrom("User")` 不会隐式选择某个模块。FFI / ffigen / surface 注册的外部 schema 类型保持 schema 中声明的名字，不会自动加 VM 模块前缀。

`TypeFrom` / `Package` 在 `Bool=false` 时返回零值 metadata。`TypeFrom` 要求类型表达式中的所有 named type 都来自已注册 metadata，`Ptr<Missing>`、`Array<Missing>` 或 `function(Missing) Void` 这类嵌套未知类型会返回 `Bool=false`。`Type.Elem()`、`Type.Key()`、`Type.In()` 和 `Type.Out()` 会解析 Mini named type alias 后读取底层 array/map/function 结构；对不适用或越界输入返回零值，不采用 Go 原生 reflect 的 panic 行为。

`Field`、`Index`、`MapKeys`、`MapIndex` 和 `Unwrap` 只会把 pure value snapshot 放入返回的 `Any`；声明类型和实际值都会校验，空 array/map/struct 也不能凭空绕过 pointer、HostRef、channel、module、closure、function、host identity 或其他 FFI Any 禁止值。`MakeMap` 和 `Zero` 只创建可进入 pure `Any` 的值。`SetField` 只修改 `*struct` 或 `Any` 包装的业务 struct 值；`SetMapIndex` 只写入 pure-value key/value；`Assign` 按正常 VM 赋值规则写入 pointer 目标；`Append` 按正常 VM 追加规则写入数组目标。`reflect.Type`、`reflect.StructField`、`reflect.Method`、`reflect.Route`、`reflect.PackageInfo` 和 `reflect.Member` 是只读 metadata struct，不能通过 `SetField` 修改。

```go
package main

import "reflect"

type User struct {
    Name string `json:"name"`
    Age  int
}

func main() {
    u := User{Name: "Ada", Age: 41}
    t := reflect.TypeOf(u)
    if t.Kind() != reflect.Struct {
        panic("bad type")
    }

    f, ok := t.FieldByName("Name")
    if !ok || f.Tag != "json:\"name\"" {
        panic("bad field metadata")
    }

    if err := reflect.SetField(&u, "Age", 42); err != nil {
        panic(err.Error())
    }
}
```

### 2.11 Encoding JSON

顶层 `ffilib` 提供的 `encoding/json` 是 VM 源码库实现，不调用 Go 标准库 `encoding/json`，也不使用 Go 原生 reflect。JSON 文本解析、序列化、struct 字段遍历和 tag 处理都在 VM 代码里完成；typed `Unmarshal` 通过 compiler call template 读取第二个实参的静态类型，因此 `&target` 不需要进入 `Any`。

`encoding/json/internal` 是实现细节，只能被 `encoding/json` 及 compiler 生成的模板代码使用，用户代码不能直接 import。

公开 API：

- `Marshal(v any) ([]byte, error)`
- `Decode(data []byte) (any, error)`
- `Unmarshal(data []byte, out *T) error`

`Decode` 返回动态纯值树：object 是 `map[string]any`，array 是 `[]any`，string/bool/null 分别映射为 `string`、`bool`、`nil`，number 统一为 `float64`。`Unmarshal` 是 direct-call compiler template API，不作为 runtime package member 暴露，也不能作为函数值反射或传递；它要求第二个实参是 pointer，按实参静态类型转换并通过 `reflect.Assign` 写回目标。未知字段忽略，缺失字段保持零值。

支持的 JSON 类型目标包括 `bool`、`int64`、`float64`、`string`、`[]byte`、`[]T`、`map[string]T`、struct 和 `any`。struct 字段默认使用字段名，支持 `json:"name"` 和 `json:"-"`；第一版不支持 `omitempty`、`string` option、匿名字段展开、自定义 marshaler/unmarshaler 或非 string map key。pointer 字段、HostRef、channel、function、module、typed interface 和 error 值会返回错误。

```go
package main

import "encoding/json"

type User struct {
    Name string `json:"name"`
    Age  int64  `json:"age"`
}

func main() {
    data, err := json.Marshal(User{Name: "mini", Age: 7})
    if err != nil {
        panic(err)
    }

    var out User
    if err := json.Unmarshal(data, &out); err != nil {
        panic(err)
    }

    dynamic, err := json.Decode(data)
    if err != nil {
        panic(err)
    }
    _ = dynamic
}
```

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

脚本支持 `new(T)`、`&x`、`&T{...}`、`&struct{...}{...}` 和 `*p`，但指针本质是受控 VM slot 引用，不是宿主裸地址。

```go
p := new(Int64)
*p = 123
println(*p)

type Point struct {
    X Int64
}

q := &Point{X: 1}
q.X = 2

r := &struct {
    Name string
}{Name: "mini"}
println(r.Name)
```

`&x` 用于 VM 可寻址 slot，例如局部变量、全局变量、VM struct 字段和 `*p`。`Ptr<T>` 与 `T` 之间通过显式 `&` / `*` 读写，不做隐式互转，也不会使用 host handle ID 表示。

VM pointer 是 runtime-only 引用，只能留在 VM slot / 指针表达式路径内。它不能写入 `Any`、FFI wire、host identity、channel payload，或 `Array<Any>` / `Map<..., Any>` 这类纯 Any payload。运行时对地址写入、composite literal、append、map index/delete、slice index 和 channel send 都会按目标声明类型重新校验。

### 值与引用语义

运行时变量统一存放在 slot 中。slot 持有声明类型，赋值时把右侧值规范化后写入 slot，而不是替换掉变量的类型身份。

赋值门禁始终以 slot 声明类型为准。`Any` slot 会保存动态值 wrapper；从 `Any` 写回非 `Any` slot 不走隐式拆箱，必须由类型断言、显式转换或已声明的语义路径产生可赋值值。数组元素、map value、struct 字段、函数参数、返回值和 channel element 使用同一套赋值规则。

值语义：

- primitive、`TypeBytes` 和 VM struct 按值复制。
- struct composite literal 生成真实 VM struct。
- struct 字段本身也是 typed slot；函数参数、返回值和 value receiver 会复制 struct 的字段 slot。

引用语义：

- array、map、`Ptr<T>`、`HostRef<T>`、closure、module 和 interface 目标共享底层对象。
- `Ptr<T>` 指向 VM slot；`*p = v` 会写回目标 slot，并继续执行声明类型校验。
- 闭包 capture 共享同一个 slot，因此子 VM 执行上下文可以修改父作用域捕获变量。

map 与 struct 是不同运行时类型。map key 会保留 primitive key 类型，`Map<Int64, String>{1: "a"}` 与 `Map<String, String>{"1": "a"}` 不会在运行时混淆。

## 4.1 VM 执行上下文调度语义

当前并发模型包括 `go f()` 和语言级 channel/select。`go f()` 会创建子 VM 执行上下文，但整个 VM 始终单线程执行；所谓并发是 VM 调度器在调度点、channel/select 阻塞点或异步 FFI completion 处切换执行上下文。

### 基本用法

VM 侧不暴露公开 yield API。需要等待外部事件或让出执行时，应通过异步 FFI；标准库 `time.Sleep(ns)` 就是异步 FFI，返回值类型仍是 `Void`。当前模型中，`time.Sleep` 绑定 `VMClock`，因此 run pause 会冻结脚本等待；`time.Now` / `Since` / `Until` 以及 `context` deadline 则继续读取真实时间，避免把冻结时钟值传入外部 FFI 或绝对时间戳逻辑。

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

- 子执行上下文的 panic 会让整个 VM 执行失败，除非在子函数中 recover
- `go f()` 没有返回值，父流程需要通过共享状态或显式同步协议观察结果
- 需要把结果带回父流程时，使用共享变量、语言级 channel 或其它同步协议

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

### Channel / Select

语言级 channel 支持 Go 风格输入，并在前端转换为 canonical type：

- `chan T` -> `Chan<T>`
- `<-chan T` -> `RecvChan<T>`
- `chan<- T` -> `SendChan<T>`

运行时支持 `make(chan T[, cap])`、send、receive、二值 receive、`close`、`len`、`cap`、`select`、`default` 和 channel `for range`。`for v := range ch` 会持续 receive，直到 channel close。

```go
package main

func worker(out chan Int64) {
    out <- 42
    close(out)
}

func main() {
    ch := make(chan Int64, 1)
    go worker(ch)

    total := 0
    for v := range ch {
        total = total + v
    }
    if total != 42 {
        panic("bad channel result")
    }
}
```

`select` 会先尝试已经 ready 的 VM channel 和 FFI channel endpoint；没有 ready case 且存在 `default` 时执行 `default`。没有 ready case 且没有 `default` 时，当前执行上下文会 park。如果所有执行上下文都只剩 VM channel 等待，runtime 会返回 `VMAllBlockedError`。FFI channel endpoint 等待属于外部 wake source，可以让调度器继续等待宿主侧 send/receive/close。FFI wire 解码 channel endpoint 时会同时校验 element type 和方向：`RecvChan<T>` 只能接收可 recv 的 endpoint，`SendChan<T>` 只能接收可 send 的 endpoint，`Chan<T>` 必须双向可用。

host goroutine 可以由 FFI channel endpoint 用来等待宿主 channel 或 I/O，并在完成后唤醒 VM 调度器；VM 指令继续由 VM 调度器执行。

### Context

顶层 `ffilib.Surface()` 提供标准库 `context` 包。脚本侧 API 对齐 Go 的核心形态：

- `Background() Context`
- `TODO() Context`
- `WithCancel(parent Context) (Context, CancelFunc)`
- `WithDeadline(parent Context, deadline *time.Time) (Context, CancelFunc)`
- `WithTimeout(parent Context, timeout int64) (Context, CancelFunc)`
- `WithValue(parent Context, key any, val any) Context`
- `Canceled` / `DeadlineExceeded`

`Context` 接口包含 `Deadline() (*time.Time, bool)`、`Done() <-chan struct{}`、`Err() error` 和 `Value(key any) any`。`Done()` 返回 VM receive-only channel，取消传播、deadline timer 和 value chain 都在 VM 源码库里完成；`WithTimeout` 的 timeout 单位是纳秒，和 `time.Duration` 的底层表示一致。已过期的 deadline / timeout 会在创建时同步关闭 `Done()` 并设置 `DeadlineExceeded`；`WithValue` 会拒绝 nil 和不可比较 key。

deadline 依赖 `time.Time` host opaque 类型，timer 等待通过异步 FFI 暴露为 `WaitExternal`，并按真实时间推进。取消 deadline context 时会停止 timer 并完成已挂起的 timer waiter；VM abort 取消 timer wait 时会移除 waiter，并在没有其它 waiter 时停止真实 timer，避免 abandoned wait 继续持有宿主计时器。VM context 父子关系会同步传播取消；只有非 VM context 形态才退回到等待父 `Done()` 的传播执行上下文。

### 当前限制

- 同步 FFI 调用会阻塞整个 VM；只有返回 `ffigo.Async[T]` 的异步 FFI 会挂起当前执行上下文并在 completion 时恢复

### 异步 FFI 等待来源

`ffigo.Async[T]` 启动后必须返回 `ffigo.WaitHandle`，用于告诉 VM 调度器这个挂起点是否还可能被 VM 外部事件唤醒。

- `ffigo.WaitExternal`: completion 可由 timer、I/O、宿主 goroutine 或其它不依赖 VM 继续执行的外部事件触发。
- `ffigo.WaitDependsOnVM`: completion 需要其它 VM 执行上下文继续运行才能发生，例如 VM 侧同步对象、等待另一个 VM action 释放的 gate。
- 只有在 `Start` 返回前已经同步调用 `done.Complete(...)` 时，才可以返回 `nil` wait handle。
- VM abort、context 取消或 fatal error 会调用 pending wait handle 的 `Cancel()`；正常 completion 不会额外调用 `Cancel()`。

当没有可运行执行上下文，且所有 pending async FFI 都不是 `WaitExternal` 时，runtime 会返回 `*runtime.VMAllBlockedError`。错误中包含阻塞的执行上下文 ID、FFI route、method ID 和 wait reason，避免这类死等退化成静默挂起。

最小 async FFI 形态：

```go
return ffigo.AsyncFunc[ffigo.Void](func(_ context.Context, done ffigo.Completion[ffigo.Void]) (ffigo.WaitHandle, error) {
    timer := time.AfterFunc(time.Millisecond, func() {
        done.Complete(ffigo.Void{}, nil)
    })
    return ffigo.NewWaitHandle(ffigo.WaitExternal, "time.Sleep", func() {
        timer.Stop()
    }), nil
})
```

### 使用警告

- `go f()` 创建新的 VM 执行上下文，由协作式调度器调度。
- 没有异步 completion 或调度点时，root `main` 可能直接返回并停止尚未运行的子执行上下文。
- 子执行上下文的 panic 默认会失败整个 VM；需要隔离失败时在子函数中使用 `try/recover`。

## 5. FFI 生成器

`ffigen` 负责把 Go 接口或结构体导出为 schema-only FFI 桥接代码；CLI 入口在 `core/cmd/ffigen`，生成器核心在 `core/ffigen`。

FFI `Any` 面向纯值数据，例如 primitive、bytes、array、map 和 VM value struct。宿主对象、error 和 channel 分别通过具体 `HostRef<T>`、`Error`、`Chan<T>` / `RecvChan<T>` / `SendChan<T>` 在 schema 中表达。VM pointer、`HostRef<T>`、channel、module、closure、host error/interface handle 都不能通过 FFI `Any` 编码；如果 API 需要这些身份，必须在 schema 中显式建模。MethodID 0 / `Invoke` 用于已有明确 schema 的 route 或 typed interface method。

生成的常量 schema 直接使用 `runtime.ConstInt64`、`runtime.ConstFloat64`、`runtime.ConstString` 或 `runtime.ConstBool`，不会在 runtime 通过反射推断 Go 常量类型。非 primitive 常量应在生成阶段失败。

生成物和手写集成都产出 `surface.Bundle`，再由 `executor.UseSurface(...)` 统一校验和绑定。

### 5.1 参数模型

命令行参数：

```bash
go run gopkg.d7z.net/go-mini/core/cmd/ffigen -pkg sampleffi -out sample_ffigen.go api.go
```

- `-pkg`: 生成文件的 Go 包名
- `-out`: 输出文件

### 5.2 目录模式与文件模式

`ffigen` 支持两种模式。

#### 目录模式

输入是一个目录，`-out` 也必须是目录：

```bash
go run gopkg.d7z.net/go-mini/core/cmd/ffigen -pkg sampleffi -out ./gen ./
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
go run gopkg.d7z.net/go-mini/core/cmd/ffigen -pkg sampleffi -out sample_ffigen.go api.go
```

文件模式行为：

- 维持单文件/多文件生成习惯
- 适合局部样例和小范围生成
- 不允许抢占目录模式的保留文件名 `ffigen_<pkg>.go`

### 5.3 注释约定

`ffigen` 通过注释决定生成目标。下面片段分别展示各类注释；目录模式下，一个 Go package 内最多保留一个 `ffigen:module`。

```go
// ffigen:module sample
type SampleAPI interface {
    Add(a, b int64) int64
}
```

```go
// ffigen:module sample
// ffigen:methods
type Counter struct {
    Value int64
}
```

```go
// ffigen:module sampleio
// ffigen:interface
type ByteReader interface {
    Read(buf []byte) (int64, error)
}
```

```go
// ffigen:module samplecopy
// ffigen:proxy
type CopyAPI interface {
    Rewrite(values *ffigo.ArrayRef[int64]) int64
}
```

```go
// ffigen:global sample DefaultCounter HostRef<sample.Counter>
var DefaultCounter = &Counter{Value: 100}
```

- `ffigen:module <name>`
  定义 VM 暴露的模块名。
- `ffigen:methods [member]`
  只用于 `struct`，并且必须和同一声明上的 `ffigen:module <name>` 配套出现；表示导出该结构体的方法集。`member` 是当前 `ffigen:module` 下的本地类型名，不接受 `pkg.Type`、Go import path 或 slash path。
- `ffigen:interface`
  只用于命名 `interface`，表示额外导出一个 VM 命名接口 schema。
- `ffigen:proxy`
  只用于需要 Go 端直连调用生成 proxy 的接口；未标记时生成 surface 和 route 绑定。
- `ffigen:global <package> <name> <canonical-type>`
  只用于只读包值，用于把宿主 opaque singleton 生成为 pinned `HostRef<T>` package value。

### 5.4 命名规则

VM 可见类型名以 `ffigen:module` 为准：

- 本地类型：`sample.Counter`
- 值类型 struct：`sample.Pair`
- 跨模块类型：优先解析为对方模块名，例如 `sampleio.ByteReader`
- VM 可见类型名不会隐式使用宿主 Go import path；如果你显式把 `ffigen:module` 设为带点号的完整 module path，canonical type grammar 可以表达该名称，但仍按精确 module path 解析，不做短名或后缀匹配。

### 5.5 导出模式

#### 模块 surface、值 struct、HostRef 方法和只读包值

```go
package sampleffi

type Pair struct {
    Left  int64
    Right int64
}

// ffigen:global sample DefaultCounter HostRef<sample.Counter>
var DefaultCounter = &Counter{Value: 100}

// ffigen:module sample
// ffigen:module sample
// ffigen:methods
type Counter struct {
    Value int64
}

func (c *Counter) Add(delta int64) int64 {
    c.Value += delta
    return c.Value
}

func (c *Counter) Current() int64 {
    return c.Value
}

// ffigen:module sample
type SampleAPI interface {
    Add(a, b int64) int64
    MakePair(left, right int64) Pair
    NewCounter(start int64) *Counter
}

type Host struct{}

func (Host) Add(a, b int64) int64 {
    return a + b
}

func (Host) MakePair(left, right int64) Pair {
    return Pair{Left: left, Right: right}
}

func (Host) NewCounter(start int64) *Counter {
    return &Counter{Value: start}
}
```

生成：

- route descriptor 列表，也就是 `[]runtime.FFIRouteDecl`；type method 使用 `TypePackagePath` / `TypeMemberName` 标识 owner
- host router
- `SurfaceXxx(...) *surface.Bundle`
- `SurfaceGlobals()`，用于只读 package value

生成的 `SurfaceXxx(...)` 在 bind 阶段通过 `ffigo.NewRouterBridge(...)` 和 `BoundFFISurface.BindSchemaRoutes(...)` 把 schema route 绑定到 host router。

生成并装配：

```bash
go run gopkg.d7z.net/go-mini/core/cmd/ffigen -pkg sampleffi -out sample_ffigen.go api.go
```

```go
executor, err := engine.NewMiniExecutor()
if err != nil {
    panic(err)
}

if err := executor.UseSurface(sampleffi.SurfaceSampleAPI(sampleffi.Host{})); err != nil {
    panic(err)
}
if err := executor.UseSurface(sampleffi.SurfaceCounter()); err != nil {
    panic(err)
}
if err := executor.UseSurface(sampleffi.SurfaceGlobals()); err != nil {
    panic(err)
}
```

Mini 代码可导入 `sample`：

```go
package main

import "sample"

func main() {
    if sample.Add(2, 3) != 5 {
        panic("add")
    }

    p := sample.MakePair(7, 9)
    if p.Left != 7 || p.Right != 9 {
        panic("pair")
    }

    c := sample.NewCounter(10)
    if c.Add(5) != 15 || c.Current() != 15 {
        panic("counter")
    }

    if sample.DefaultCounter.Current() != 100 {
        panic("global")
    }
}
```

#### 命名接口 schema

```go
package sampleiface

// ffigen:module sampleio
// ffigen:interface
type ByteReader interface {
    Read(buf []byte) (int64, error)
}
```

生成：

- `RuntimeInterfaceSpec`
- `SurfaceByteReaderSchema() *surface.Bundle`

这类用于把宿主命名接口暴露给 VM 类型系统，例如需要在其他 FFI schema 中声明 `sampleio.ByteReader` 参数。

#### Go 端 proxy

```go
package sampleproxy

import "gopkg.d7z.net/go-mini/core/ffigo"

// ffigen:module samplecopy
// ffigen:proxy
type CopyAPI interface {
    Rewrite(values *ffigo.ArrayRef[int64]) int64
}
```

生成：

- `SurfaceCopyAPI(impl) *surface.Bundle`
- `NewCopyAPIProxy(bridge, registry) CopyAPI`

`ffigen:proxy` 适合 Go 端单元测试、桥接自检或显式需要从 Go 调用生成 FFI wire 的场景。VM 装配仍然使用 `SurfaceCopyAPI(impl)`。

目录模式下，只有显式标记 `ffigen:interface` 的命名接口才会按 schema-only 方式导出；未标记的普通命名接口会被跳过。文件模式下，普通命名 `interface` 会生成 FFI target；Go 端 proxy 仍然只由 `ffigen:proxy` 控制。

#### FFI channel endpoint

`ffigen` 支持 Go channel 类型：

- `chan T` 生成 `Chan<T>`
- `<-chan T` 生成 `RecvChan<T>`
- `chan<- T` 生成 `SendChan<T>`

FFI wire 上不复制 channel 本身，只传 `ffigo.ChannelRegistry` 中的 endpoint ID。生成的 host router 和 Go proxy 会把 Go channel 包装成 `ffigo.ChannelEndpoint`，send/receive payload 继续使用同一套 FFI 编解码。receive-only endpoint 只暴露 `Recv` / `TryRecv`，send-only endpoint 只暴露 `Send` / `TrySend`。endpoint 解码会校验 schema 方向，方向不匹配会直接返回 FFI channel direction mismatch，而不是把错误延后成 VM 阻塞。

这使 `context.Context.Done()` 这类 API 可以按 receive-only channel 暴露。对于 `Done() <-chan struct{}`，empty struct 会映射为 `Void`，VM 侧 schema 形态是 `RecvChan<Void>`。

channel 参数使用 `<-chan T` 或 `chan<- T` 表达方向；非 proxy surface 返回的 bidirectional `chan T` 会映射为 VM 侧 `Chan<T>` endpoint。显式 `ffigen:proxy` 的 channel API 使用方向类型。

FFI channel endpoint 的宿主 goroutine 负责等待宿主 channel、完成 payload 编解码并唤醒 VM 调度器。`ffigo.ChannelRegistry` 支持 `UnregisterChannel`，VM endpoint close、host channel close 和生成代理的 close 路径会注销 endpoint。

#### 结构体方法集导出

```go
// ffigen:module sample
// ffigen:methods
type Counter struct {
    Value int64
}
```

结构体上写 `ffigen:methods` 时，必须显式写出同一类型的 `ffigen:module`。owner member 默认使用结构体名；也可以写 `ffigen:methods CounterLike` 改名，但该名称仍然只能是当前 `ffigen:module` 下的本地 member/type 名。这类会生成结构体方法 route descriptor 和对应 struct schema。结构体方法 route 会写入 type schema 的 methods，而不是包成员函数。

### 5.6 Struct ownership

FFI struct schema 有两类 ownership：

- `VMValue`: 普通值类型 struct。VM 可以创建 zero value、composite literal 和 `new(T)`，FFI wire 使用 struct payload 传递字段。
- `HostOpaque`: opaque host type。VM 通过 `HostRef<T>` 持有宿主对象。

`HostOpaque` 对象通常来自 FFI 工厂函数或 FFI 返回值，例如 `sync.NewWaitGroup()`。

`ffigen` 生成 schema 时会把普通值 struct 标记为 `VMValue`，把通过 `HostRef<T>` 暴露的宿主对象标记为 `HostOpaque`。

### 5.7 装配

生成后通过 surface bundle 装配：

```go
executor, err := engine.NewMiniExecutor()
if err != nil {
    panic(err)
}

if err := executor.UseSurface(sampleffi.SurfaceSampleAPI(sampleffi.Host{})); err != nil {
    panic(err)
}
if err := executor.UseSurface(sampleffi.SurfaceCounter()); err != nil {
    panic(err)
}
if err := executor.UseSurface(sampleffi.SurfaceGlobals()); err != nil {
    panic(err)
}
```

## 6. Interface

`go-mini` 支持命名接口、匿名接口、接口嵌入、类型断言和 type switch。`ffigen` 生成宿主到 VM 的 schema、route descriptor、host router 和显式 opt-in proxy；反向代理生成不属于 `ffigen` 范围。

## 7. LSP / IDE

LSP 和查询能力建立在 `AnalysisProgram` 的源码信息之上，执行主路径使用 `ExecutableProgram` / bytecode。常用 API：

```go
analysis, _ := executor.AnalyzeGoCodeTolerant(source)
hover := analysis.GetHoverAt(10, 5)
refs := analysis.GetReferencesAt(10, 5, true)
def := analysis.GetDefinitionAt(10, 5)
```

stdio LSP 使用 full text sync，diagnostics debounce 在 server 侧处理；保存会立即 flush pending diagnostics，关闭文件会取消 pending diagnostics 并清理对应诊断。

详细集成方式见 [LSP.md](./LSP.md)。
