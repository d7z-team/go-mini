# Go-Mini 使用文档

`go-mini` 是一个 Go 风格脚本引擎。

- 编译阶段生成稳定 `go-mini-bytecode`
- `core/gofrontend` 负责 Go source / Go AST 到 Mini AST 的前端转换
- `core/lowering` 负责 AST 到 `PreparedProgram` 的唯一转换，并把不支持的 AST / 非 canonical type 报为编译错误
- 运行时执行 lowered task plan / prepared program
- `PreparedProgram` 持久化显式 `Exports` / `ConstantTypes` 表，VM 模块导入按该表构造成员集合，常量按真实类型参与语义检查与导出
- FFI 通过 `ffigen` 生成 schema-only 桥接代码
- FFI schema 冲突判断由 runtime 统一实现，compiler 与 runtime 注册路径共享同一套一致性规则
- 对外扩展通过 `executor.UseSurface(...)` 装配 FFI schema/bind、模板和纯 VM 源码库
- 调用模板在 compiler 阶段展开成真实代码，随后进入正常 lowering / bytecode / runtime 链路
- surface 可以携带纯 VM 源码库，executor 按需重新解析 fresh AST，bytecode 会记录导入库的 module hash，装载时校验当前 executor 的库源码是否匹配
- 执行入口统一落在 `Artifact` / bytecode，runtime 只消费 prepared executable，AST 只保留在编译、分析和调试边界
- canonical type 文本由 `core/typespec` 统一实现；AST 前端通过 `core/ast/ast_types.go` 使用，runtime/VM 通过 `core/runtime/schema.go` 使用
- 二元运算、比较、nil 比较和赋值门禁由 `core/typespec` 统一定义；AST 语义检查与 runtime fallback 使用同一套规则
- 运算符重载只存在于 compiler/AST 阶段：原生运算不支持时解析左操作数的 `Op*` 方法，并在优化和 lowering 前改写为普通方法调用；bytecode/runtime 不保留重载分派
- 命名常量是编译期值，表达式中会降低为常量 push，不作为 runtime 变量加载；局部变量可以遮蔽同名常量，常量不能作为赋值目标
- `Any` 不是静态通配符；VM 语言层 `Any` 使用动态值 wrapper，FFI `Any` wire 只接受纯值数据
- VM 可见 `Error` 直接承载 Go `error`，VM 创建的 error 会附带 VM stack，FFI host error 会保留 host error chain
- 语言级 channel/select 通过 lowering / bytecode / runtime 执行，保持单线程协作式 VM 调度
- 异步 FFI 必须暴露等待来源，调度器会在所有 VM 执行上下文只剩内部互等时返回明确的 all-blocked 错误
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

`core` 默认提供核心引擎、native `errors` / `fmt.Errorf`，以及 `strings`、`strconv`、`math`、`sort` 这类纯原生值类型标准库 FFI。若需要 io/os/time/context/fmt/image 等完整标准库 FFI，再通过 `executor.UseSurface(ffilib.Surface())` 装配顶层 surface。

`NewRuntimeByGoCode` 是便捷入口，内部仍会先 `CompileGoCode(...)`，再从编译产物创建运行时；`Eval` 这类表达式便捷入口也会先降低成 prepared function 再交给 runtime 执行。对外持久化、跨进程传输和正式装载统一推荐使用 bytecode。

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

Mini AST、lowering、compiler、bytecode 和 runtime 只接受 canonical type。常见格式包括：

- `Array<T>`
- `Map<K, V>`
- `Chan<T>` / `RecvChan<T>` / `SendChan<T>`
- `Ptr<T>`
- `HostRef<T>`
- `tuple(A, B)`
- `function(A, B) R`
- `interface{Read(TypeBytes) tuple(Int64, Error);}`
- `struct { Name String; }`

Go 风格输入如 `[]int`、`*T`、`map[string]int`、`interface{}` 只允许出现在 Go 前端，必须在 `core/gofrontend` 转换阶段立即规范化。其他语言前端应实现 `core/frontend.Frontend`，输出已经规范化的 Mini AST。手写 AST、bytecode 和 FFI schema 必须使用 canonical type；执行装载只接受 bytecode。

