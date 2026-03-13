package ast

import (
	"errors"
	"fmt"
)

type MiniComplex128 struct {
	data complex128
}

func NewMiniComplex128(data complex128) MiniComplex128 {
	return MiniComplex128{data: data}
}

// Set 更新底层的值
func (o *MiniComplex128) Set(data complex128) {
	o.data = data
}

// GoMiniType 获取类型名
func (o *MiniComplex128) GoMiniType() Ident {
	return "Complex128"
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniComplex128) GoValue() any {
	return o.data
}

// Data 返回底层的值
func (o *MiniComplex128) Data() complex128 {
	return o.data
}

// String 转换为字符串对象
func (o *MiniComplex128) String() MiniString {
	return NewMiniString(fmt.Sprintf("%v", o.data))
}

// Clone 克隆一个对象
func (o *MiniComplex128) Clone() MiniObj {
	return &MiniComplex128{data: o.data}
}

// Plus 加法运算
func (o *MiniComplex128) Plus(other *MiniComplex128) MiniComplex128 {
	return MiniComplex128{data: o.data + other.data}
}

// Minus 减法运算
func (o *MiniComplex128) Minus(other *MiniComplex128) MiniComplex128 {
	return MiniComplex128{data: o.data - other.data}
}

// Mult 乘法运算
func (o *MiniComplex128) Mult(other *MiniComplex128) MiniComplex128 {
	return MiniComplex128{data: o.data * other.data}
}

// Div 除法运算
func (o *MiniComplex128) Div(other *MiniComplex128) MiniComplex128 {
	return MiniComplex128{data: o.data / other.data}
}

// Eq 判断两个对象是否相等
func (o *MiniComplex128) Eq(other *MiniComplex128) MiniBool {
	return NewMiniBool(o.data == other.data)
}

// Neq 判断两个对象是否不相等
func (o *MiniComplex128) Neq(other *MiniComplex128) MiniBool {
	return NewMiniBool(o.data != other.data)
}

func (o *MiniComplex128) New(static string) (MiniObj, error) {
	return nil, errors.New("complex parsing not supported")
}
