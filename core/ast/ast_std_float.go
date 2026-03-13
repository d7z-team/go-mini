package ast

import "strconv"

type MiniFloat struct {
	data float64
}

func NewMiniFloat(data float64) MiniFloat {
	return MiniFloat{data: data}
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniFloat) GoValue() any {
	return o.data
}

// OPSType 获取类型名
func (o *MiniFloat) OPSType() Ident {
	return "Float"
}

// Clone 克隆一个浮点数对象
func (o *MiniFloat) Clone() MiniObj {
	return &MiniFloat{data: o.data}
}

// Plus 加法运算
func (o *MiniFloat) Plus(other *MiniFloat) MiniFloat {
	f := o.data + other.data
	return MiniFloat{data: f}
}

// Minus 减法运算
func (o *MiniFloat) Minus(other *MiniFloat) MiniFloat {
	f := o.data - other.data
	return MiniFloat{data: f}
}

// Mult 乘法运算
func (o *MiniFloat) Mult(other *MiniFloat) MiniFloat {
	f := o.data * other.data
	return MiniFloat{data: f}
}

// Div 除法运算
func (o *MiniFloat) Div(other *MiniFloat) MiniFloat {
	f := o.data / other.data
	return MiniFloat{data: f}
}

// Eq 判断两个浮点数是否相等
func (o *MiniFloat) Eq(other *MiniFloat) MiniBool {
	b := o.data == other.data
	return MiniBool{data: b}
}

// Neq 判断两个浮点数是否不相等
func (o *MiniFloat) Neq(other *MiniFloat) MiniBool {
	b := o.data != other.data
	return MiniBool{data: b}
}

// Lt 判断是否小于
func (o *MiniFloat) Lt(other *MiniFloat) MiniBool {
	b := o.data < other.data
	return MiniBool{data: b}
}

// Gt 判断是否大于
func (o *MiniFloat) Gt(other *MiniFloat) MiniBool {
	b := o.data > other.data
	return MiniBool{data: b}
}

// Le 判断是否小于等于
func (o *MiniFloat) Le(other *MiniFloat) MiniBool {
	b := o.data <= other.data
	return MiniBool{data: b}
}

// Ge 判断是否大于等于
func (o *MiniFloat) Ge(other *MiniFloat) MiniBool {
	b := o.data >= other.data
	return MiniBool{data: b}
}

// Sub 取相反数（负值）
func (o *MiniFloat) Sub() MiniFloat {
	f := -o.data
	return MiniFloat{data: f}
}

// New 内部方法：根据字面量创建对象
func (o *MiniFloat) New(n string) (MiniObj, error) {
	float, err := strconv.ParseFloat(n, 64)
	if err != nil {
		return nil, err
	}
	return &MiniFloat{data: float}, nil
}

// GoString 返回字符串表示形式
func (o *MiniFloat) GoString() string {
	return strconv.FormatFloat(o.data, 'f', -1, 64)
}

// String 转换为字符串对象
func (o *MiniFloat) String() MiniString {
	return NewMiniString(o.GoString())
}
