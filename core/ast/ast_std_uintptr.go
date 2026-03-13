package ast

import (
	"strconv"
)

type MiniUintptr struct {
	data uintptr
}

func NewMiniUintptr(data uintptr) MiniUintptr {
	return MiniUintptr{data: data}
}

// Set 更新底层的值
func (o *MiniUintptr) Set(data uintptr) {
	o.data = data
}

// GoMiniType 获取类型名
func (o *MiniUintptr) GoMiniType() Ident {
	return "Uintptr"
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniUintptr) GoValue() any {
	return o.data
}

// Data 返回底层的值
func (o *MiniUintptr) Data() uintptr {
	return o.data
}

// String 转换为字符串对象
func (o *MiniUintptr) String() MiniString {
	return NewMiniString(strconv.FormatUint(uint64(o.data), 10))
}

// Clone 克隆一个对象
func (o *MiniUintptr) Clone() MiniObj {
	return &MiniUintptr{data: o.data}
}

// Plus 加法运算
func (o *MiniUintptr) Plus(other *MiniUintptr) MiniUintptr {
	return MiniUintptr{data: o.data + other.data}
}

// Minus 减法运算
func (o *MiniUintptr) Minus(other *MiniUintptr) MiniUintptr {
	return MiniUintptr{data: o.data - other.data}
}

// Mult 乘法运算
func (o *MiniUintptr) Mult(other *MiniUintptr) MiniUintptr {
	return MiniUintptr{data: o.data * other.data}
}

// Div 除法运算
func (o *MiniUintptr) Div(other *MiniUintptr) MiniUintptr {
	return MiniUintptr{data: o.data / other.data}
}

// Mod 取模运算
func (o *MiniUintptr) Mod(other *MiniUintptr) MiniUintptr {
	return MiniUintptr{data: o.data % other.data}
}

// Eq 判断两个对象是否相等
func (o *MiniUintptr) Eq(other *MiniUintptr) MiniBool {
	return NewMiniBool(o.data == other.data)
}

// Neq 判断两个对象是否不相等
func (o *MiniUintptr) Neq(other *MiniUintptr) MiniBool {
	return NewMiniBool(o.data != other.data)
}

// Lt 判断是否小于
func (o *MiniUintptr) Lt(other *MiniUintptr) MiniBool {
	return NewMiniBool(o.data < other.data)
}

// Gt 判断是否大于
func (o *MiniUintptr) Gt(other *MiniUintptr) MiniBool {
	return NewMiniBool(o.data > other.data)
}

// Le 判断是否小于等于
func (o *MiniUintptr) Le(other *MiniUintptr) MiniBool {
	return NewMiniBool(o.data <= other.data)
}

// Ge 判断是否大于等于
func (o *MiniUintptr) Ge(other *MiniUintptr) MiniBool {
	return NewMiniBool(o.data >= other.data)
}

func (o *MiniUintptr) New(static string) (MiniObj, error) {
	parsed, err := strconv.ParseUint(static, 10, 64)
	if err != nil {
		return nil, err
	}
	return &MiniUintptr{data: uintptr(parsed)}, nil
}
