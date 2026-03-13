package test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/go-mini/core/utils"
)

// base64 加解码
func TestDataHandler(t *testing.T) {
	_, err := utils.TestGoCode(`
	func main() {
		str1 := "{\"name\":\"张三\"}"
		res :=str1.Base64Encode()
		println(str1.Len())
		println(str1.Base64Encode())
		println(res.Base64Decode())
	}
	`)
	assert.NoError(t, err)
}

// 随机数获取
func TestRandom(t *testing.T) {
	// todo:重构流程
	t.Skip()
	_, err := utils.TestGoExpr(`
	for i := 0; i < 10; i++ {
		num1 := RandomInt(-20, 10)
		println(&num1)
	}
	`)
	assert.NoError(t, err)
}

// 转换成Json对象
func TestText2Json(t *testing.T) {
	_, err := utils.TestGoCode(`
	func main() {
		str1 :=  "{\"name\":\"hello world\",\"age\":18,\"sex\":true,\"hobby\":[\"football\",\"basketball\"]}"
		json := str1.Text2Json()
		println(json)
		println(json.JSON2Text())
	}
	`)
	assert.NoError(t, err)
}

// 追加新文本
func TestGetData(t *testing.T) {
	_, err := utils.TestGoCode(`
	func main() {
		str1 := "   hello world"
		str2 := "go-mini"
		num := 1
		lineFlag := 2
		println(str1.Append(str2,lineFlag))
	}
	`)
	assert.NoError(t, err)
}

// 截取一段文本 ,截取到最后就填-1
func TestSubstring(t *testing.T) {
	_, err := utils.TestGoCode(`
	func main() {
		str1 := "hello world 雕鸽"
		start := 0
		length := -1
		println(str1.Substring(start,length))
	}
	`)
	assert.NoError(t, err)
}

// 补齐文本至指定长度
func TestPad(t *testing.T) {
	_, err := utils.TestGoCode(`
	func main() {
		str1 := "hello world"
		str2 := "***"
		fillLocation := 1
		println(str1.Pad(str2,12,fillLocation))
	}
	`)
	assert.NoError(t, err)
}

// 改变文本的大小写测试
func TestChangeCase(t *testing.T) {
	_, err := utils.TestGoCode(`
	func main() {
		str1 := "hello WORLD 雕鸽"
		flag := 1
		println(str1.ChangeCase(flag))
		flag = 2
		println(str1.ChangeCase(flag))
	}
	`)
	assert.NoError(t, err)
}

// 列表转换为文本测试
func TestList2Text(t *testing.T) {
	// todo:重构流程
	t.Skip()
	_, err := utils.TestGoCode(`
	func main() {
		list := []string{"hello","world","雕鸽"}
		linkSymn := "|"
		println(list.List2Text(linkSymn))
	}
	`)
	assert.NoError(t, err)
}

// 文本转换为列表测试
func TestText2List(t *testing.T) {
	// todo:重构流程
	t.Skip()
	_, err := utils.TestGoCode(`
	func main() {
		str1 := "hello|world|雕鸽"
		isBank := 1
		rexp := "|"
		println(str1.Text2List(isBank,rexp))
	}
	`)
	assert.NoError(t, err)
}
