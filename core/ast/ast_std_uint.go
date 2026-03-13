package ast

import (
	"strconv"
)

type MiniUint struct {
	data uint
}

func NewMiniUint(data uint) MiniUint {
	return MiniUint{data: data}
}

// Set 更新底层的值
func (o *MiniUint) Set(data uint) {
	o.data = data
}

// OPSType 获取类型名
func (o *MiniUint) OPSType() Ident {
	return "Uint"
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniUint) GoValue() any {
	return o.data
}

// Data 返回底层的值
func (o *MiniUint) Data() uint {
	return o.data
}

// String 转换为字符串对象
func (o *MiniUint) String() MiniString {
	return NewMiniString(strconv.FormatUint(uint64(o.data), 10))
}

// Clone 克隆一个对象
func (o *MiniUint) Clone() MiniObj {
	return &MiniUint{data: o.data}
}

// Plus 加法运算
func (o *MiniUint) Plus(other *MiniUint) MiniUint {
	return MiniUint{data: o.data + other.data}
}

// Minus 减法运算
func (o *MiniUint) Minus(other *MiniUint) MiniUint {
	return MiniUint{data: o.data - other.data}
}

// Mult 乘法运算
func (o *MiniUint) Mult(other *MiniUint) MiniUint {
	return MiniUint{data: o.data * other.data}
}

// Div 除法运算
func (o *MiniUint) Div(other *MiniUint) MiniUint {
	return MiniUint{data: o.data / other.data}
}

// Mod 取模运算
func (o *MiniUint) Mod(other *MiniUint) MiniUint {
	return MiniUint{data: o.data % other.data}
}

// Eq 判断两个对象是否相等
func (o *MiniUint) Eq(other *MiniUint) MiniBool {
	return NewMiniBool(o.data == other.data)
}

// Neq 判断两个对象是否不相等
func (o *MiniUint) Neq(other *MiniUint) MiniBool {
	return NewMiniBool(o.data != other.data)
}

// Lt 判断是否小于
func (o *MiniUint) Lt(other *MiniUint) MiniBool {
	return NewMiniBool(o.data < other.data)
}

// Gt 判断是否大于
func (o *MiniUint) Gt(other *MiniUint) MiniBool {
	return NewMiniBool(o.data > other.data)
}

// Le 判断是否小于等于
func (o *MiniUint) Le(other *MiniUint) MiniBool {
	return NewMiniBool(o.data <= other.data)
}

// Ge 判断是否大于等于
func (o *MiniUint) Ge(other *MiniUint) MiniBool {
	return NewMiniBool(o.data >= other.data)
}

func (o *MiniUint) New(static string) (MiniObj, error) {
	parsed, err := strconv.ParseUint(static, 10, 64)
	if err != nil {
		return nil, err
	}
	return &MiniUint{data: uint(parsed)}, nil
}
