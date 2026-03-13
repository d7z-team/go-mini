package ast

import (
	"strconv"
)

type MiniUint8 struct {
	data byte
}

func NewMiniUint8(data byte) MiniUint8 {
	return MiniUint8{data: data}
}

// Set 更新底层的 byte 值
func (o *MiniUint8) Set(data byte) {
	o.data = data
}

// OPSType 获取类型名
func (o *MiniUint8) OPSType() Ident {
	return "Uint8"
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniUint8) GoValue() any {
	return o.data
}

// Data 返回底层的 byte 值
func (o *MiniUint8) Data() byte {
	return o.data
}

// String 转换为字符串对象
func (o *MiniUint8) String() MiniString {
	return NewMiniString(strconv.FormatUint(uint64(o.data), 10))
}

// Clone 克隆一个 byte 对象
func (o *MiniUint8) Clone() MiniObj {
	return &MiniUint8{data: o.data}
}

// Eq 判断两个 byte 是否相等
func (o *MiniUint8) Eq(other *MiniUint8) MiniBool {
	return NewMiniBool(o.data == other.data)
}

// Neq 判断两个 byte 是否不相等
func (o *MiniUint8) Neq(other *MiniUint8) MiniBool {
	return NewMiniBool(o.data != other.data)
}

func (o *MiniUint8) New(static string) (MiniObj, error) {
	parseInt, err := strconv.ParseUint(static, 10, 8)
	if err != nil {
		return nil, err
	}
	return &MiniUint8{data: byte(parseInt)}, nil
}