公开 FFI schema 使用具体 `HostRef<T>` 或明确的 typed interface schema 表达 host identity。`Ptr<T>` 表示 VM 内部 slot 引用；channel 通过 `Chan<T>` / `RecvChan<T>` / `SendChan<T>` 作为 schema endpoint 暴露。`Any` 在 VM 语言层是动态值 wrapper，用于保存 nil 身份和真实动态值；它不是类型系统通配符，`Any` 赋给具体类型需要类型断言或显式转换路径。

FFI `Any` wire 只面向 primitive、bytes、array、map 和 VM value struct 这类纯值数据。VM pointer、`HostRef<T>`、channel、module、host error/interface handle 不允许进入 VM `Any` 或 FFI `Any`；VM 内部可保存的一等函数、闭包和接口身份也不会被 FFI `Any` 编码。

运算和比较遵循统一类型门禁：数值之间可做数值运算和比较，`String` 只和 `String` 做拼接与有序比较，`TypeBytes` 只和 `TypeBytes` 拼接，`%` / 位运算 / 位移只接受 `Int64`。不同 primitive 类型不会通过字符串化或 `Any` 通配自动比较；例如 `10 == "10"` 是编译错误，不会求值为 `true` 或 `false`。动态 `Any` 参与比较时会在运行时展开真实值并继续执行同一套规则，类型不匹配会返回 VM 错误。

当原生运算不支持时，compiler 会尝试解析左操作数上的运算符方法，并把表达式改写为普通方法调用，例如 `a + b` -> `a.OpAdd(b)`、`-a` -> `a.OpNeg()`。支持的方法名包括 `OpAdd`、`OpSub`、`OpMul`、`OpDiv`、`OpMod`、`OpBitAnd`、`OpBitOr`、`OpBitXor`、`OpLsh`、`OpRsh`、`OpEq`、`OpNeq`、`OpLt`、`OpLe`、`OpGt`、`OpGe`、`OpNeg`、`OpPos`、`OpNot` 和 `OpBitNot`。比较运算和 `OpNot` 必须返回 `Bool`，所有重载方法都必须返回单个非 `Void` 值；`&&` / `||` 不参与重载，不做右操作数查找或交换律匹配。

函数返回支持 Go 风格 tuple 转发：如果当前函数声明多个返回值，`return other()` 可以直接转发另一个 tuple-return 函数或 FFI route 的结果。返回分析、lowering 和 runtime return slot 会按 tuple item 校验与赋值。

### 2.1 编译为 Artifact

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

bytecode JSON 装载执行只依赖 `Executable`。如果 payload 缺少 executable prepared program，运行时装载会拒绝；展示指令和展示元数据不参与执行装载。`Executable` 中的显式导出表与常量类型表会和 globals/functions/constants/schema 一起校验，导出目标缺失、常量类型缺失或类型非法的 bytecode 会在执行前被拒绝。

### 2.5 推荐的互转路径

- 源码编译：`CompileGoCode`
- 其他语言前端编译：`CompileWithFrontend`
- 已有 AST 编译：`CompileAST`
- 源码便捷执行：`NewRuntimeByGoCode`
- 正式装载执行：`NewRuntimeByCompiled` 或 `NewRuntimeByBytecodeJSON`
- 源码转 bytecode：`CompileGoCodeToBytecode`
- 程序导出：`MarshalBytecodeJSON`
- bytecode JSON 恢复编译产物：`ArtifactFromBytecodeJSON`

FFI、编译期模板和纯 VM 源码库都通过 `UseSurface(...)` 装配。模块源码使用 `surface.Library(...)`，预编译模块使用 `RegisterModule(...)` 注册 `ExecutableProgram`。

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

Go 源码的 tolerant 分析入口是：

```go
analysis, diags := executor.AnalyzeGoCodeTolerant(source)
_ = analysis
_ = diags
```

`AnalysisProgram` 承载 AST、模板 hover 预览和 LSP 查询缓存；`ExecutableProgram` 承载 bytecode artifact 与 runtime executor。

### 2.7 调用模板

调用模板用于把源码中的虚拟函数调用在 compiler 首次语义检查后、AST 优化前展开成真实代码调用。顶层 `ffilib.Surface()` 会注册标准库 FFI 和 `print(...)` / `println(...)` 模板，它们会展开为 `fmt.Print(...)` / `fmt.Println(...)`。模板只参与编译期校验、补全和 AST 展开；展开完成后，lowering、bytecode 和 runtime 只看到真实函数调用。

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

