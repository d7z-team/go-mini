//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg e2e -out ffi_advanced_ffigen_test.go ffi_advanced_interface.go
package e2e

import (
	"testing"
)

// ffigen:module test
type AdvancedFFI interface {
	// Identity check
	GetSameObject() *TestObj
	IsSame(a, b *TestObj) bool

	// Map keys
	EchoMap(m map[bool]string) map[float64]bool

	// Embedded structs
	EchoEmbedded(e EmbeddedStruct) EmbeddedStruct
}

type TestObj struct {
	Name string
}

type BaseStruct struct {
	BaseField string
}

type EmbeddedStruct struct {
	BaseStruct // Embedded
	ExtraField int
}

type AdvancedFFIImpl struct {
	obj *TestObj
}

func (i *AdvancedFFIImpl) GetSameObject() *TestObj { return i.obj }
func (i *AdvancedFFIImpl) IsSame(a, b *TestObj) bool { return a == b }
func (i *AdvancedFFIImpl) EchoMap(m map[bool]string) map[float64]bool {
	res := make(map[float64]bool)
	for k, v := range m {
		if k {
			res[1.5] = (v == "true")
		}
	}
	return res
}
func (i *AdvancedFFIImpl) EchoEmbedded(e EmbeddedStruct) EmbeddedStruct { return e }

func TestAdvancedFFI(t *testing.T) {
	// 这个测试需要 ffigen 生成的代码，我们先手工验证 HandleRegistry 和 Map 键
}
