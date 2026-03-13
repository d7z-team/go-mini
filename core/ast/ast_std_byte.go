package ast

import (
	"fmt"
	"strconv"
)

type MiniByte struct {
	data byte
}

func NewMiniByte(data byte) MiniByte {
	return MiniByte{data: data}
}

// Set 更新底层的 byte 值
func (o *MiniByte) Set(data byte) {
	o.data = data
}

// OPSType 获取类型名
func (o *MiniByte) OPSType() Ident {
	return "Byte"
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniByte) GoValue() any {
	return o.data
}

// Data 返回底层的 byte 值
func (o *MiniByte) Data() byte {
	return o.data
}

// String 转换为字符串对象
func (o *MiniByte) String() MiniString {
	return NewMiniString(fmt.Sprintf("%d", o.data))
}

// Clone 克隆一个 byte 对象
func (o *MiniByte) Clone() MiniObj {
	return &MiniByte{data: o.data}
}

// Eq 判断两个 byte 是否相等
func (o *MiniByte) Eq(other *MiniByte) MiniBool {
	return NewMiniBool(o.data == other.data)
}

// Neq 判断两个 byte 是否不相等
func (o *MiniByte) Neq(other *MiniByte) MiniBool {
	return NewMiniBool(o.data != other.data)
}

func (o *MiniByte) New(static string) (MiniObj, error) {
	parseInt, err := strconv.ParseUint(static, 10, 8)
	if err != nil {
		return nil, err
	}
	return &MiniByte{data: byte(parseInt)}, nil
}
