package ast

import (
	"fmt"
	"strconv"
)

type MiniFloat64 struct {
	data float64
}

func NewMiniFloat64(data float64) MiniFloat64 {
	return MiniFloat64{data: data}
}

// Set 更新底层的值
func (o *MiniFloat64) Set(data float64) {
	o.data = data
}

// GoMiniType 获取类型名
func (o *MiniFloat64) GoMiniType() Ident {
	return "Float64"
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniFloat64) GoValue() any {
	return o.data
}

// Data 返回底层的值
func (o *MiniFloat64) Data() float64 {
	return o.data
}

// String 转换为字符串对象
func (o *MiniFloat64) String() MiniString {
	return NewMiniString(fmt.Sprintf("%f", o.data))
}

// Clone 克隆一个对象
func (o *MiniFloat64) Clone() MiniObj {
	return &MiniFloat64{data: o.data}
}

// Plus 加法运算
func (o *MiniFloat64) Plus(other *MiniFloat64) MiniFloat64 {
	return MiniFloat64{data: o.data + other.data}
}

// Minus 减法运算
func (o *MiniFloat64) Minus(other *MiniFloat64) MiniFloat64 {
	return MiniFloat64{data: o.data - other.data}
}

// Mult 乘法运算
func (o *MiniFloat64) Mult(other *MiniFloat64) MiniFloat64 {
	return MiniFloat64{data: o.data * other.data}
}

// Div 除法运算
func (o *MiniFloat64) Div(other *MiniFloat64) MiniFloat64 {
	return MiniFloat64{data: o.data / other.data}
}

// Eq 判断两个对象是否相等
func (o *MiniFloat64) Eq(other *MiniFloat64) MiniBool {
	return NewMiniBool(o.data == other.data)
}

// Neq 判断两个对象是否不相等
func (o *MiniFloat64) Neq(other *MiniFloat64) MiniBool {
	return NewMiniBool(o.data != other.data)
}

// Lt 判断是否小于
func (o *MiniFloat64) Lt(other *MiniFloat64) MiniBool {
	return NewMiniBool(o.data < other.data)
}

// Gt 判断是否大于
func (o *MiniFloat64) Gt(other *MiniFloat64) MiniBool {
	return NewMiniBool(o.data > other.data)
}

// Le 判断是否小于等于
func (o *MiniFloat64) Le(other *MiniFloat64) MiniBool {
	return NewMiniBool(o.data <= other.data)
}

// Ge 判断是否大于等于
func (o *MiniFloat64) Ge(other *MiniFloat64) MiniBool {
	return NewMiniBool(o.data >= other.data)
}

func (o *MiniFloat64) New(static string) (MiniObj, error) {
	parsed, err := strconv.ParseFloat(static, 64)
	if err != nil {
		return nil, err
	}
	return &MiniFloat64{data: float64(parsed)}, nil
}
