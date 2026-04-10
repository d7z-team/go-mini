# Go-Mini 使用文档

`go-mini` 是一个 Go 风格脚本引擎。

- 编译阶段生成稳定 `go-mini-bytecode`
- 运行时执行 lowered task plan / prepared program
- FFI 通过 `ffigen` 生成 schema-only 桥接代码

## 1. 基础执行

```go
executor := engine.NewMiniExecutor()
executor.InjectStandardLibraries()

program, err := executor.NewRuntimeByGoCode(`
package main
func main() {
    println("hello")
}
`)
if err != nil {
    panic(err)
}
if err := program.Execute(context.Background()); err != nil {
    panic(err)
}
```

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

- `Program`: 重建蓝图用的 AST metadata
- `Bytecode`: 稳定 `go-mini-bytecode`

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
bytecodeProgram, err := executor.CompileGoFileToBytecode("script.mini", source)
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

### 2.5 推荐的互转路径

- 源码执行：`NewRuntimeByGoCode`
- 源码编译：`CompileGoCode`
- 源码转 bytecode：`CompileGoCodeToBytecode`
- 程序导出：`MarshalBytecodeJSON`
- bytecode JSON 装载：`NewRuntimeByBytecodeJSON`
- bytecode JSON 恢复编译产物：`ArtifactFromBytecodeJSON`

## 3. CLI

`cmd/exec` 使用 bytecode-first 模型：

```bash
# 执行源码
mini-exec -run script.mini

# 只编译并输出 bytecode JSON
mini-exec -o script.json script.mini

# 反汇编源码编译结果
mini-exec -d script.mini

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

## 5. FFI 生成器

`ffigen` 负责把 Go 接口或结构体导出为 schema-only FFI 桥接代码。

### 5.1 参数模型

现在只保留两个参数：

```bash
go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg orderlib -out order_ffigen.go interface.go
```

- `-pkg`: 生成文件的 Go 包名
- `-out`: 输出文件

命令行参数只包含：

- `-pkg`
- `-out`

### 5.2 目录模式与文件模式

`ffigen` 现在分两种正式模式。

#### 目录模式

输入是一个目录，`-out` 也必须是目录：

```bash
go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg orderlib -out ./gen ./
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
go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg orderlib -out order_ffigen.go interface.go
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

// ffigen:methods orderlib.Page
type PageMethods interface {
    Click(p *orderlib.Page) error
}
```

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

#### 结构体方法集导出

```go
// ffigen:methods
type Calculator struct {
    Base Int64
}
```

结构体上只写 `ffigen:methods` 时，默认使用结构体名作为方法集前缀。

### 5.6 注册

生成后直接调用 `RegisterXXX`：

```go
executor := engine.NewMiniExecutor()
registry := executor.HandleRegistry()

orderlib.RegisterOrderAPI(executor, impl, registry)
```

## 6. Interface

`go-mini` 支持命名接口、匿名接口、接口嵌入、类型断言和 type switch。`ffigen` 当前只生成宿主到 VM 的 schema/proxy/router，不再提供反向代理生成功能。

## 7. LSP / IDE

LSP 和查询能力建立在 AST 蓝图之上，执行主路径使用 compiled artifact / prepared program。常用 API：

```go
hover := program.GetHoverAt(10, 5)
refs := program.GetReferencesAt(10, 5)
def := program.GetDefinitionAt(10, 5)
```

详细集成方式见 [LSP.md](./LSP.md)。
