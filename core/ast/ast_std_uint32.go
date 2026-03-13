package ast

import (
	"strconv"
)

type MiniUint32 struct {
	data uint32
}

func NewMiniUint32(data uint32) MiniUint32 {
	return MiniUint32{data: data}
}

// Set 更新底层的值
func (o *MiniUint32) Set(data uint32) {
	o.data = data
}

// OPSType 获取类型名
func (o *MiniUint32) OPSType() Ident {
	return "Uint32"
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniUint32) GoValue() any {
	return o.data
}

// Data 返回底层的值
func (o *MiniUint32) Data() uint32 {
	return o.data
}

// String 转换为字符串对象
func (o *MiniUint32) String() MiniString {
	return NewMiniString(strconv.FormatUint(uint64(o.data), 10))
}

// Clone 克隆一个对象
func (o *MiniUint32) Clone() MiniObj {
	return &MiniUint32{data: o.data}
}

// Plus 加法运算
func (o *MiniUint32) Plus(other *MiniUint32) MiniUint32 {
	return MiniUint32{data: o.data + other.data}
}

// Minus 减法运算
func (o *MiniUint32) Minus(other *MiniUint32) MiniUint32 {
	return MiniUint32{data: o.data - other.data}
}

// Mult 乘法运算
func (o *MiniUint32) Mult(other *MiniUint32) MiniUint32 {
	return MiniUint32{data: o.data * other.data}
}

// Div 除法运算
func (o *MiniUint32) Div(other *MiniUint32) MiniUint32 {
	return MiniUint32{data: o.data / other.data}
}

// Mod 取模运算
func (o *MiniUint32) Mod(other *MiniUint32) MiniUint32 {
	return MiniUint32{data: o.data % other.data}
}

// Eq 判断两个对象是否相等
func (o *MiniUint32) Eq(other *MiniUint32) MiniBool {
	return NewMiniBool(o.data == other.data)
}

// Neq 判断两个对象是否不相等
func (o *MiniUint32) Neq(other *MiniUint32) MiniBool {
	return NewMiniBool(o.data != other.data)
}

// Lt 判断是否小于
func (o *MiniUint32) Lt(other *MiniUint32) MiniBool {
	return NewMiniBool(o.data < other.data)
}

// Gt 判断是否大于
func (o *MiniUint32) Gt(other *MiniUint32) MiniBool {
	return NewMiniBool(o.data > other.data)
}

// Le 判断是否小于等于
func (o *MiniUint32) Le(other *MiniUint32) MiniBool {
	return NewMiniBool(o.data <= other.data)
}

// Ge 判断是否大于等于
func (o *MiniUint32) Ge(other *MiniUint32) MiniBool {
	return NewMiniBool(o.data >= other.data)
}

func (o *MiniUint32) New(static string) (MiniObj, error) {
	parsed, err := strconv.ParseUint(static, 10, 64)
	if err != nil {
		return nil, err
	}
	return &MiniUint32{data: uint32(parsed)}, nil
}
