package ast

import (
	"strconv"
)

type MiniInt struct {
	data int
}

func NewMiniInt(data int) MiniInt {
	return MiniInt{data: data}
}

// Set 更新底层的值
func (o *MiniInt) Set(data int) {
	o.data = data
}

// GoMiniType 获取类型名
func (o *MiniInt) GoMiniType() Ident {
	return "Int"
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniInt) GoValue() any {
	return o.data
}

// Data 返回底层的值
func (o *MiniInt) Data() int {
	return o.data
}

// String 转换为字符串对象
func (o *MiniInt) String() MiniString {
	return NewMiniString(strconv.FormatInt(int64(o.data), 10))
}

// Clone 克隆一个对象
func (o *MiniInt) Clone() MiniObj {
	return &MiniInt{data: o.data}
}

// Plus 加法运算
func (o *MiniInt) Plus(other *MiniInt) MiniInt {
	return MiniInt{data: o.data + other.data}
}

// Minus 减法运算
func (o *MiniInt) Minus(other *MiniInt) MiniInt {
	return MiniInt{data: o.data - other.data}
}

// Mult 乘法运算
func (o *MiniInt) Mult(other *MiniInt) MiniInt {
	return MiniInt{data: o.data * other.data}
}

// Div 除法运算
func (o *MiniInt) Div(other *MiniInt) MiniInt {
	return MiniInt{data: o.data / other.data}
}

// Mod 取模运算
func (o *MiniInt) Mod(other *MiniInt) MiniInt {
	return MiniInt{data: o.data % other.data}
}

// Eq 判断两个对象是否相等
func (o *MiniInt) Eq(other *MiniInt) MiniBool {
	return NewMiniBool(o.data == other.data)
}

// Neq 判断两个对象是否不相等
func (o *MiniInt) Neq(other *MiniInt) MiniBool {
	return NewMiniBool(o.data != other.data)
}

// Lt 判断是否小于
func (o *MiniInt) Lt(other *MiniInt) MiniBool {
	return NewMiniBool(o.data < other.data)
}

// Gt 判断是否大于
func (o *MiniInt) Gt(other *MiniInt) MiniBool {
	return NewMiniBool(o.data > other.data)
}

// Le 判断是否小于等于
func (o *MiniInt) Le(other *MiniInt) MiniBool {
	return NewMiniBool(o.data <= other.data)
}

// Ge 判断是否大于等于
func (o *MiniInt) Ge(other *MiniInt) MiniBool {
	return NewMiniBool(o.data >= other.data)
}

func (o *MiniInt) New(static string) (MiniObj, error) {
	parsed, err := strconv.ParseInt(static, 10, 64)
	if err != nil {
		return nil, err
	}
	return &MiniInt{data: int(parsed)}, nil
}
