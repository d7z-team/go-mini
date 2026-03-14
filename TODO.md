# GoMini 引擎容器与引用类型跨界传递重构指南 (Proxy 方案)

## 一、 现状分析 (Current State Analysis)

在现有的 GoMini 引擎中，脚本层（VM）与宿主层（Native Go）之间的边界在处理容器和自定义类型时，存在严重的“副作用丢失”问题以及性能隐患。

### 1. 内部存储结构与 Native 签名的割裂
*   **Array (切片)**: VM 内部动态语言特性要求容器具有包容性，因此通常存储为 `[]interface{}` 或 `*[]interface{}`。但 Native 函数声明为了类型安全，通常为强类型，例如 `func(list []ast.MiniString)`。
*   **Map (映射)**: 同样地，VM 内部存储为 `map[interface{}]interface{}`。Native 函数声明例如 `func(m map[ast.MiniString]ast.MiniInt64)`。
*   **DynStruct (用户自定义结构体)**: 这是 VM 独有的运行时概念，由 `runtime.DynStruct` 表达，核心数据存在 `map[string]any` 中。现有的 Native API 几乎无法优雅且安全地接收此类对象。

### 2. 核心瓶颈：单向拷贝导致副作用丢失与性能灾难
在 `core/runtime/executor.go` 的 `prepareCallArgs` 阶段，引擎目前处理 Array 和 Map 的方式是：**通过反射（`reflect.MakeSlice` / `reflect.MakeMap`）创建一个新的强类型 Go 切片/字典，然后将 VM 数据遍历、类型转换后填充进去**。
*   **副作用丢失 (Side-Effect Loss)**：Native 函数收到的是一个**深拷贝副本**。如果 Native 函数执行了 `list[0] = NewMiniString("A")` 或 `m["k"] = v`，这些修改仅仅作用于局部副本内存，VM 原始内存里的数据依然是旧的。这破坏了引用类型传递的基本语义。
*   **性能灾难 (Performance Penalty)**：当在循环中向 Native 传递包含成千上万个元素的切片时，每次调用都会触发 $O(N)$ 级别的全量内存分配和数据遍历转换，这是不可接受的。

### 3. Native 注册系统的妥协现状 (`parseMiniType`)
目前 `core/ast/ast_types_native.go` 中的 `parseMiniType` 允许并识别 Go 原生的 `reflect.Slice`, `reflect.Array`, `reflect.Map`。这鼓励了开发者使用原生的 `[]T` 和 `map[K]V` 作为函数签名，从而不断加剧上述的深拷贝与副作用丢失问题。

---

## 二、 核心架构决策：Proxy 代理模式

为了彻底解决上述问题，我们决定采用 **Proxy 代理模式** 进行核心架构级重构。

**核心思想**：废弃数据拷贝。通过定义一组标准的抽象接口（Interface），将 VM 的底层存储结构包装为代理对象，让 Native 函数**直接操作 VM 的底层存储内存**。

*   **Native 签名变更示例**：从接受原生切片 `func([]ast.MiniString)` 变更为接受代理接口 `func(ast.MiniArray)`。
*   **绝对优势**：
    1.  **极致性能 (Zero-Copy)**：无论容器有多大，传递的仅仅是一个实现了接口的结构体指针，时间复杂度为 $O(1)$。
    2.  **严格一致性 (Strict Consistency)**：Native 函数的任何增删改查，都是直接作用于 VM 的 `[]interface{}` 上。天然支持双向数据同步，副作用实时生效。
    3.  **消除转换隐患**：避免了 `interface{}` 到强类型的隐式反射转换带来的各种运行时 Panic 风险。

---

## 三、 详细结构设计 (Architecture Design)

必须在核心 AST 库中引入代理接口，使得底层的弱类型容器能够以安全的方式暴露给 Native 层。

### 1. 代理接口定义 (Interface Definitions)
在 `core/ast` 目录下新增以下接口。这些接口应嵌入 `MiniObj`，表明它们是合法的跨界对象。

```go
// ast.MiniArray: 代理切片接口
// 允许 Native 层直接安全地读写 VM 内部的 []interface{}
type MiniArray interface {
    MiniObj
    // 返回数组长度
    Len() int
    // 获取指定索引的元素。如果索引越界或类型不匹配（可选的强类型检查），返回错误。
    Get(index int) (MiniObj, error)
    // 设置指定索引的元素。如果索引越界，返回错误。
    Set(index int, val MiniObj) error
    // 在数组末尾追加元素
    Append(val MiniObj) error
    // 获取该数组被声明的内部元素类型（用于可选的类型安全校验）
    ElemType() GoMiniType 
}

// ast.MiniMap: 代理映射接口
// 允许 Native 层直接安全地读写 VM 内部的 map[interface{}]interface{}
type MiniMap interface {
    MiniObj
    // 返回 Map 中键值对的数量
    Len() int
    // 根据键获取值。如果键不存在，ok 返回 false。
    Get(key MiniObj) (val MiniObj, ok bool, err error)
    // 设置键值对
    Set(key MiniObj, val MiniObj) error
    // 删除指定键
    Delete(key MiniObj) error
    // 获取所有的键列表
    Keys() []MiniObj
}

// ast.MiniStruct: 代理结构体接口 
// 用于将 VM 的 DynStruct 安全地暴露给 Native
type MiniStruct interface {
    MiniObj
    // 获取结构体名（类型名）
    StructName() Ident
    // 获取指定字段的值
    GetField(name string) (MiniObj, error)
    // 设置指定字段的值
    SetField(name string, val MiniObj) error
    // 获取该结构体定义的所有字段名
    FieldNames() []string
}
```