如果 `audit` 是 compile-only facade，compiler 会用它识别模板调用，展开后的可执行产物直接包含真实代码。如果 `audit` 是真实包，则实际使用 `audit.Log(...)` 时会校验真实成员签名和 `SourceSig` 一致。

模板入口有两类：

- 全局函数模板：例如 `println(...)`，可以在任意位置调用，模板名作为编译期入口参与符号校验。
- 包成员模板：例如 `aaa.BBB(...)`，调用会在展开后替换成模板生成的真实代码；实际使用模板时，如果 `PackagePath` 对应真实包，compiler 会校验真实成员签名一致；compile-only facade 会在展开后落到真实代码。

模板体使用 Go `text/template` 渲染，常用 helper：

- `pkg "fmt"`：取得卫生的内部包 alias，并自动加入真实 import；参数必须是精确的完整包路径字符串字面量。
- `args`：展开全部调用参数，适合转发可变参数。
- `arg 0`：展开第 0 个参数占位符，不附带 `...`。
- `callArg 0`：展开第 0 个参数占位符；如果它是可变参数调用的最后一个参数，会保留 `...`。
- `argc` / `ellipsis`：读取参数数量和调用点是否使用 `...`。
- `fresh "name"`：生成稳定且不会撞用户代码的临时标识符。

模板的包依赖在实际渲染时校验，因此未使用的模板不会污染无关编译。模板参数以 AST 占位符方式回填，不允许通过 `.Args` 等模板数据对象访问参数，也不把参数重新格式化为 Go 源码。模板展开是 fixed-point：模板生成的代码如果继续调用模板，会递归展开直到只剩真实代码；递归模板链会在编译期报错。模板体形态由使用位置和渲染结果推导：表达式位置必须渲染成表达式；语句列表位置先按表达式解析，失败且源码签名返回 `Void` 时再按语句列表解析；`defer` / `go` 中的模板必须最终展开为单个 call expression。

LSP hover 会展示模板的最终渲染视图。展示参数来自调用点源码切片，`pkg` 以 `import "fmt"` 和 `fmt.Println(...)` 这类用户可读形式显示，不暴露 `__gomini_tpl_` 内部 alias；这只是 IDE 展示，实际 compiler 仍使用卫生 alias 和 AST 占位符完成展开。

模板名与真实内置函数、FFI 函数、常量、结构体和接口保持一一对应关系。`__gomini_tpl_` 前缀由模板展开器保留使用。

### 2.8 Surface VM 源码库

`surface.Bundle` 除了 FFI schema、bind 和 compiler-only 模板，也可以携带纯 VM 源码模块。源码库适合实现 Go-Mini 标准库里可以由 VM 语义表达的部分。标准库 `context` 包就是这个形态：公开 API 由 VM 源码库实现，内部 `context/internal` FFI 只提供 sentinel error、真实时间 timer 和 value key 校验。

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

源码库的边界是 compiler/engine 侧能力：`UseSurface` 会解析源码用于校验和计算 resolved module hash，但 executor 状态只保留规范化后的源码描述。compiler、LSP 分析和 module 装载每次按需重新解析 fresh AST，不共享或复用已被语义检查改写过的 AST。compiler 只在实际 import 该库时把 module requirement 写入 bytecode；runtime 装载 bytecode 时只校验 hash，并在执行 import 时通过 `ModulePlanLoader` 取得 `PreparedProgram`。runtime 包本身不 import AST，也不解析源码。

library hash 包含自身源码以及同一个 surface 内导入的 library hash，因此下层纯 VM 库变化会让上层库的 bytecode requirement 失配。module path 与 source hash 一一对应；相同源码重复合并是幂等的。

源码库对外只暴露自身源码声明出的导出成员：函数、变量、常量、类型、结构体和接口的名字必须是 Go 风格 exported identifier，也就是 ASCII 大写字母开头。小写 helper 可以被库内部调用，但不会出现在 `pkg.` 成员补全、语义校验或 runtime 模块对象上。导出表不会继承主程序局部/全局 scope，也不会把默认内建函数或 FFI 注入的常量当成库成员；因此 `mathx.len`、`mathx.math.Pi` 这类不属于库源码导出表的成员会被诊断为不存在。

