package ast

import (
	"strconv"
)

type MiniInt8 struct {
	data int8
}

func NewMiniInt8(data int8) MiniInt8 {
	return MiniInt8{data: data}
}

// Set 更新底层的值
func (o *MiniInt8) Set(data int8) {
	o.data = data
}

// OPSType 获取类型名
func (o *MiniInt8) OPSType() Ident {
	return "Int8"
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniInt8) GoValue() any {
	return o.data
}

// Data 返回底层的值
func (o *MiniInt8) Data() int8 {
	return o.data
}

// String 转换为字符串对象
func (o *MiniInt8) String() MiniString {
	return NewMiniString(strconv.FormatInt(int64(o.data), 10))
}

// Clone 克隆一个对象
func (o *MiniInt8) Clone() MiniObj {
	return &MiniInt8{data: o.data}
}

// Plus 加法运算
func (o *MiniInt8) Plus(other *MiniInt8) MiniInt8 {
	return MiniInt8{data: o.data + other.data}
}

// Minus 减法运算
func (o *MiniInt8) Minus(other *MiniInt8) MiniInt8 {
	return MiniInt8{data: o.data - other.data}
}

// Mult 乘法运算
func (o *MiniInt8) Mult(other *MiniInt8) MiniInt8 {
	return MiniInt8{data: o.data * other.data}
}

// Div 除法运算
func (o *MiniInt8) Div(other *MiniInt8) MiniInt8 {
	return MiniInt8{data: o.data / other.data}
}

// Mod 取模运算
func (o *MiniInt8) Mod(other *MiniInt8) MiniInt8 {
	return MiniInt8{data: o.data % other.data}
}

// Eq 判断两个对象是否相等
func (o *MiniInt8) Eq(other *MiniInt8) MiniBool {
	return NewMiniBool(o.data == other.data)
}

// Neq 判断两个对象是否不相等
func (o *MiniInt8) Neq(other *MiniInt8) MiniBool {
	return NewMiniBool(o.data != other.data)
}

// Lt 判断是否小于
func (o *MiniInt8) Lt(other *MiniInt8) MiniBool {
	return NewMiniBool(o.data < other.data)
}

// Gt 判断是否大于
func (o *MiniInt8) Gt(other *MiniInt8) MiniBool {
	return NewMiniBool(o.data > other.data)
}

// Le 判断是否小于等于
func (o *MiniInt8) Le(other *MiniInt8) MiniBool {
	return NewMiniBool(o.data <= other.data)
}

// Ge 判断是否大于等于
func (o *MiniInt8) Ge(other *MiniInt8) MiniBool {
	return NewMiniBool(o.data >= other.data)
}

func (o *MiniInt8) New(static string) (MiniObj, error) {
	parsed, err := strconv.ParseInt(static, 10, 64)
	if err != nil {
		return nil, err
	}
	return &MiniInt8{data: int8(parsed)}, nil
}