### 2. 内部类型的实现桥接 (Implementation Bridge)
在 `core/runtime` 包中，现有的运行时结构必须实现这些接口。
*   包含 `[]interface{}` 的数组变量包装体必须实现 `ast.MiniArray`。
*   包含 `map[interface{}]interface{}` 的字典变量包装体必须实现 `ast.MiniMap`。
*   `runtime.DynStruct` 必须实现 `ast.MiniStruct`，提供对其内部 `Body` map 的访问代理。

---

## 四、 逐步实施计划 (Implementation TODOs)

请按照以下严格的顺序执行重构，每一步完成后必须确保编译通过。

### [ ] 阶段 1：定义并注册核心接口
1.  在 `core/ast/` 下新建 `ast_proxy.go` 或在现有接口文件中定义 `MiniArray`, `MiniMap`, `MiniStruct` 接口。
2.  在 `core/ast/ast_types.go` 和 `ast_types_native.go` 中，确保这三种接口作为特权类型被解析器识别。

### [ ] 阶段 2：重构 Native 类型注册拦截网 (`parseMiniType`)
**（极其重要）**
1.  修改 `core/ast/ast_types_native.go` 中的 `parseMiniType` 函数。
2.  **彻底移除** 对 Go 原生 `reflect.Slice` (`[]T`) 和 `reflect.Map` (`map[K]V`) 的支持。如果开发者试图注册包含原生切片或 Map 的函数，必须报错或忽略该方法。
3.  新增对 `ast.MiniArray` 和 `ast.MiniMap` 类型的反射识别。此时，Native 开发者只能使用这些接口签名。

### [ ] 阶段 3：在 Runtime 层实现代理适配器 (Adapters)
在 `core/runtime` 目录下提供上述接口的内部实现：
1.  **Array Adapter**: 创建一个类似 `type runtimeArray struct { data *[]any; elemType ast.GoMiniType }` 的结构，实现 `ast.MiniArray`。在 `Get/Set` 方法中，需要处理 `any` 到 `ast.MiniObj` 的安全转换。
2.  **Map Adapter**: 创建 `type runtimeMap struct { data map[any]any }`，实现 `ast.MiniMap`。处理 Map 的迭代和安全转换。
3.  **Struct Adapter**: 为现有的 `DynStruct` 补充实现 `ast.MiniStruct` 的方法（如 `GetField`, `SetField`），让其直接操作 `ds.Body`。

### [ ] 阶段 4：改造执行器的跨界数据通道 (`executor.go`)
1.  **`prepareCallArgs` (入参重构)**：
    *   移除旧的 `reflect.MakeSlice` 和 `reflect.MakeMap` 深拷贝逻辑。
    *   当 Native 函数的参数类型是 `ast.MiniArray` 时，将 VM 中的 `expr.Data` (`[]any`) 封装为 `runtimeArray` 适配器，直接作为参数传入。
2.  **`callRetParser` (返回值重构)**：
    *   当 Native 函数返回一个 `ast.MiniArray` 接口实例时，提取其底层的 `[]any` 数据指针，并封装为引擎可识别的 `Var` 注入 VM。

### [ ] 阶段 5：迁移现存的标准库与测试用例 (Migration)
这是破坏性最大的步骤，需要仔细处理。
1.  **标准库迁移**：全局搜索 `runtimes/` 目录。例如 `strings.go` 中的 `Split` 目前返回 `[]interface{}`（被识别为原生 Slice）。必须修改 `Split`，让其内部创建一个 `runtimeArray` 适配器并返回 `ast.MiniArray`。
2.  **测试修复**：修改 `core/e2e/` 中的 Native 函数测试注册用例。所有接收或返回 `[]T` 的 mock 函数都需要改为使用 `ast.MiniArray` 的 `Get`/`Append` 接口。

---

## 五、 验收标准 (Acceptance Criteria)

1.  **纯粹性验证**：代码库中（特别是 `parseMiniType`）不再存在允许原生 Go 切片 (`reflect.Slice`) 和 Map (`reflect.Map`) 自由进出 Native 函数的逻辑。
2.  **副作用闭环测试**：编写一个测试：Native 函数接收 `ast.MiniArray` 并修改索引 `0` 的值。函数返回后，VM 内再次访问数组的索引 `0`，必须能读取到修改后的新值（证明零拷贝引用生效）。
3.  **性能优化确认**：传递大型数组到 Native 函数不再有明显卡顿（移除了遍历拷贝）。
4.  **全量测试通过**：执行 `go test ./...`，包含语法树、运行时以及 E2E 集成测试在内的所有测试用例必须呈绿色通过状态。