lowering 会把导出成员写入 `PreparedProgram.Exports`，bytecode JSON 会持久化这张表。runtime 执行 `import` 时只从导入模块的 prepared exports 构造 `VMModule.Data`，不会再从 lexical context、shared globals 或 FFI route fallback 查找模块成员。模块对象使用独立的 `TypeModule`，不会再作为 `Any` 参与成员访问或 LSP 类型推断。

源码库方法会在 prepared function 中记录 receiver 元数据。闭包中捕获的 VM pointer 可以直接取私有指针方法值，例如 `fn := c.cancel` 或 `c.cancel(err)`；runtime 通过 receiver 元数据和闭包词法 executor 解析 VM 方法体。

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

FFI 返回的 Go `error` 会进入 `VMHostError` 包装。包装会保留 host handle/bridge，并在可解析时保留原始 Go error chain，因此 VM 内的 `errors.Is(hostWrapped, hostTarget)` 会走 Go `errors.Is` 语义。跨 FFI 传递 error 时在 schema 中显式声明 `Error`。

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

`&x` 用于 VM 可寻址 slot，例如局部变量、全局变量、VM struct 字段和 `*p`。`Ptr<T>` 在 runtime 内部使用独立 pointer 值保存 `*Slot`。`Ptr<T>` 与 `T` 之间通过显式 `&` / `*` 读写，不做隐式互转，也不会使用 host handle ID 表示。

VM pointer 是 runtime-only 引用，只能留在 VM slot / 指针表达式路径内。它不能写入 `Any`、FFI wire、host identity、channel payload，或 `Array<Any>` / `Map<..., Any>` 这类纯 Any payload。运行时对地址写入、composite literal、append、map index/delete、slice index 和 channel send 都会按目标声明类型重新校验。

### 值与引用语义

运行时变量统一存放在 slot 中。slot 持有声明类型，赋值时把右侧值规范化后写入 slot，而不是替换掉变量的类型身份。

赋值门禁始终以 slot 声明类型为准。`Any` slot 会保存动态值 wrapper；从 `Any` 写回非 `Any` slot 不走隐式拆箱，必须由类型断言、显式转换或已声明的语义路径产生可赋值值。数组元素、map value、struct 字段、函数参数、返回值和 channel element 使用同一套赋值规则。

值语义：

- primitive、`TypeBytes` 和 VM struct 按值复制。
- struct composite literal 生成真实 VM struct。
- struct 字段本身也是 typed slot；函数参数、返回值和 value receiver 会复制 struct 的字段 slot。

引用语义：

- array、map、`Ptr<T>`、`HostRef<T>`、closure、module 和 interface 内部目标仍共享底层对象。
- `Ptr<T>` 指向 VM slot；`*p = v` 会写回目标 slot，并继续执行声明类型校验。
- 闭包 capture 共享同一个 slot，因此子 VM 执行上下文可以修改父作用域捕获变量。

map 与 struct 是不同运行时类型。map key 会保留 primitive key 类型，`Map<Int64, String>{1: "a"}` 与 `Map<String, String>{"1": "a"}` 不会在运行时混淆。

## 4.1 VM 执行上下文调度语义

当前并发模型包括 `go f()` 和语言级 channel/select。`go f()` 会创建子 VM 执行上下文，但整个 VM 始终单线程执行；所谓并发只是 VM 调度器在内部 safe point、channel/select 阻塞点或异步 FFI completion 处切换执行上下文。

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

- 子执行上下文的 panic 会让整个 VM 执行失败，除非在子上下文内部 recover
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

`select` 会先尝试已经 ready 的 VM channel 和 FFI channel endpoint；没有 ready case 且存在 `default` 时执行 `default`。没有 ready case 且没有 `default` 时，当前执行上下文会 park。纯 VM channel 等待属于 VM 内部互等；如果所有执行上下文都只剩这类等待，runtime 会返回 `VMAllBlockedError`。FFI channel endpoint 等待属于外部 wake source，可以让调度器继续等待宿主侧 send/receive/close。FFI wire 解码 channel endpoint 时会同时校验 element type 和方向：`RecvChan<T>` 只能接收可 recv 的 endpoint，`SendChan<T>` 只能接收可 send 的 endpoint，`Chan<T>` 必须双向可用。

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

