package ast

import (
	"math/rand"
	"strconv"
	"time"
)

type MiniInt64 struct {
	data int64
}

func NewMiniInt64(data int64) MiniInt64 {
	return MiniInt64{data: data}
}

// Set 更新底层的数字值
func (o *MiniInt64) Set(data int64) {
	o.data = data
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniInt64) GoValue() any {
	return o.data
}

// Data 返回底层的 Go 整数值
func (o *MiniInt64) Data() int64 {
	return o.data
}

// GoMiniType 获取类型名
func (o *MiniInt64) GoMiniType() Ident {
	return "Int64"
}

// Clone 克隆一个数字对象
func (o *MiniInt64) Clone() MiniObj {
	return &MiniInt64{data: o.data}
}

// Plus 加法运算
func (o *MiniInt64) Plus(other *MiniInt64) MiniInt64 {
	n := o.data + other.data
	return MiniInt64{data: n}
}

// Minus 减法运算
func (o *MiniInt64) Minus(other *MiniInt64) MiniInt64 {
	n := o.data - other.data
	return MiniInt64{data: n}
}

// Mult 乘法运算
func (o *MiniInt64) Mult(other *MiniInt64) MiniInt64 {
	n := o.data * other.data
	return MiniInt64{data: n}
}

// Div 除法运算
func (o *MiniInt64) Div(other *MiniInt64) MiniInt64 {
	n := o.data / other.data
	return MiniInt64{data: n}
}

// Mod 取模运算
func (o *MiniInt64) Mod(other *MiniInt64) MiniInt64 {
	n := o.data % other.data
	return MiniInt64{data: n}
}

// Eq 判断两个数字是否相等
func (o *MiniInt64) Eq(other *MiniInt64) MiniBool {
	b := o.data == other.data
	return MiniBool{data: b}
}

// Neq 判断两个数字是否不相等
func (o *MiniInt64) Neq(other *MiniInt64) MiniBool {
	b := o.data != other.data
	return MiniBool{data: b}
}

// Lt 判断是否小于
func (o *MiniInt64) Lt(other *MiniInt64) MiniBool {
	b := o.data < other.data
	return MiniBool{data: b}
}

// Gt 判断是否大于
func (o *MiniInt64) Gt(other *MiniInt64) MiniBool {
	b := o.data > other.data
	return MiniBool{data: b}
}

// Le 判断是否小于等于
func (o *MiniInt64) Le(other *MiniInt64) MiniBool {
	b := o.data <= other.data
	return MiniBool{data: b}
}

// Ge 判断是否大于等于
func (o *MiniInt64) Ge(other *MiniInt64) MiniBool {
	b := o.data >= other.data
	return MiniBool{data: b}
}

// Sub 取相反数（负值）
func (o *MiniInt64) Sub() MiniInt64 {
	n := -o.data
	return MiniInt64{data: n}
}

// BitwiseNot 按位取反操作
func (o *MiniInt64) BitwiseNot() MiniInt64 {
	return MiniInt64{data: ^o.data}
}

// String 转换为字符串对象
func (o *MiniInt64) String() MiniString {
	return NewMiniString(o.GoString())
}

// GoString 返回字符串表示形式
func (o *MiniInt64) GoString() string {
	return strconv.FormatInt(o.data, 10)
}

// New 内部方法：根据字面量创建对象
func (o *MiniInt64) New(n string) (MiniObj, error) {
	atoi, err := strconv.Atoi(n)
	if err != nil {
		return nil, err
	}
	return &MiniInt64{data: int64(atoi)}, nil
}

// RandomInt 生成指定范围内的随机整数
func (o *MiniInt64) RandomInt(ltNum, gtNum *MiniInt64) MiniInt64 {
	if ltNum.data > gtNum.data {
		return MiniInt64{data: -1}
	}
	time.Sleep(time.Millisecond * 3)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return MiniInt64{data: r.Int63n(gtNum.data-ltNum.data+1) + ltNum.data}
}
