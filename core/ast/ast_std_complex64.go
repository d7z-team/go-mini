package ast

import (
	"fmt"
)

type MiniComplex64 struct {
	data complex64
}

func NewMiniComplex64(data complex64) MiniComplex64 {
	return MiniComplex64{data: data}
}

// Set 更新底层的值
func (o *MiniComplex64) Set(data complex64) {
	o.data = data
}

// OPSType 获取类型名
func (o *MiniComplex64) OPSType() Ident {
	return "Complex64"
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniComplex64) GoValue() any {
	return o.data
}

// Data 返回底层的值
func (o *MiniComplex64) Data() complex64 {
	return o.data
}

// String 转换为字符串对象
func (o *MiniComplex64) String() MiniString {
	return NewMiniString(fmt.Sprintf("%v", o.data))
}

// Clone 克隆一个对象
func (o *MiniComplex64) Clone() MiniObj {
	return &MiniComplex64{data: o.data}
}

// Plus 加法运算
func (o *MiniComplex64) Plus(other *MiniComplex64) MiniComplex64 {
	return MiniComplex64{data: o.data + other.data}
}

// Minus 减法运算
func (o *MiniComplex64) Minus(other *MiniComplex64) MiniComplex64 {
	return MiniComplex64{data: o.data - other.data}
}

// Mult 乘法运算
func (o *MiniComplex64) Mult(other *MiniComplex64) MiniComplex64 {
	return MiniComplex64{data: o.data * other.data}
}

// Div 除法运算
func (o *MiniComplex64) Div(other *MiniComplex64) MiniComplex64 {
	return MiniComplex64{data: o.data / other.data}
}

// Eq 判断两个对象是否相等
func (o *MiniComplex64) Eq(other *MiniComplex64) MiniBool {
	return NewMiniBool(o.data == other.data)
}

// Neq 判断两个对象是否不相等
func (o *MiniComplex64) Neq(other *MiniComplex64) MiniBool {
	return NewMiniBool(o.data != other.data)
}

func (o *MiniComplex64) New(static string) (MiniObj, error) {
	return nil, fmt.Errorf("complex parsing not supported")
}
