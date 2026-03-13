package ast

import (
	"strconv"
)

type MiniUint64 struct {
	data uint64
}

func NewMiniUint64(data uint64) MiniUint64 {
	return MiniUint64{data: data}
}

// Set 更新底层的值
func (o *MiniUint64) Set(data uint64) {
	o.data = data
}

// GoMiniType 获取类型名
func (o *MiniUint64) GoMiniType() Ident {
	return "Uint64"
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniUint64) GoValue() any {
	return o.data
}

// Data 返回底层的值
func (o *MiniUint64) Data() uint64 {
	return o.data
}

// String 转换为字符串对象
func (o *MiniUint64) String() MiniString {
	return NewMiniString(strconv.FormatUint(uint64(o.data), 10))
}

// Clone 克隆一个对象
func (o *MiniUint64) Clone() MiniObj {
	return &MiniUint64{data: o.data}
}

// Plus 加法运算
func (o *MiniUint64) Plus(other *MiniUint64) MiniUint64 {
	return MiniUint64{data: o.data + other.data}
}

// Minus 减法运算
func (o *MiniUint64) Minus(other *MiniUint64) MiniUint64 {
	return MiniUint64{data: o.data - other.data}
}

// Mult 乘法运算
func (o *MiniUint64) Mult(other *MiniUint64) MiniUint64 {
	return MiniUint64{data: o.data * other.data}
}

// Div 除法运算
func (o *MiniUint64) Div(other *MiniUint64) MiniUint64 {
	return MiniUint64{data: o.data / other.data}
}

// Mod 取模运算
func (o *MiniUint64) Mod(other *MiniUint64) MiniUint64 {
	return MiniUint64{data: o.data % other.data}
}

// Eq 判断两个对象是否相等
func (o *MiniUint64) Eq(other *MiniUint64) MiniBool {
	return NewMiniBool(o.data == other.data)
}

// Neq 判断两个对象是否不相等
func (o *MiniUint64) Neq(other *MiniUint64) MiniBool {
	return NewMiniBool(o.data != other.data)
}

// Lt 判断是否小于
func (o *MiniUint64) Lt(other *MiniUint64) MiniBool {
	return NewMiniBool(o.data < other.data)
}

// Gt 判断是否大于
func (o *MiniUint64) Gt(other *MiniUint64) MiniBool {
	return NewMiniBool(o.data > other.data)
}

// Le 判断是否小于等于
func (o *MiniUint64) Le(other *MiniUint64) MiniBool {
	return NewMiniBool(o.data <= other.data)
}

// Ge 判断是否大于等于
func (o *MiniUint64) Ge(other *MiniUint64) MiniBool {
	return NewMiniBool(o.data >= other.data)
}

func (o *MiniUint64) New(static string) (MiniObj, error) {
	parsed, err := strconv.ParseUint(static, 10, 64)
	if err != nil {
		return nil, err
	}
	return &MiniUint64{data: uint64(parsed)}, nil
}