deadline 依赖 `time.Time` host opaque 类型，timer 等待通过内部异步 FFI 暴露为 `WaitExternal`，并按真实时间推进。取消 deadline context 时会调用内部 timer `Stop()`，并完成已挂起的 timer waiter；VM abort 取消 timer wait 时会移除 waiter，并在没有其它 waiter 时停止真实 timer，避免 abandoned wait 继续持有宿主计时器。VM context 父子关系通过 child 注册表同步传播取消；只有非 VM context 形态才退回到等待父 `Done()` 的传播执行上下文。

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
- 没有异步 completion 或内部 safe point 时，root `main` 可能直接返回并停止尚未运行的子执行上下文。
- 子执行上下文的 panic 默认会失败整个 VM；需要隔离失败时在子上下文内部使用 `try/recover`。

## 5. FFI 生成器

`ffigen` 负责把 Go 接口或结构体导出为 schema-only FFI 桥接代码；CLI 入口在 `core/cmd/ffigen`，生成器核心在 `core/ffigen`。

FFI `Any` 面向纯值数据，例如 primitive、bytes、array、map 和 VM value struct。宿主对象、error 和 channel 分别通过具体 `HostRef<T>`、`Error`、`Chan<T>` / `RecvChan<T>` / `SendChan<T>` 在 schema 中表达。VM pointer、`HostRef<T>`、channel、module、closure、host error/interface handle 都不能通过 FFI `Any` 编码；如果 API 需要这些身份，必须在 schema 中显式建模。MethodID 0 / `Invoke` 用于已有明确 schema 的 route 或 typed interface method。

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
- `ffigen:methods [prefix]`
  只用于 `struct`，表示导出该结构体的方法集。
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
- 完整 Go import path 不会暴露到 `Ptr<T>`、struct schema、方法前缀中

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

- route descriptor 列表，也就是 `[]runtime.FFIRouteDecl`
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

FFI wire 上不复制 channel 本身，只传 `ffigo.ChannelRegistry` 中的 endpoint ID。生成的 host router 和 Go proxy 会把 Go channel 包装成 `ffigo.ChannelEndpoint`，send/receive payload 继续使用同一套 FFI 编解码。receive-only endpoint 只暴露 `Recv` / `TryRecv`，send-only endpoint 只暴露 `Send` / `TrySend`。endpoint 解码会校验 schema 方向，方向不匹配会直接返回 FFI channel direction mismatch，而不是把错误延后成 VM 内部阻塞。

这使 `context.Context.Done()` 这类 API 可以按 receive-only channel 暴露。对于 `Done() <-chan struct{}`，empty struct 会映射为 `Void`，VM 侧 schema 形态是 `RecvChan<Void>`。

channel 参数使用 `<-chan T` 或 `chan<- T` 表达方向；非 proxy surface 返回的 bidirectional `chan T` 会映射为 VM 侧 `Chan<T>` endpoint。显式 `ffigen:proxy` 的 channel API 使用方向类型。

FFI channel endpoint 的宿主 goroutine 负责等待宿主 channel、完成 payload 编解码并唤醒 VM 调度器。`ffigo.ChannelRegistry` 支持 `UnregisterChannel`，VM endpoint close、host channel close 和生成代理的 close 路径会注销 endpoint。

#### 结构体方法集导出

```go
// ffigen:methods
type Counter struct {
    Value int64
}
```

结构体上只写 `ffigen:methods` 时，默认使用结构体名作为方法集前缀。这类会生成结构体方法 route descriptor 和对应 struct schema。结构体方法 route 会写入 type schema 的 methods，而不是包成员函数。

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

LSP 和查询能力建立在 `AnalysisProgram` 的源码 AST 之上，执行主路径使用 `ExecutableProgram` / prepared program / bytecode。常用 API：

```go
analysis, _ := executor.AnalyzeGoCodeTolerant(source)
hover := analysis.GetHoverAt(10, 5)
refs := analysis.GetReferencesAt(10, 5, true)
def := analysis.GetDefinitionAt(10, 5)
```

stdio LSP 使用 full text sync，diagnostics debounce 在 server 侧处理；保存会立即 flush pending diagnostics，关闭文件会取消 pending diagnostics 并清理对应诊断。

详细集成方式见 [LSP.md](./LSP.md)。
