package e2e_test

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
		str1 := "hello world GO"
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
		str1 := "hello WORLD GO"
		flag := 1
		println(str1.ChangeCase(flag))
		flag = 2
		println(str1.ChangeCase(flag))
	}
	`)
	assert.NoError(t, err)
}
