package ast

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

type MiniString struct {
	data string
}

func NewMiniString(data string) MiniString {
	return MiniString{data: data}
}

// Set 更新底层的 Go 字符串
func (o *MiniString) Set(data string) {
	o.data = data
}

// GoMiniType 获取类型名
func (o *MiniString) GoMiniType() Ident {
	return "String"
}

// GoString 返回底层的 Go 字符串
func (o *MiniString) GoString() string {
	return o.data
}

// GoValue 返回底层的 Go 任意类型值
func (o *MiniString) GoValue() any {
	return o.data
}

// String 转换为字符串对象
func (o *MiniString) String() MiniString {
	return NewMiniString(o.data)
}

// Clone 克隆一个字符串对象
func (o *MiniString) Clone() MiniObj {
	return &MiniString{data: o.data}
}

// Plus 字符串拼接
func (o *MiniString) Plus(other *MiniString) MiniString {
	s := o.data + other.data
	return MiniString{data: s}
}

// Eq 判断两个字符串是否相等
func (o *MiniString) Eq(other *MiniString) MiniBool {
	return MiniBool{data: o.data == other.data}
}

// Neq 判断两个字符串是否不相等
func (o *MiniString) Neq(other *MiniString) MiniBool {
	return MiniBool{data: o.data != other.data}
}

// HasPrefix 判断字符串是否以指定前缀开头
func (o *MiniString) HasPrefix(other *MiniString) MiniBool {
	return MiniBool{data: strings.HasPrefix(o.data, other.data)}
}

// New 内部方法：根据字面量创建对象
func (o *MiniString) New(static string) (MiniObj, error) {
	return &MiniString{data: static}, nil
}

// Base64解码
// Base64Decode Base64 解码为普通字符串
func (o *MiniString) Base64Decode() MiniString {
	res, err := base64.StdEncoding.DecodeString(o.data)
	if err != nil {
		return MiniString{data: ""}
	}
	return MiniString{data: string(res)}
}

// Base64编码
// Base64Encode 将字符串进行 Base64 编码
func (o *MiniString) Base64Encode() MiniString {
	return MiniString{data: base64.StdEncoding.EncodeToString([]byte(o.data))}
}

// 文本压缩转换成Json对象
// Text2Json 将 JSON 格式的字符串解析为 Json 对象
func (o *MiniString) Text2Json() MiniString {
	var js json.RawMessage
	err := json.Unmarshal([]byte(o.data), &js)
	if err != nil {
		return MiniString{data: ""}
	}
	return MiniString{data: string(js)}
}

// Json转换成文本
// JSON2Text 将 Json 对象格式化为字符串
func (o *MiniString) JSON2Text() MiniString {
	jsonBytes, err := json.Marshal(o.data)
	if err != nil {
		return MiniString{data: ""}
	}
	return MiniString{data: string(jsonBytes)}
}

// GetData 根据 JSON Path 获取对象中的数据
func (o *MiniString) GetData(other *MiniString) MiniString {
	var js json.RawMessage
	err := json.Unmarshal([]byte(o.data), &js)
	if err != nil {
		return MiniString{data: ""}
	}
	return MiniString{data: string(js)}
}

// 获取文本长度
// Len 获取字符串的字符长度
func (o *MiniString) Len() MiniInt64 {
	return MiniInt64{data: int64(len(o.data))}
}

// 追加新文本
// Append 追加新文本，可指定是否换行
func (o *MiniString) Append(other *MiniString, lineFlag *MiniInt64) MiniString {
	if lineFlag.data == 1 {
		return MiniString{data: o.data + "\n" + other.data}
	}
	return MiniString{data: o.data + other.data}
}

// 截取一段文本
// Substring 截取指定范围的子字符串
func (o *MiniString) Substring(start, length *MiniInt64) MiniString {
	if length.data < 0 {
		length.data = int64(len(o.data))
	}
	return MiniString{data: o.data[start.data:length.data]}
}

// 补齐文本填充到指定长度
// Pad 按照指定长度和字符填充字符串
func (o *MiniString) Pad(fillText *MiniString, length, fillLocation *MiniInt64) MiniString {
	if length.data <= o.Len().data {
		return MiniString{data: o.data}
	}
	repeatCount := int(length.data-o.Len().data)/int(fillText.Len().data) + 1
	if fillLocation.data == 1 {
		res := MiniString{data: strings.Repeat(fillText.data, repeatCount) + o.data}
		return MiniString{data: res.data[int(res.Len().data-length.data):]}
	}
	res := MiniString{data: o.data + strings.Repeat(fillText.data, repeatCount)}
	return MiniString{data: res.data[:length.data]}
}

// 改变文本的大小写
// ChangeCase 转换字符串大小写（1为大写，2为小写）
func (o *MiniString) ChangeCase(flag *MiniInt64) MiniString {
	switch flag.data {
	case 1:
		return MiniString{data: strings.ToUpper(o.data)}
	case 2:
		return MiniString{data: strings.ToLower(o.data)}
	}
	return MiniString{data: o.data}
}

// Trim 去除字符串首尾的指定字符
func (o *MiniString) Trim(cutset *MiniString) MiniString {
	return MiniString{data: strings.Trim(o.data, cutset.data)}
}

// Contains 判断字符串是否包含子串
func (o *MiniString) Contains(other *MiniString) MiniBool {
	return MiniBool{data: strings.Contains(o.data, other.data)}
}

// HasSuffix 判断字符串是否以指定后缀结尾
func (o *MiniString) HasSuffix(other *MiniString) MiniBool {
	return MiniBool{data: strings.HasSuffix(o.data, other.data)}
}

// Replace 替换字符串中的子串 (指定次数)
func (o *MiniString) Replace(oldText, newText *MiniString, n *MiniInt64) MiniString {
	return MiniString{data: strings.Replace(o.data, oldText.data, newText.data, int(n.data))}
}

// ReplaceAll 替换字符串中的所有匹配项
func (o *MiniString) ReplaceAll(oldText, newText *MiniString) MiniString {
	return MiniString{data: strings.ReplaceAll(o.data, oldText.data, newText.data)}
}

// Split 分割字符串
func (o *MiniString) Split(sep *MiniString) []interface{} {
	parts := strings.Split(o.data, sep.data)
	res := make([]interface{}, len(parts))
	for i, p := range parts {
		res[i] = NewMiniString(p)
	}
	return res
}

// Index 获取子串第一次出现的位置
func (o *MiniString) Index(other *MiniString) MiniInt64 {
	return MiniInt64{data: int64(strings.Index(o.data, other.data))}
}

// Repeat 重复字符串
func (o *MiniString) Repeat(count *MiniInt64) MiniString {
	return MiniString{data: strings.Repeat(o.data, int(count.data))}
}

// TrimLeft 去除左侧指定字符
func (o *MiniString) TrimLeft(cutset *MiniString) MiniString {
	return MiniString{data: strings.TrimLeft(o.data, cutset.data)}
}

// TrimRight 去除右侧指定字符
func (o *MiniString) TrimRight(cutset *MiniString) MiniString {
	return MiniString{data: strings.TrimRight(o.data, cutset.data)}
}

// TrimPrefix 去除指定前缀
func (o *MiniString) TrimPrefix(prefix *MiniString) MiniString {
	return MiniString{data: strings.TrimPrefix(o.data, prefix.data)}
}

// TrimSuffix 去除指定后缀
func (o *MiniString) TrimSuffix(suffix *MiniString) MiniString {
	return MiniString{data: strings.TrimSuffix(o.data, suffix.data)}
}
