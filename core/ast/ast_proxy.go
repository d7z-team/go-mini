package ast

// MiniArray: 代理切片接口
// 允许 Native 层直接安全地读写 VM 内部的 []interface{}
type MiniArray interface {
	MiniObj
	// Len 返回数组长度
	Len() int
	// Get 获取指定索引的元素。如果索引越界或类型不匹配（可选的强类型检查），返回错误。
	Get(index int) (MiniObj, error)
	// Set 设置指定索引的元素。如果索引越界，返回错误。
	Set(index int, val MiniObj) error
	// Append 在数组末尾追加元素
	Append(val MiniObj) error
	// ElemType 获取该数组被声明的内部元素类型（用于可选的类型安全校验）
	ElemType() GoMiniType
}

// MiniMap: 代理映射接口
// 允许 Native 层直接安全地读写 VM 内部的 map[interface{}]interface{}
type MiniMap interface {
	MiniObj
	// Len 返回 Map 中键值对的数量
	Len() int
	// Get 根据键获取值。如果键不存在，ok 返回 false。
	Get(key MiniObj) (val MiniObj, ok bool, err error)
	// Set 设置键值对
	Set(key MiniObj, val MiniObj) error
	// Delete 删除指定键
	Delete(key MiniObj) error
	// Keys 获取所有的键列表
	Keys() []MiniObj
}

// MiniStruct: 代理结构体接口
// 用于将 VM 的 DynStruct 安全地暴露给 Native
type MiniStruct interface {
	MiniObj
	// StructName 获取结构体名（类型名）
	StructName() Ident
	// GetField 获取指定字段的值
	GetField(name string) (MiniObj, error)
	// SetField 设置指定字段的值
	SetField(name string, val MiniObj) error
	// FieldNames 获取该结构体定义的所有字段名
	FieldNames() []string
}

// SimpleMiniArray 是一个简单的 MiniArray 接口实现，用于 AST 内部或简单场景
type SimpleMiniArray struct {
	Data     []MiniObj
	ElemKind GoMiniType
}

func (s *SimpleMiniArray) GoMiniType() Ident {
	return Ident(CreateArrayType(s.ElemKind))
}

func (s *SimpleMiniArray) Len() int {
	return len(s.Data)
}

func (s *SimpleMiniArray) Get(index int) (MiniObj, error) {
	if index < 0 || index >= len(s.Data) {
		return nil, nil
	}
	return s.Data[index], nil
}

func (s *SimpleMiniArray) Set(index int, val MiniObj) error {
	if index < 0 || index >= len(s.Data) {
		return nil
	}
	s.Data[index] = val
	return nil
}

func (s *SimpleMiniArray) Append(val MiniObj) error {
	s.Data = append(s.Data, val)
	return nil
}

func (s *SimpleMiniArray) ElemType() GoMiniType {
	return s.ElemKind
}
