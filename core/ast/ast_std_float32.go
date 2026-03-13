package ast

import (
	"fmt"
	"strconv"
)

type MiniFloat32 struct {
	data float32
}

func NewMiniFloat32(data float32) MiniFloat32 {
	return MiniFloat32{data: data}
}

// Set 更新底层的值
func (o *MiniFloat32) Set(data float32) {
	o.data = data
}

// OPSType 获取类型名
func (o *MiniFloat32) OPSType() Ident {
	return "Float32"
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniFloat32) GoValue() any {
	return o.data
}

// Data 返回底层的值
func (o *MiniFloat32) Data() float32 {
	return o.data
}

// String 转换为字符串对象
func (o *MiniFloat32) String() MiniString {
	return NewMiniString(fmt.Sprintf("%f", o.data))
}

// Clone 克隆一个对象
func (o *MiniFloat32) Clone() MiniObj {
	return &MiniFloat32{data: o.data}
}

// Plus 加法运算
func (o *MiniFloat32) Plus(other *MiniFloat32) MiniFloat32 {
	return MiniFloat32{data: o.data + other.data}
}

// Minus 减法运算
func (o *MiniFloat32) Minus(other *MiniFloat32) MiniFloat32 {
	return MiniFloat32{data: o.data - other.data}
}

// Mult 乘法运算
func (o *MiniFloat32) Mult(other *MiniFloat32) MiniFloat32 {
	return MiniFloat32{data: o.data * other.data}
}

// Div 除法运算
func (o *MiniFloat32) Div(other *MiniFloat32) MiniFloat32 {
	return MiniFloat32{data: o.data / other.data}
}

// Eq 判断两个对象是否相等
func (o *MiniFloat32) Eq(other *MiniFloat32) MiniBool {
	return NewMiniBool(o.data == other.data)
}

// Neq 判断两个对象是否不相等
func (o *MiniFloat32) Neq(other *MiniFloat32) MiniBool {
	return NewMiniBool(o.data != other.data)
}

// Lt 判断是否小于
func (o *MiniFloat32) Lt(other *MiniFloat32) MiniBool {
	return NewMiniBool(o.data < other.data)
}

// Gt 判断是否大于
func (o *MiniFloat32) Gt(other *MiniFloat32) MiniBool {
	return NewMiniBool(o.data > other.data)
}

// Le 判断是否小于等于
func (o *MiniFloat32) Le(other *MiniFloat32) MiniBool {
	return NewMiniBool(o.data <= other.data)
}

// Ge 判断是否大于等于
func (o *MiniFloat32) Ge(other *MiniFloat32) MiniBool {
	return NewMiniBool(o.data >= other.data)
}

func (o *MiniFloat32) New(static string) (MiniObj, error) {
	parsed, err := strconv.ParseFloat(static, 32)
	if err != nil {
		return nil, err
	}
	return &MiniFloat32{data: float32(parsed)}, nil
}
