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

## 2. CLI

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

## 3. 安全与沙盒

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

## 4. FFI 生成器

`ffigen` 负责把 Go 接口或结构体导出为 schema-only FFI 桥接代码。

### 4.1 参数模型

现在只保留两个参数：

```bash
go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg orderlib -out order_ffigen.go interface.go
```

- `-pkg`: 生成文件的 Go 包名
- `-out`: 输出文件

命令行参数只包含：

- `-pkg`
- `-out`

### 4.2 注释约定

```go
// ffigen:module order
type OrderAPI interface {
    New(id string) (*Order, error)
}

// ffigen:methods orderlib.Page
type PageMethods interface {
    Click(p *orderlib.Page) error
}

// ffigen:reverse
type ScriptHandler interface {
    OnEvent(name string, data any) error
}
```

### 4.3 命名规则

VM 可见类型名以 `ffigen:module` 为准：

- 本地类型：`order.Order`
- 跨模块类型：优先解析为对方模块名，例如 `io.File`
- 完整 Go import path 不会暴露到 `Ptr<T>`、struct schema、方法前缀中

### 4.4 导出模式

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

### 4.5 注册

生成后直接调用 `RegisterXXX`：

```go
executor := engine.NewMiniExecutor()
registry := executor.HandleRegistry()

orderlib.RegisterOrderAPI(executor, impl, registry)
```

## 5. Interface 与反向代理

`go-mini` 支持命名接口、匿名接口、接口嵌入、类型断言、type switch，以及 `ffigen:reverse` 生成的反向代理。

```go
// ffigen:reverse
type ScriptHandler interface {
    OnEvent(name string, data any) error
}
```

生成后可以把脚本里的实现包装回 Go 接口：

```go
proxy := NewScriptHandler_ReverseProxy(program, session, callable, bridge)
err := proxy.OnEvent("login", "user_1")
```

## 6. LSP / IDE

LSP 和查询能力建立在 AST 蓝图之上，执行主路径使用 compiled artifact / prepared program。常用 API：

```go
hover := program.GetHoverAt(10, 5)
refs := program.GetReferencesAt(10, 5)
def := program.GetDefinitionAt(10, 5)
```

详细集成方式见 [LSP.md](./LSP.md)。
