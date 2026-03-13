package ast

import (
	"errors"
	"strconv"
)

type MiniBool struct {
	data bool
}

func NewMiniBool(data bool) MiniBool {
	return MiniBool{data: data}
}

// GoMiniType 获取类型名
func (o *MiniBool) GoMiniType() Ident {
	return "Bool"
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniBool) GoValue() any {
	return o.data
}

// Clone 克隆一个布尔对象
func (o *MiniBool) Clone() MiniObj {
	return &MiniBool{data: o.data}
}

// And 逻辑与运算
func (o *MiniBool) And(other *MiniBool) MiniBool {
	b := o.data && other.data
	return MiniBool{data: b}
}

// Or 逻辑或运算
func (o *MiniBool) Or(other *MiniBool) MiniBool {
	b := o.data || other.data
	return MiniBool{data: b}
}

// Not 逻辑非运算
func (o *MiniBool) Not() MiniBool {
	b := !o.data
	return MiniBool{data: b}
}

func (o *MiniBool) GoString() string {
	return strconv.FormatBool(o.data)
}

// String 转换为字符串对象
func (o *MiniBool) String() MiniString {
	return NewMiniString(o.GoString())
}

// New 内部方法：根据字面量创建对象
func (o *MiniBool) New(n string) (MiniObj, error) {
	if n != "true" && n != "false" {
		return nil, errors.New("unknown bool :" + n)
	}
	return &MiniBool{data: n == "true"}, nil
}

// Data 获取底层的 Go 布尔值
func (o *MiniBool) Data() bool {
	return o.data
}
