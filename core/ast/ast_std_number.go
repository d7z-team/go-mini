package ast

import (
	"math/rand"
	"strconv"
	"time"
)

type MiniNumber struct {
	data int64
}

func NewMiniNumber(data int64) MiniNumber {
	return MiniNumber{data: data}
}

// Set 更新底层的数字值
func (o *MiniNumber) Set(data int64) {
	o.data = data
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniNumber) GoValue() any {
	return o.data
}

// OPSType 获取类型名
func (o *MiniNumber) OPSType() Ident {
	return "Number"
}

// Clone 克隆一个数字对象
func (o *MiniNumber) Clone() MiniObj {
	return &MiniNumber{data: o.data}
}

// Plus 加法运算
func (o *MiniNumber) Plus(other *MiniNumber) MiniNumber {
	n := o.data + other.data
	return MiniNumber{data: n}
}

// Minus 减法运算
func (o *MiniNumber) Minus(other *MiniNumber) MiniNumber {
	n := o.data - other.data
	return MiniNumber{data: n}
}

// Mult 乘法运算
func (o *MiniNumber) Mult(other *MiniNumber) MiniNumber {
	n := o.data * other.data
	return MiniNumber{data: n}
}

// Div 除法运算
func (o *MiniNumber) Div(other *MiniNumber) MiniNumber {
	n := o.data / other.data
	return MiniNumber{data: n}
}

// Mod 取模运算
func (o *MiniNumber) Mod(other *MiniNumber) MiniNumber {
	n := o.data % other.data
	return MiniNumber{data: n}
}

// Eq 判断两个数字是否相等
func (o *MiniNumber) Eq(other *MiniNumber) MiniBool {
	b := o.data == other.data
	return MiniBool{data: b}
}

// Neq 判断两个数字是否不相等
func (o *MiniNumber) Neq(other *MiniNumber) MiniBool {
	b := o.data != other.data
	return MiniBool{data: b}
}

// Lt 判断是否小于
func (o *MiniNumber) Lt(other *MiniNumber) MiniBool {
	b := o.data < other.data
	return MiniBool{data: b}
}

// Gt 判断是否大于
func (o *MiniNumber) Gt(other *MiniNumber) MiniBool {
	b := o.data > other.data
	return MiniBool{data: b}
}

// Le 判断是否小于等于
func (o *MiniNumber) Le(other *MiniNumber) MiniBool {
	b := o.data <= other.data
	return MiniBool{data: b}
}

// Ge 判断是否大于等于
func (o *MiniNumber) Ge(other *MiniNumber) MiniBool {
	b := o.data >= other.data
	return MiniBool{data: b}
}

// Sub 取相反数（负值）
func (o *MiniNumber) Sub() MiniNumber {
	n := -o.data
	return MiniNumber{data: n}
}

// String 转换为字符串对象
func (o *MiniNumber) String() MiniString {
	return NewMiniString(o.GoString())
}

// GoString 返回字符串表示形式
func (o *MiniNumber) GoString() string {
	return strconv.FormatInt(o.data, 10)
}

// New 内部方法：根据字面量创建对象
func (o *MiniNumber) New(n string) (MiniObj, error) {
	atoi, err := strconv.Atoi(n)
	if err != nil {
		return nil, err
	}
	return &MiniNumber{data: int64(atoi)}, nil
}

// RandomInt 生成指定范围内的随机整数
func (o *MiniNumber) RandomInt(ltNum, gtNum *MiniNumber) MiniNumber {
	if ltNum.data > gtNum.data {
		return MiniNumber{data: -1}
	}
	time.Sleep(time.Millisecond * 3)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return MiniNumber{data: r.Int63n(gtNum.data-ltNum.data+1) + ltNum.data}
}
