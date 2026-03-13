package ast

import (
	"strconv"
)

type MiniUint16 struct {
	data uint16
}

func NewMiniUint16(data uint16) MiniUint16 {
	return MiniUint16{data: data}
}

// Set 更新底层的值
func (o *MiniUint16) Set(data uint16) {
	o.data = data
}

// OPSType 获取类型名
func (o *MiniUint16) OPSType() Ident {
	return "Uint16"
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniUint16) GoValue() any {
	return o.data
}

// Data 返回底层的值
func (o *MiniUint16) Data() uint16 {
	return o.data
}

// String 转换为字符串对象
func (o *MiniUint16) String() MiniString {
	return NewMiniString(strconv.FormatUint(uint64(o.data), 10))
}

// Clone 克隆一个对象
func (o *MiniUint16) Clone() MiniObj {
	return &MiniUint16{data: o.data}
}

// Plus 加法运算
func (o *MiniUint16) Plus(other *MiniUint16) MiniUint16 {
	return MiniUint16{data: o.data + other.data}
}

// Minus 减法运算
func (o *MiniUint16) Minus(other *MiniUint16) MiniUint16 {
	return MiniUint16{data: o.data - other.data}
}

// Mult 乘法运算
func (o *MiniUint16) Mult(other *MiniUint16) MiniUint16 {
	return MiniUint16{data: o.data * other.data}
}

// Div 除法运算
func (o *MiniUint16) Div(other *MiniUint16) MiniUint16 {
	return MiniUint16{data: o.data / other.data}
}

// Mod 取模运算
func (o *MiniUint16) Mod(other *MiniUint16) MiniUint16 {
	return MiniUint16{data: o.data % other.data}
}

// Eq 判断两个对象是否相等
func (o *MiniUint16) Eq(other *MiniUint16) MiniBool {
	return NewMiniBool(o.data == other.data)
}

// Neq 判断两个对象是否不相等
func (o *MiniUint16) Neq(other *MiniUint16) MiniBool {
	return NewMiniBool(o.data != other.data)
}

// Lt 判断是否小于
func (o *MiniUint16) Lt(other *MiniUint16) MiniBool {
	return NewMiniBool(o.data < other.data)
}

// Gt 判断是否大于
func (o *MiniUint16) Gt(other *MiniUint16) MiniBool {
	return NewMiniBool(o.data > other.data)
}

// Le 判断是否小于等于
func (o *MiniUint16) Le(other *MiniUint16) MiniBool {
	return NewMiniBool(o.data <= other.data)
}

// Ge 判断是否大于等于
func (o *MiniUint16) Ge(other *MiniUint16) MiniBool {
	return NewMiniBool(o.data >= other.data)
}

func (o *MiniUint16) New(static string) (MiniObj, error) {
	parsed, err := strconv.ParseUint(static, 10, 64)
	if err != nil {
		return nil, err
	}
	return &MiniUint16{data: uint16(parsed)}, nil
}
