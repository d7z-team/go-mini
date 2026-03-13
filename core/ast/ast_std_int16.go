package ast

import (
	"strconv"
)

type MiniInt16 struct {
	data int16
}

func NewMiniInt16(data int16) MiniInt16 {
	return MiniInt16{data: data}
}

// Set 更新底层的值
func (o *MiniInt16) Set(data int16) {
	o.data = data
}

// GoMiniType 获取类型名
func (o *MiniInt16) GoMiniType() Ident {
	return "Int16"
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniInt16) GoValue() any {
	return o.data
}

// Data 返回底层的值
func (o *MiniInt16) Data() int16 {
	return o.data
}

// String 转换为字符串对象
func (o *MiniInt16) String() MiniString {
	return NewMiniString(strconv.FormatInt(int64(o.data), 10))
}

// Clone 克隆一个对象
func (o *MiniInt16) Clone() MiniObj {
	return &MiniInt16{data: o.data}
}

// Plus 加法运算
func (o *MiniInt16) Plus(other *MiniInt16) MiniInt16 {
	return MiniInt16{data: o.data + other.data}
}

// Minus 减法运算
func (o *MiniInt16) Minus(other *MiniInt16) MiniInt16 {
	return MiniInt16{data: o.data - other.data}
}

// Mult 乘法运算
func (o *MiniInt16) Mult(other *MiniInt16) MiniInt16 {
	return MiniInt16{data: o.data * other.data}
}

// Div 除法运算
func (o *MiniInt16) Div(other *MiniInt16) MiniInt16 {
	return MiniInt16{data: o.data / other.data}
}

// Mod 取模运算
func (o *MiniInt16) Mod(other *MiniInt16) MiniInt16 {
	return MiniInt16{data: o.data % other.data}
}

// Eq 判断两个对象是否相等
func (o *MiniInt16) Eq(other *MiniInt16) MiniBool {
	return NewMiniBool(o.data == other.data)
}

// Neq 判断两个对象是否不相等
func (o *MiniInt16) Neq(other *MiniInt16) MiniBool {
	return NewMiniBool(o.data != other.data)
}

// Lt 判断是否小于
func (o *MiniInt16) Lt(other *MiniInt16) MiniBool {
	return NewMiniBool(o.data < other.data)
}

// Gt 判断是否大于
func (o *MiniInt16) Gt(other *MiniInt16) MiniBool {
	return NewMiniBool(o.data > other.data)
}

// Le 判断是否小于等于
func (o *MiniInt16) Le(other *MiniInt16) MiniBool {
	return NewMiniBool(o.data <= other.data)
}

// Ge 判断是否大于等于
func (o *MiniInt16) Ge(other *MiniInt16) MiniBool {
	return NewMiniBool(o.data >= other.data)
}

func (o *MiniInt16) New(static string) (MiniObj, error) {
	parsed, err := strconv.ParseInt(static, 10, 64)
	if err != nil {
		return nil, err
	}
	return &MiniInt16{data: int16(parsed)}, nil
}
