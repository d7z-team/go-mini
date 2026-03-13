package ast

import (
	"strconv"
)

type MiniInt32 struct {
	data int32
}

func NewMiniInt32(data int32) MiniInt32 {
	return MiniInt32{data: data}
}

// Set 更新底层的值
func (o *MiniInt32) Set(data int32) {
	o.data = data
}

// GoMiniType 获取类型名
func (o *MiniInt32) GoMiniType() Ident {
	return "Int32"
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniInt32) GoValue() any {
	return o.data
}

// Data 返回底层的值
func (o *MiniInt32) Data() int32 {
	return o.data
}

// String 转换为字符串对象
func (o *MiniInt32) String() MiniString {
	return NewMiniString(strconv.FormatInt(int64(o.data), 10))
}

// Clone 克隆一个对象
func (o *MiniInt32) Clone() MiniObj {
	return &MiniInt32{data: o.data}
}

// Plus 加法运算
func (o *MiniInt32) Plus(other *MiniInt32) MiniInt32 {
	return MiniInt32{data: o.data + other.data}
}

// Minus 减法运算
func (o *MiniInt32) Minus(other *MiniInt32) MiniInt32 {
	return MiniInt32{data: o.data - other.data}
}

// Mult 乘法运算
func (o *MiniInt32) Mult(other *MiniInt32) MiniInt32 {
	return MiniInt32{data: o.data * other.data}
}

// Div 除法运算
func (o *MiniInt32) Div(other *MiniInt32) MiniInt32 {
	return MiniInt32{data: o.data / other.data}
}

// Mod 取模运算
func (o *MiniInt32) Mod(other *MiniInt32) MiniInt32 {
	return MiniInt32{data: o.data % other.data}
}

// Eq 判断两个对象是否相等
func (o *MiniInt32) Eq(other *MiniInt32) MiniBool {
	return NewMiniBool(o.data == other.data)
}

// Neq 判断两个对象是否不相等
func (o *MiniInt32) Neq(other *MiniInt32) MiniBool {
	return NewMiniBool(o.data != other.data)
}

// Lt 判断是否小于
func (o *MiniInt32) Lt(other *MiniInt32) MiniBool {
	return NewMiniBool(o.data < other.data)
}

// Gt 判断是否大于
func (o *MiniInt32) Gt(other *MiniInt32) MiniBool {
	return NewMiniBool(o.data > other.data)
}

// Le 判断是否小于等于
func (o *MiniInt32) Le(other *MiniInt32) MiniBool {
	return NewMiniBool(o.data <= other.data)
}

// Ge 判断是否大于等于
func (o *MiniInt32) Ge(other *MiniInt32) MiniBool {
	return NewMiniBool(o.data >= other.data)
}

func (o *MiniInt32) New(static string) (MiniObj, error) {
	parsed, err := strconv.ParseInt(static, 10, 64)
	if err != nil {
		return nil, err
	}
	return &MiniInt32{data: int32(parsed)}, nil
}
