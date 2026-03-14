package e2e_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/utils"
)

// MockTypeA 模拟包 pkgA 中的类型
type MockTypeA struct{}

func (m *MockTypeA) GoMiniType() ast.Ident { return "pkgA.TypeA" }
func (m *MockTypeA) GetB() *MockTypeB      { return &MockTypeB{} }
func (m *MockTypeA) GetVal() ast.MiniInt64 { return ast.NewMiniInt64(1) }

// MockTypeB 模拟包 pkgB 中的类型
type MockTypeB struct{}

func (m *MockTypeB) GoMiniType() ast.Ident { return "pkgB.TypeB" }
func (m *MockTypeB) GetA() *MockTypeA      { return &MockTypeA{} }
func (m *MockTypeB) GetVal() ast.MiniInt64 { return ast.NewMiniInt64(2) }

func TestCrossPackageMinimal(t *testing.T) {
	executor := engine.NewMiniExecutor()

	// 注册跨包 Mock 类型
	executor.AddPackageStruct("pkgA", "TypeA", (*MockTypeA)(nil))
	executor.AddPackageStruct("pkgB", "TypeB", (*MockTypeB)(nil))

	var results []string
	executor.MustAddFunc("push", func(v any) {
		results = append(results, utils.FormatValue(v))
	})

	t.Run("circular_dependency_validation", func(t *testing.T) {
		// 验证 A -> B -> A 的链式调用
		code := `
			import "pkgA"
			import "pkgB"
			func main() {
				a := pkgA.TypeA{}
				b := a.GetB()
				a2 := b.GetA()
				push(a2.GetVal())
				push(b.GetVal())
			}
		`
		rt, err := executor.NewRuntimeByGoCode(code)
		assert.NoError(t, err)

		err = rt.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"1", "2"}, results)
	})
}
